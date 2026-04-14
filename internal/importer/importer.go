// Package importer runs the post-download pipeline: find media file, apply
// naming template, transfer to library, update DB, refresh media servers.
// Fed by internal/jobs/download_monitor (event-fast path) and by the
// import_scan scheduler job (restart-safe path).
package importer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("github.com/datahearth/streamline/internal/importer")

type MediaServerDispatcher interface {
	RefreshAll(ctx context.Context, libraryPath string) error
}

// Enqueuer is the consumer-facing queue surface. download_monitor accepts it
// so it can be driven by a fake in tests without pulling in the full Worker.
type Enqueuer interface {
	Enqueue(recordID uint32)
}

// Deps is worker wiring. User-facing knobs (max attempts, keep seeding,
// allowed roots, movie library path) are read via config.Get() inside
// runImport.
type Deps struct {
	DB          db.Store
	Library     *library.ImportService
	Download    download.Downloader
	MediaServer MediaServerDispatcher
}

const (
	consumers  = 2
	channelCap = 100
)

type Worker struct {
	db  db.Store
	lib *library.ImportService
	dl  download.Downloader
	ms  MediaServerDispatcher

	ch       chan uint32
	mu       sync.Mutex
	inFlight map[uint32]struct{}
}

func NewWorker(d Deps) *Worker {
	return &Worker{
		db:       d.DB,
		lib:      d.Library,
		dl:       d.Download,
		ms:       d.MediaServer,
		ch:       make(chan uint32, channelCap),
		inFlight: make(map[uint32]struct{}),
	}
}

// Start spawns consumer goroutines reading from the queue. Blocks until ctx
// is canceled. Safe to call once per app lifetime.
func (w *Worker) Start(ctx context.Context) {
	var wg sync.WaitGroup
	for range consumers {
		wg.Go(func() { w.consume(ctx) })
	}
	<-ctx.Done()
	close(w.ch)
	wg.Wait()
}

// Enqueue pushes a record ID into the import queue. Non-blocking: when the
// queue is full the ID is dropped (import_scan will pick it up on the next
// tick). Dedupe: IDs already in-flight are dropped.
func (w *Worker) Enqueue(recordID uint32) {
	w.mu.Lock()
	_, inFlight := w.inFlight[recordID]
	w.mu.Unlock()
	if inFlight {
		return
	}
	select {
	case w.ch <- recordID:
	default:
		slog.WarnContext(
			context.Background(),
			"importer queue full, dropping enqueue",
			"record.id", recordID,
		)
	}
}

// Scan re-enqueues all DownloadRecords sitting at status=importing. Used by
// the scheduler as a safety net after a restart or a dropped enqueue.
func (w *Worker) Scan(ctx context.Context) error {
	records, err := w.db.ListImportingDownloadRecords(ctx)
	if err != nil {
		return err
	}
	for _, r := range records {
		w.Enqueue(r.ID)
	}
	return nil
}

func (w *Worker) consume(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id, ok := <-w.ch:
			if !ok {
				return
			}
			w.mu.Lock()
			if _, dup := w.inFlight[id]; dup {
				w.mu.Unlock()
				continue
			}
			w.inFlight[id] = struct{}{}
			w.mu.Unlock()

			err := w.runImport(ctx, id)
			w.handleOutcome(ctx, id, err)

			w.mu.Lock()
			delete(w.inFlight, id)
			w.mu.Unlock()
		}
	}
}

func (w *Worker) runImport(ctx context.Context, recordID uint32) error {
	ctx, span := tracer.Start(ctx, "importer.run",
		trace.WithAttributes(attribute.Int64("download_record.id", int64(recordID))),
	)
	defer span.End()

	rec, err := w.db.FindImportingDownloadRecordByID(ctx, recordID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("find importing record: %w", err),
		)
	}
	libCfg := config.Get().Library
	span.SetAttributes(
		attribute.Int("import.attempt", int(rec.ImportAttempts)+1),
		attribute.String("save_path", rec.SavePath),
	)

	if len(libCfg.AllowedDownloadRoots) > 0 {
		allowed := false
		for _, root := range libCfg.AllowedDownloadRoots {
			if strings.HasPrefix(rec.SavePath, root) {
				allowed = true
				break
			}
		}
		if !allowed {
			return otelx.RecordSpanError(span, ErrPathNotAllowed)
		}
	}

	switch {
	case rec.Edges.Movie != nil:
		return w.importMovieRecord(ctx, span, rec, libCfg)
	case rec.Edges.Episode != nil:
		return w.importEpisodeRecord(ctx, span, rec, libCfg)
	default:
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("record %d has neither movie nor episode", recordID),
		)
	}
}

