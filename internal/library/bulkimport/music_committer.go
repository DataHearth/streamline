package bulkimport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanalbum "github.com/datahearth/streamline/ent/importscanalbum"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/media/music"
)

// runCommitMusic adopts every reviewed album folder in a music scan in place:
// the artist is seeded from MusicBrainz when new, then the files already on
// disk are recorded against the album's tracks. Sequential, since MusicBrainz
// serializes behind a 1 req/s limiter anyway.
func (s *Service) runCommitMusic(ctx context.Context, scan *ent.ImportScan) {
	ctx, span := tracer.Start(ctx, "bulkimport.run_commit_music",
		trace.WithAttributes(attribute.Int64("scan.id", int64(scan.ID))))
	defer span.End()

	defer func() {
		if r := recover(); r != nil {
			s.markScanFailed(ctx, scan.ID, fmt.Sprintf("panic: %v", r))
		}
	}()

	albums, err := s.store.ListImportScanAlbumsForCommit(ctx, scan.ID)
	if err != nil {
		s.markScanFailed(ctx, scan.ID, err.Error())
		return
	}

	artists := map[string]struct{}{}
	var success, failed uint32
	for _, sc := range albums {
		outcome, msg, createdID := s.commitAlbum(ctx, sc, artists)
		if uerr := s.store.UpdateImportScanAlbumOutcome(
			ctx,
			sc.ID,
			outcome,
			db.UpdateScanAlbumOutcomeOpts{Message: msg, CreatedAlbumID: createdID},
		); uerr != nil {
			slog.ErrorContext(ctx, "music commit: failed to record album outcome",
				"scan.id", scan.ID, "album.scan_id", sc.ID, "error", uerr)
		}
		switch outcome {
		case entimportscanalbum.OutcomeCreated:
			success++
		case entimportscanalbum.OutcomeFailed:
			failed++
		}
	}

	committedAt := time.Now()
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scan.ID,
		entimportscan.StatusCompleted,
		db.UpdateScanStatusOpts{
			CommittedAt:        &committedAt,
			CommitSuccessCount: &success,
			CommitFailedCount:  &failed,
		},
	); err != nil {
		slog.ErrorContext(ctx, "music commit: failed to flip scan to completed",
			"scan.id", scan.ID, "error", err)
	}
	slog.InfoContext(ctx, "music commit finished",
		"scan.id", scan.ID,
		"commit.success_count", success,
		"commit.failed_count", failed)
	countCommit(ctx, "music", "success", int64(success))
	countCommit(ctx, "music", "failed", int64(failed))
}

// commitAlbum adopts one album folder. A failure here is that album's alone.
func (s *Service) commitAlbum(
	ctx context.Context,
	sc *ent.ImportScanAlbum,
	artists map[string]struct{},
) (entimportscanalbum.Outcome, string, uint32) {
	ctx, span := tracer.Start(ctx, "bulkimport.commit_album",
		trace.WithAttributes(
			attribute.Int64("album.scan_id", int64(sc.ID)),
			attribute.String("album.folder", sc.FolderPath)))
	defer span.End()

	alb, err := s.resolveAlbum(ctx, sc, artists)
	if err != nil {
		return commitAlbumFail("resolve album", err, 0)
	}

	plan, unmatched, adopted, err := planAlbumFiles(ctx, sc.FolderPath, alb)
	if err != nil {
		return commitAlbumFail("list folder", err, alb.ID)
	}
	if len(plan) == 0 && adopted == 0 {
		return commitAlbumFail(
			"adopt files",
			fmt.Errorf("no file matched a track (%d unmatched)", unmatched),
			alb.ID,
		)
	}
	if err := s.store.AdoptAlbumFiles(ctx, alb.ID, plan); err != nil {
		return commitAlbumFail("adopt files", err, alb.ID)
	}

	var notes []string
	if unmatched > 0 {
		notes = append(notes, fmt.Sprintf("%d files unmatched", unmatched))
	}
	if missing := len(alb.Edges.Tracks) - adopted - len(plan); missing > 0 {
		notes = append(notes, fmt.Sprintf(
			"%d of %d tracks have no file", missing, len(alb.Edges.Tracks)))
	}
	slog.InfoContext(ctx, "album adopted",
		"album.id", alb.ID, "files", len(plan), "unmatched", unmatched)
	return entimportscanalbum.OutcomeCreated, strings.Join(notes, "; "), alb.ID
}

