package importer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

// albumImportMode is the transfer mode an album import uses. A hardlink shares
// its inode with the seeding source, and tag writing rewrites the file in
// place; a moved file is the seeding source itself. Either would corrupt what
// the torrent is still serving, so those combinations copy.
func albumImportMode(libCfg config.LibraryConfig) string {
	if libCfg.ImportMode == "hardlink" ||
		(libCfg.KeepTorrentSeeding && libCfg.ImportMode == "move") {
		return "copy"
	}
	return libCfg.ImportMode
}

// listAudioFiles returns every audio file at path, which is a file or a
// directory walked recursively. Unreadable descendants are skipped; only an
// unreadable root is an error.
func listAudioFiles(path string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(
		path,
		func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if p == path {
					return walkErr
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if _, ok := audiotags.AudioExtensions[strings.ToLower(filepath.Ext(p))]; ok {
				out = append(out, p)
			}
			return nil
		},
	)
	slices.Sort(out)
	return out, err
}

// albumFile is one source file bound to the track it fills.
type albumFile struct {
	path string
	info audiotags.Info
	tr   *ent.Track
	q    albumFileQuality
}

// importAlbumRecord imports a completed album download: every audio file is
// matched to a track by its tags, placed under the music library, and retagged
// on the library copy. Tracks the release did not cover stay without a file and
// the album stays wanted. Verification and probing are skipped, they are
// video-specific.
func (w *Worker) importAlbumRecord(
	ctx context.Context,
	span trace.Span,
	rec *ent.DownloadRecord,
	libCfg config.LibraryConfig,
) error {
	alb := rec.Edges.Album
	artist := alb.Edges.Artist
	if artist == nil {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("album %d missing artist context", alb.ID),
		)
	}
	span.SetAttributes(
		attribute.Int64("album.id", int64(alb.ID)),
		attribute.Int64("artist.id", int64(artist.ID)),
	)
	defer w.lockEntity(fmt.Sprintf("album:%d", alb.ID))()

	files, err := listAudioFiles(rec.SavePath)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("list album files: %w", err))
	}
	profile, haveProfile := config.ResolveMusicQualityProfile(artist.QualityProfile)
	plan, skippedCovered := w.planAlbumImport(ctx, rec, alb, files, profile)
	if len(plan) == 0 {
		if skippedCovered > 0 {
			return otelx.RecordSpanError(span, library.ErrDestExists)
		}
		return otelx.RecordSpanError(span, ErrNoAlbumTracks)
	}
	// Verified before anything is set aside or transferred, so a hold leaves
	// the library exactly as it was.
	if haveProfile && !rec.VerificationBypassed {
		if reasons := tierHoldReasons(plan, profile); len(reasons) > 0 {
			return w.hold(ctx, span, rec, reasons)
		}
	}

	multiDisc := slices.ContainsFunc(alb.Edges.Tracks, func(t *ent.Track) bool {
		return t.Disc > 1
	})
	mode := albumImportMode(libCfg)
	var year uint16
	if alb.ReleaseDate != nil {
		year = numeric.SaturateU16(alb.ReleaseDate.Year())
	}

	var (
		rows     []db.AdoptAlbumFile
		aside    []string
		replaced []*ent.MediaFile
		firstErr error
	)
	for _, f := range plan {
		old := f.tr.Edges.MediaFiles
		moved, err := setAside(ctx, old)
		if err != nil {
			slog.WarnContext(ctx, "album import: set existing file aside failed",
				"track.id", f.tr.ID, "error", err)
			firstErr = errors.Join(firstErr, err)
			continue
		}
		imported, err := w.lib.ImportAlbumTrack(
			ctx, f.path, artist, alb, f.tr, multiDisc, mode,
		)
		if err != nil {
			putBack(ctx, moved)
			slog.WarnContext(ctx, "album import: track import failed",
				"file", filepath.Base(f.path), "track.id", f.tr.ID, "error", err)
			firstErr = errors.Join(firstErr, err)
			continue
		}
		aside = append(aside, moved...)
		replaced = append(replaced, old...)

		if err := audiotags.Write(imported.Path, audiotags.WriteTags{
			Artist:           artist.Name,
			AlbumArtist:      artist.Name,
			Album:            alb.Title,
			Title:            f.tr.Title,
			Track:            f.tr.Position,
			Disc:             f.tr.Disc,
			Year:             year,
			MBArtistID:       artist.Mbid,
			MBReleaseGroupID: alb.Mbid,
			MBRecordingID:    f.tr.Mbid,
		}); err != nil {
			slog.WarnContext(
				ctx,
				"album import: tag write failed, file imported untagged",
				"file",
				imported.Path,
				"track.id",
				f.tr.ID,
				"error",
				err,
			)
		}
		size := imported.Size
		if st, err := os.Stat(imported.Path); err == nil {
			size = st.Size()
		}
		rows = append(rows, db.AdoptAlbumFile{
			TrackID: f.tr.ID,
			Path:    imported.Path,
			Quality: tierName(f.q),
			Format:  f.info.Format,
			Size:    size,
		})
	}
	if len(rows) == 0 {
		return otelx.RecordSpanError(span, firstErr)
	}

	for _, mf := range replaced {
		if err := w.db.DeleteMediaFile(ctx, mf.ID); err != nil {
			return otelx.RecordSpanError(
				span, fmt.Errorf("delete replaced track media file: %w", err),
			)
		}
	}
	if err := w.db.RecordAlbumImportSuccess(ctx, db.RecordAlbumImportSuccessParams{
		RecordID: rec.ID,
		AlbumID:  alb.ID,
		Files:    rows,
	}); err != nil {
		return otelx.RecordSpanError(
			span, fmt.Errorf("record album import success: %w", err),
		)
	}
	dropAside(ctx, aside)
	if w.covers != nil {
		w.covers.ResolveCoversInBackground(ctx, alb.ID)
	}

	if gaps := missingTracks(alb, rows); gaps > 0 {
		slog.WarnContext(ctx, "album imported with tracks still missing",
			"album.id", alb.ID, "imported", len(rows), "missing", gaps)
	}
	slog.InfoContext(ctx, "imported album",
		"album.id", alb.ID, "artist.id", artist.ID, "files", len(rows))

	w.markRequestsAvailableByMBID(ctx, "artist", artist.Mbid)

	// The torrent is judged by what the import actually did: a forced copy left
	// the source untouched, so a configured "move" must not delete it.
	libCfg.ImportMode = mode
	w.cleanupTorrent(ctx, rec, libCfg)
	w.refreshMediaServers(ctx, "music", libCfg.MusicPath)
	return nil
}