func (w *Worker) importMovieRecord(
	ctx context.Context,
	span trace.Span,
	rec *ent.DownloadRecord,
	libCfg config.LibraryConfig,
) error {
	m := rec.Edges.Movie
	span.SetAttributes(attribute.Int64("movie.id", int64(m.ID)))

	imported, err := w.lib.ImportMovie(ctx, rec.SavePath, m, "")
	if errors.Is(err, library.ErrDestExists) && rec.ReplaceExisting {
		if rErr := w.replaceMovieFiles(ctx, m.ID); rErr != nil {
			return otelx.RecordSpanError(span, rErr)
		}
		imported, err = w.lib.ImportMovie(ctx, rec.SavePath, m, "")
	}
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}

	if err := w.db.RecordImportSuccess(ctx, db.RecordImportSuccessParams{
		RecordID: rec.ID,
		MovieID:  m.ID,
		File: db.MediaFileRow{
			Path:         imported.Path,
			Size:         imported.Size,
			Quality:      imported.Parsed.Resolution,
			Format:       imported.Parsed.Extension,
			ReleaseGroup: imported.Parsed.Group,
		},
	}); err != nil {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("record import success: %w", err),
		)
	}
	slog.InfoContext(ctx, "imported file",
		"media_file.path", imported.Path,
		"movie.id", m.ID,
		"movie.tmdb_id", m.TmdbID,
	)

	w.markRequestsAvailable(ctx, "movie", m.TmdbID)
	w.refreshMediaServers(ctx, libCfg.MoviePath)
	w.cleanupTorrent(ctx, rec, libCfg)
	return nil
}

// replaceMovieFiles deletes a movie's current media file(s) from disk and DB so
// a replace-flagged grab can re-import over them. Only reached when an import
// hit ErrDestExists and the record requested replacement.
func (w *Worker) replaceMovieFiles(ctx context.Context, movieID uint32) error {
	files, err := w.db.ListMediaFilesByMovieID(ctx, movieID)
	if err != nil {
		return fmt.Errorf("list movie files: %w", err)
	}
	for _, mf := range files {
		if err := os.Remove(mf.Path); err != nil && !os.IsNotExist(err) {
			slog.WarnContext(ctx, "replace: remove existing movie file failed",
				"path", mf.Path, "error", err)
		}
		if err := w.db.DeleteMediaFileAndRevertMovie(
			ctx,
			mf.ID,
			movieID,
		); err != nil {
			return fmt.Errorf("delete movie media file: %w", err)
		}
	}
	return nil
}

// replaceEpisodeFile deletes an episode's current media file (disk + DB) so a
// replace-flagged grab can re-import over it. A missing file is a no-op.
func (w *Worker) replaceEpisodeFile(ctx context.Context, episodeID uint32) error {
	mf, err := w.db.FindMediaFileByEpisodeID(ctx, episodeID)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find episode media file: %w", err)
	}
	if err := os.Remove(mf.Path); err != nil && !os.IsNotExist(err) {
		slog.WarnContext(ctx, "replace: remove existing episode file failed",
			"path", mf.Path, "error", err)
	}
	return w.db.DeleteMediaFileAndRevertEpisode(ctx, mf.ID, episodeID)
}

