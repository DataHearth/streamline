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
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/mediaserver"
	"github.com/datahearth/streamline/internal/otelx"
)

// albumImportMode is the transfer mode an album import uses. A move while the
// torrent seeds would pull the file out from under it, so it copies. A
// hardlink is safe from the tag writer, which renames a fresh copy over the
// file and so never reaches the torrent's bytes, but its first write splits
// the two: under music_tags.seeding copy that is accepted now and the import
// copies outright, under defer the hardlink is kept and the job tags the file
// once its torrent is gone. With tags off a hardlink stays a hardlink.
func albumImportMode(libCfg config.LibraryConfig) string {
	tags := libCfg.MusicTags
	switch {
	case libCfg.KeepTorrentSeeding && libCfg.ImportMode == "move":
		return "copy"
	case libCfg.ImportMode == "hardlink" &&
		tags.Mode != config.MusicTagsOff &&
		tags.Seeding == config.MusicTagsSeedingCopy:
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
			if audiotags.IsAudioFile(p) {
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
	files, err := listAudioFiles(rec.SavePath)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("list album files: %w", err))
	}
	files = w.sourceFilter(ctx, rec).Filter(files)
	held, err := w.importAlbumFiles(
		ctx, span, rec, rec.Edges.Album, files, libCfg, false,
	)
	if err != nil || held {
		return err
	}
	// The torrent is judged by what the import actually did: a forced copy left
	// the source untouched, so a configured "move" must not delete it.
	libCfg.ImportMode = albumImportMode(libCfg)
	w.cleanupTorrent(ctx, rec, libCfg)
	w.refreshMediaServers(ctx, mediaserver.KindMusic, libCfg.MusicPath)
	return nil
}

// cueImage reports whether files are one audio file with a CUE sheet in its
// folder: a disc ripped to a single image, its tracks only marked by the sheet.
func cueImage(files []string) bool {
	if len(files) != 1 {
		return false
	}
	entries, err := os.ReadDir(filepath.Dir(files[0]))
	if err != nil {
		return false
	}
	return slices.ContainsFunc(entries, func(e os.DirEntry) bool {
		return !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".cue")
	})
}

// hydratedAlbum returns the album with its tracks: one added as a stub has
// none until its release data is fetched, and a download for it cannot be
// matched before that. One hydrated before genres were fetched (NULL) gets
// them now, best effort, since the tags written below carry them.
func (w *Worker) hydratedAlbum(
	ctx context.Context,
	alb *ent.Album,
) (*ent.Album, error) {
	if (alb.MetadataFetchedAt != nil && alb.Genres != nil) || w.albums == nil {
		return alb, nil
	}
	if err := w.albums.HydrateAlbum(ctx, alb.ID); err != nil {
		return nil, fmt.Errorf("hydrate album %d: %w", alb.ID, err)
	}
	return w.db.FindAlbumByID(ctx, alb.ID)
}