// planAlbumImport binds each source file to the track it fills. A track
// already holding a file is left alone unless the record replaces it: a record
// in replace mode all takes every track it covers, one in upgrades mode only
// the tracks whose held tier the profile lets this file beat. Two files never
// fill one track: the first wins. skippedCovered counts the files dropped
// because their track was already filled in the library.
func (w *Worker) planAlbumImport(
	ctx context.Context,
	rec *ent.DownloadRecord,
	alb *ent.Album,
	files []string,
	profile config.MusicQualityProfileEntry,
) ([]albumFile, int) {
	claim := library.ParseMusicRelease(rec.Title)
	claimed := map[uint32]struct{}{}
	var (
		plan           []albumFile
		skippedCovered int
	)
	for _, path := range files {
		info, err := audiotags.Read(path)
		if err != nil {
			slog.WarnContext(ctx, "album import: tag read failed",
				"file", path, "error", err)
			continue
		}
		tr := library.MatchTrack(alb.Edges.Tracks, info)
		if tr == nil {
			slog.WarnContext(ctx, "album import: file matched no track",
				"file", filepath.Base(path), "album.id", alb.ID)
			continue
		}
		if _, dup := claimed[tr.ID]; dup {
			slog.WarnContext(
				ctx,
				"album import: track already filled by another file",
				"file",
				filepath.Base(path),
				"track.id",
				tr.ID,
			)
			continue
		}
		q := w.assessAlbumFile(ctx, path, claim)
		if len(tr.Edges.MediaFiles) > 0 && !replacesTrack(rec, tr, q, profile) {
			skippedCovered++
			slog.InfoContext(ctx, "album import: leaving track's file in place",
				"track.id", tr.ID, "file", filepath.Base(path))
			continue
		}
		claimed[tr.ID] = struct{}{}
		plan = append(plan, albumFile{path: path, info: info, tr: tr, q: q})
	}
	return plan, skippedCovered
}

// missingTracks counts the album's tracks that hold no file once rows landed.
func missingTracks(alb *ent.Album, rows []db.AdoptAlbumFile) int {
	filled := map[uint32]struct{}{}
	for _, r := range rows {
		filled[r.TrackID] = struct{}{}
	}
	missing := 0
	for _, t := range alb.Edges.Tracks {
		_, got := filled[t.ID]
		if !got && len(t.Edges.MediaFiles) == 0 {
			missing++
		}
	}
	return missing
}

// replacesTrack decides whether an incoming file takes the place of the files
// its track already holds.
func replacesTrack(
	rec *ent.DownloadRecord,
	tr *ent.Track,
	q albumFileQuality,
	profile config.MusicQualityProfileEntry,
) bool {
	switch rec.ReplaceMode {
	case downloadrecord.ReplaceModeAll:
		return true
	case downloadrecord.ReplaceModeUpgrades:
		have, haveKnown := heldTrackTier(tr)
		return q.known && profile.Profile().Replaces(have, haveKnown, q.tier)
	}
	return false
}

// tierName is what MediaFile.quality stores: the tier, or empty when it could
// not be established.
func tierName(q albumFileQuality) string {
	if !q.known {
		return ""
	}
	return q.tier.String()
}