// importEpisodeRecord links a completed TV download to its episode(s). A
// single-episode record imports the one file; a season-pack record (a
// directory of multiple video files) matches each file to an episode and
// imports the matches, leaving unmatched episodes wanted.
func (w *Worker) importEpisodeRecord(
	ctx context.Context,
	span trace.Span,
	rec *ent.DownloadRecord,
	libCfg config.LibraryConfig,
) error {
	ep := rec.Edges.Episode
	season := ep.Edges.Season
	if season == nil || season.Edges.TvShow == nil {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("episode %d missing season/show context", ep.ID),
		)
	}
	show := season.Edges.TvShow
	anime := show.Type == tvshow.TypeAnime
	span.SetAttributes(
		attribute.Int64("tvshow.id", int64(show.ID)),
		attribute.Int64("episode.id", int64(ep.ID)),
	)

	info, err := os.Stat(rec.SavePath)
	if err != nil {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("stat save path: %w", err),
		)
	}

	// Single file (or a dir resolving to exactly one file) → import directly to
	// the record's own episode. Otherwise treat it as a season pack.
	if !info.IsDir() {
		return w.importSingleEpisode(ctx, span, rec, show, season.Number, ep)
	}
	files, err := library.ListVideoFiles(rec.SavePath)
	if err != nil {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("list pack files: %w", err),
		)
	}
	if len(files) <= 1 {
		return w.importSingleEpisode(ctx, span, rec, show, season.Number, ep)
	}

	matched := 0
	for _, f := range files {
		parsed := library.Parse(filepath.Base(f))
		target := library.MatchEpisode(parsed, show.Edges.Seasons, anime)
		if target == nil {
			slog.WarnContext(ctx, "season pack file matched no episode",
				"file", filepath.Base(f), "tvshow.id", show.ID)
			continue
		}
		tSeason := episodeSeasonNumber(show.Edges.Seasons, target)
		imported, err := w.lib.ImportEpisode(ctx, f, show, tSeason, target)
		if errors.Is(err, library.ErrDestExists) && rec.ReplaceExisting {
			if rErr := w.replaceEpisodeFile(ctx, target.ID); rErr != nil {
				slog.WarnContext(ctx, "season pack replace: clear existing failed",
					"episode.id", target.ID, "error", rErr)
				continue
			}
			imported, err = w.lib.ImportEpisode(ctx, f, show, tSeason, target)
		}
		if err != nil {
			slog.WarnContext(ctx, "season pack file import failed",
				"file", filepath.Base(f), "error", err)
			continue
		}
		if err := w.db.RecordEpisodeImportSuccess(
			ctx,
			db.RecordEpisodeImportSuccessParams{
				RecordID:  rec.ID,
				EpisodeID: target.ID,
				File: db.MediaFileRow{
					Path:         imported.Path,
					Size:         imported.Size,
					Quality:      imported.Parsed.Resolution,
					Format:       imported.Parsed.Extension,
					ReleaseGroup: imported.Parsed.Group,
				},
			},
		); err != nil {
			return otelx.RecordSpanError(
				span,
				fmt.Errorf("record episode import success: %w", err),
			)
		}
		matched++
	}
	if matched == 0 {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("season pack matched no episodes"),
		)
	}
	slog.InfoContext(ctx, "imported season pack",
		"tvshow.id", show.ID, "matched", matched, "files", len(files))

	w.markRequestsAvailable(ctx, "tvshow", show.TvdbID)
	w.refreshMediaServers(ctx, libCfg.SeriesPath)
	w.cleanupTorrent(ctx, rec, libCfg)
	return nil
}

func (w *Worker) importSingleEpisode(
	ctx context.Context,
	span trace.Span,
	rec *ent.DownloadRecord,
	show *ent.TVShow,
	seasonNumber uint16,
	ep *ent.Episode,
) error {
	imported, err := w.lib.ImportEpisode(ctx, rec.SavePath, show, seasonNumber, ep)
	if errors.Is(err, library.ErrDestExists) && rec.ReplaceExisting {
		if rErr := w.replaceEpisodeFile(ctx, ep.ID); rErr != nil {
			return otelx.RecordSpanError(span, rErr)
		}
		imported, err = w.lib.ImportEpisode(
			ctx,
			rec.SavePath,
			show,
			seasonNumber,
			ep,
		)
	}
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	if err := w.db.RecordEpisodeImportSuccess(
		ctx,
		db.RecordEpisodeImportSuccessParams{
			RecordID:  rec.ID,
			EpisodeID: ep.ID,
			File: db.MediaFileRow{
				Path:         imported.Path,
				Size:         imported.Size,
				Quality:      imported.Parsed.Resolution,
				Format:       imported.Parsed.Extension,
				ReleaseGroup: imported.Parsed.Group,
			},
		},
	); err != nil {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("record episode import success: %w", err),
		)
	}
	slog.InfoContext(ctx, "imported episode file",
		"media_file.path", imported.Path,
		"tvshow.id", show.ID, "episode.id", ep.ID)

	w.markRequestsAvailable(ctx, "tvshow", show.TvdbID)
	libCfg := config.Get().Library
	w.refreshMediaServers(ctx, libCfg.SeriesPath)
	w.cleanupTorrent(ctx, rec, libCfg)
	return nil
}