// resolveAlbum returns the stored album a folder belongs to, seeding its
// artist from MusicBrainz first when the library does not hold it yet.
func (s *Service) resolveAlbum(
	ctx context.Context,
	sc *ent.ImportScanAlbum,
	artists map[string]struct{},
) (*ent.Album, error) {
	rgMBID, artistMBID := sc.ReleaseGroupMbid, sc.ArtistMbid
	if sc.DecisionReleaseGroupMbid != "" {
		rgMBID = sc.DecisionReleaseGroupMbid
		artistMBID = ""
		for _, c := range sc.Candidates {
			if c.ReleaseGroupMBID == rgMBID {
				artistMBID = c.ArtistMBID
			}
		}
	} else if sc.ExistingAlbumID != nil {
		return s.store.FindAlbumByID(ctx, *sc.ExistingAlbumID)
	}
	if rgMBID == "" {
		return nil, errors.New("no musicbrainz match to adopt")
	}

	found, err := s.store.FindAlbumByMBID(ctx, rgMBID)
	if err != nil {
		return nil, fmt.Errorf("look up album: %w", err)
	}
	if found != nil {
		return found, nil
	}
	if artistMBID == "" {
		return nil, fmt.Errorf("no artist known for release group %s", rgMBID)
	}
	if err := s.resolveArtist(ctx, artistMBID, artists); err != nil {
		return nil, err
	}
	found, err = s.store.FindAlbumByMBID(ctx, rgMBID)
	if err != nil {
		return nil, fmt.Errorf("look up album: %w", err)
	}
	if found == nil {
		return nil, fmt.Errorf(
			"release group %s is not in the artist's discography", rgMBID)
	}
	return found, nil
}

// resolveArtist makes sure the artist is stored. Adoption must not turn a whole
// discography wanted, so a new artist is added unmonitored; the monitored flag
// is then set on the adopted album alone.
func (s *Service) resolveArtist(
	ctx context.Context, artistMBID string, seen map[string]struct{},
) error {
	if _, ok := seen[artistMBID]; ok {
		return nil
	}
	existing, err := s.store.FindArtistByMBID(ctx, artistMBID)
	if err != nil {
		return fmt.Errorf("look up artist: %w", err)
	}
	if existing == nil {
		_, err := s.musicAdder.Add(ctx, music.AddParams{MBID: artistMBID})
		if err != nil && !errors.Is(err, music.ErrArtistExists) {
			return fmt.Errorf("add artist: %w", err)
		}
	}
	seen[artistMBID] = struct{}{}
	return nil
}

// planAlbumFiles binds the audio files directly in folder to the album's
// tracks without touching the database. adopted counts files a track already
// holds at the same path (an idempotent re-scan); unmatched counts files that
// bind to no track.
func planAlbumFiles(
	ctx context.Context, folder string, alb *ent.Album,
) ([]db.AdoptAlbumFile, int, int, error) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, 0, 0, err
	}
	held := map[string]struct{}{}
	for _, t := range alb.Edges.Tracks {
		for _, mf := range t.Edges.MediaFiles {
			held[mf.Path] = struct{}{}
		}
	}

	var plan []db.AdoptAlbumFile
	unmatched, adopted := 0, 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(folder, e.Name())
		if _, ok := audiotags.AudioExtensions[strings.ToLower(filepath.Ext(path))]; !ok {
			continue
		}
		if _, ok := held[path]; ok {
			adopted++
			continue
		}
		info, err := audiotags.Read(path)
		if err != nil {
			slog.WarnContext(ctx, "music adopt: tag read failed",
				"file", path, "error", err)
			unmatched++
			continue
		}
		stat, err := os.Stat(path)
		if err != nil {
			slog.WarnContext(ctx, "music adopt: stat failed",
				"file", path, "error", err)
			unmatched++
			continue
		}
		tr := matchTrack(alb.Edges.Tracks, info)
		if tr == nil {
			unmatched++
			continue
		}
		plan = append(plan, db.AdoptAlbumFile{
			TrackID: tr.ID,
			Path:    path,
			Quality: info.Format,
			Format:  info.Format,
			Size:    stat.Size(),
		})
	}
	return plan, unmatched, adopted, nil
}

// matchTrack prefers the tagged (disc, track) pair and falls back to the title.
func matchTrack(tracks []*ent.Track, info audiotags.Info) *ent.Track {
	disc := info.Disc
	if disc == 0 {
		disc = 1
	}
	if info.Track > 0 {
		if i := slices.IndexFunc(tracks, func(t *ent.Track) bool {
			return t.Disc == disc && t.Position == info.Track
		}); i >= 0 {
			return tracks[i]
		}
	}
	title := strings.TrimSpace(info.Title)
	if title == "" {
		return nil
	}
	if i := slices.IndexFunc(tracks, func(t *ent.Track) bool {
		return strings.EqualFold(strings.TrimSpace(t.Title), title)
	}); i >= 0 {
		return tracks[i]
	}
	return nil
}

func commitAlbumFail(
	label string, err error, albumID uint32,
) (entimportscanalbum.Outcome, string, uint32) {
	return entimportscanalbum.OutcomeFailed, fmt.Sprintf(
		"%s: %v",
		label,
		err,
	), albumID
}