// importAlbumFiles places the given source files into one album. pack is set
// when the album is one of a discography pack's: the record is then left for
// the caller to complete, and the torrent is not cleaned up here. held reports
// that the record was parked for review instead of imported.
func (w *Worker) importAlbumFiles(
	ctx context.Context,
	span trace.Span,
	rec *ent.DownloadRecord,
	alb *ent.Album,
	files []string,
	libCfg config.LibraryConfig,
	pack bool,
) (bool, error) {
	alb, err := w.hydratedAlbum(ctx, alb)
	if err != nil {
		return false, otelx.RecordSpanError(span, err)
	}
	artist := alb.Edges.Artist
	if artist == nil {
		return false, otelx.RecordSpanError(
			span,
			fmt.Errorf("album %d missing artist context", alb.ID),
		)
	}
	span.SetAttributes(
		attribute.Int64("album.id", int64(alb.ID)),
		attribute.Int64("artist.id", int64(artist.ID)),
	)
	defer w.lockEntity(fmt.Sprintf("album:%d", alb.ID))()

	// Checked before matching: the image's tags can name the first track, and
	// the whole disc would then be imported as that one track.
	if len(alb.Edges.Tracks) > 1 && cueImage(files) {
		return false, otelx.RecordSpanError(span, ErrCueImage)
	}

	profile, haveProfile := config.ResolveMusicQualityProfile(artist.QualityProfile)
	plan, skippedCovered := w.planAlbumImport(ctx, rec, alb, files, profile)
	if len(plan) == 0 {
		if skippedCovered > 0 {
			return false, otelx.RecordSpanError(span, ErrTracksHeld)
		}
		return false, otelx.RecordSpanError(span, ErrNoAlbumTracks)
	}
	// Verified before anything is set aside or transferred, so a hold leaves
	// the library exactly as it was.
	if !rec.VerificationBypassed {
		reasons := albumHoldReasons(plan, alb)
		if haveProfile {
			reasons = append(reasons, tierHoldReasons(plan, profile)...)
		}
		if len(reasons) > 0 {
			return true, w.hold(ctx, span, rec, reasons)
		}
	}

	multiDisc := slices.ContainsFunc(alb.Edges.Tracks, func(t *ent.Track) bool {
		return t.Disc > 1
	})
	mode := albumImportMode(libCfg)
	tagging := libCfg.MusicTags.Mode != config.MusicTagsOff

	var (
		rows     []db.AdoptAlbumFile
		aside    []string
		replaced []*ent.MediaFile
		firstErr error
	)
	// The album's files change from here until the replaced ones are gone:
	// a tag write must not rename a copy of an old file over its path in
	// between. Released before the album is tagged, which takes it per file.
	unlockFiles := library.LockAlbumFiles(alb.ID)
	defer unlockFiles()
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

		rows = append(rows, db.AdoptAlbumFile{
			TrackID:     f.tr.ID,
			Path:        imported.Path,
			Quality:     tierName(f.q),
			Format:      f.info.Format,
			Size:        imported.Size,
			TagsPending: tagging,
		})
	}
	if len(rows) == 0 {
		return false, otelx.RecordSpanError(span, firstErr)
	}

	for _, mf := range replaced {
		if err := w.db.DeleteMediaFile(ctx, mf.ID); err != nil {
			return false, otelx.RecordSpanError(
				span, fmt.Errorf("delete replaced track media file: %w", err),
			)
		}
	}
	if err := w.db.RecordAlbumImportSuccess(ctx, db.RecordAlbumImportSuccessParams{
		RecordID:   rec.ID,
		AlbumID:    alb.ID,
		Files:      rows,
		KeepRecord: pack,
	}); err != nil {
		return false, otelx.RecordSpanError(
			span, fmt.Errorf("record album import success: %w", err),
		)
	}
	dropAside(ctx, aside)
	unlockFiles()
	w.recordAlbumImported(ctx, alb.ID, rec.Title, pack, rows)
	// Tagged before the media servers hear of the files, so they index them
	// once. A file the torrent still shares is deferred to the job.
	coverResolved := false
	if tagging && w.tagger != nil {
		var err error
		if coverResolved, err = w.tagger.TagAlbum(ctx, alb.ID); err != nil {
			slog.WarnContext(
				ctx,
				"album import: tagging the files failed, the job retries",
				"album.id",
				alb.ID,
				"error",
				err,
			)
		}
	}
	// The tagger resolved the cover before writing the files; a second
	// pass would read every file again for nothing.
	if w.covers != nil && !coverResolved {
		w.covers.ResolveCoversInBackground(ctx, alb.ID)
	}

	if gaps := missingTracks(alb, rows); gaps > 0 {
		slog.WarnContext(ctx, "album imported with tracks still missing",
			"album.id", alb.ID, "imported", len(rows), "missing", gaps)
	}
	slog.InfoContext(ctx, "imported album",
		"album.id", alb.ID, "artist.id", artist.ID, "files", len(rows))

	w.markRequestsAvailableByMBID(ctx, "artist", artist.Mbid)
	w.markRequestsAvailableByMBID(ctx, "album", alb.Mbid)
	return false, nil
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
	// Every file's tags first: whether their numbers name the album's tracks
	// is decided over the whole release.
	paths := make([]string, 0, len(files))
	infos := make([]audiotags.Info, 0, len(files))
	for _, path := range files {
		info, err := audiotags.Read(path)
		if err != nil {
			slog.WarnContext(ctx, "album import: tag read failed",
				"file", path, "error", err)
			continue
		}
		paths = append(paths, path)
		infos = append(infos, info)
	}
	matcher := library.NewTrackMatcher(alb.Edges.Tracks, infos)
	if !matcher.ByNumber() {
		slog.InfoContext(
			ctx,
			"album import: track numbers name other titles, matching by title",
			"album.id",
			alb.ID,
		)
	}
	for i, path := range paths {
		info := infos[i]
		tr := matcher.Match(info)
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

// recordAlbumImported files one imported event for the album, however many
// tracks it took: the per-file create hook leaves track files alone, since an
// album is one import from where its owner stands. A pack records one per
// album it filled, against that album.
func (w *Worker) recordAlbumImported(
	ctx context.Context,
	albumID uint32,
	release string,
	pack bool,
	rows []db.AdoptAlbumFile,
) {
	var size int64
	for _, r := range rows {
		size += r.Size
	}
	p := map[string]any{
		"release_title": release,
		"tracks":        len(rows),
		"size_bytes":    size,
	}
	if q := rows[0].Quality; q != "" {
		p["quality"] = q
	}
	if pack {
		p["source"] = "pack"
	}
	if err := events.Record(
		ctx, nil, events.TypeImported, events.ScopeAlbum, albumID, p,
	); err != nil {
		slog.WarnContext(ctx, "album import: record imported event failed",
			"album.id", albumID, "error", err)
	}
}