func (w *Worker) refreshMediaServers(ctx context.Context, libraryPath string) {
	if w.ms == nil {
		return
	}
	if err := w.ms.RefreshAll(ctx, libraryPath); err != nil {
		slog.WarnContext(ctx, "media server refresh reported errors", "error", err)
	}
}

// markRequestsAvailable best-effort flips any approved requests for this media
// to available once it imports. Failures are logged, never fatal to the import.
func (w *Worker) markRequestsAvailable(
	ctx context.Context,
	mediaType string,
	mediaID uint32,
) {
	if err := w.db.MarkRequestsAvailable(ctx, mediaType, mediaID); err != nil {
		slog.WarnContext(ctx, "mark requests available failed",
			"media.type", mediaType, "media.id", mediaID, "error", err)
	}
}

func (w *Worker) cleanupTorrent(
	ctx context.Context,
	rec *ent.DownloadRecord,
	libCfg config.LibraryConfig,
) {
	if libCfg.KeepTorrentSeeding || rec.DownloadClientName == "" {
		return
	}
	if err := w.dl.RemoveTorrent(
		ctx,
		rec.DownloadClientName,
		rec.TorrentHash,
	); err != nil {
		slog.WarnContext(ctx, "remove torrent failed",
			"hash", rec.TorrentHash, "error", err)
	}
}

// episodeSeasonNumber finds which season an episode belongs to within the
// eager-loaded season set (episodes don't carry the season number directly).
func episodeSeasonNumber(seasons []*ent.Season, ep *ent.Episode) uint16 {
	for _, se := range seasons {
		for _, e := range se.Edges.Episodes {
			if e.ID == ep.ID {
				return se.Number
			}
		}
	}
	return 0
}

func (w *Worker) handleOutcome(ctx context.Context, recordID uint32, runErr error) {
	if runErr == nil {
		return
	}
	if errors.Is(runErr, context.Canceled) ||
		errors.Is(runErr, context.DeadlineExceeded) {
		return
	}

	rec, err := w.db.FindImportingDownloadRecordByID(ctx, recordID)
	if err != nil {
		if ent.IsNotFound(err) {
			return
		}
		slog.ErrorContext(ctx, "importer outcome lookup failed", "error", err)
		return
	}
	attempts := rec.ImportAttempts + 1
	isTerminal := classify(runErr) == terminal ||
		attempts >= config.Get().Library.ImportMaxAttempts

	params := db.RecordImportFailureParams{
		RecordID: rec.ID,
		Terminal: isTerminal,
		Attempts: attempts,
	}
	if rec.Edges.Movie != nil {
		params.MovieID = rec.Edges.Movie.ID
	}
	if rec.Edges.Episode != nil {
		params.EpisodeID = rec.Edges.Episode.ID
	}
	if isTerminal {
		params.Reason = strings.TrimSpace(runErr.Error())
		if len(params.Reason) > 256 {
			params.Reason = params.Reason[:256]
		}
	}
	if err := w.db.RecordImportFailure(ctx, params); err != nil {
		slog.ErrorContext(ctx, "record import failure write failed", "error", err)
		return
	}
	slog.LogAttrs(ctx, slog.LevelWarn, "import failed",
		slog.Int("record.id", int(rec.ID)),
		slog.Int("attempts", int(attempts)),
		slog.Bool("terminal", isTerminal),
		slog.String("error", runErr.Error()))
}
