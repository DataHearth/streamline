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
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/ffmpeg"
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
	Prober      ffmpeg.Prober
}

const (
	consumers  = 2
	channelCap = 100
)

type Worker struct {
	db    db.Store
	lib   *library.ImportService
	dl    download.Downloader
	ms    MediaServerDispatcher
	probe ffmpeg.Prober

	ch   chan uint32
	stop chan struct{}

	mu       sync.Mutex
	inFlight map[uint32]struct{}

	// entityLocks holds one *sync.Mutex per import target. Queue dedup is by
	// download-record ID, but two records can target one movie or show, and a
	// destination path is derived from the target alone — concurrent consumers
	// would race on it. Entries are never evicted: the key space is the
	// library's, and deleting a mutex another goroutine is about to lock is
	// how this bug comes back.
	entityLocks sync.Map
}

func NewWorker(d Deps) *Worker {
	return &Worker{
		db:       d.DB,
		lib:      d.Library,
		dl:       d.Download,
		ms:       d.MediaServer,
		probe:    d.Prober,
		ch:       make(chan uint32, channelCap),
		stop:     make(chan struct{}),
		inFlight: make(map[uint32]struct{}),
	}
}

// Start spawns consumer goroutines reading from the queue. Blocks until ctx
// is canceled and every consumer has finished its current import. Safe to
// call once per app lifetime.
//
// w.ch is deliberately never closed: scheduler jobs holding this worker as an
// importer.Enqueuer keep calling Enqueue after ctx is canceled, and a send on
// a closed channel would panic them. Consumers terminate on ctx.Done instead;
// w.stop turns those late enqueues into no-ops.
func (w *Worker) Start(ctx context.Context) {
	var wg sync.WaitGroup
	for range consumers {
		wg.Go(func() { w.consume(ctx) })
	}
	<-ctx.Done()
	close(w.stop)
	wg.Wait()
}

// Enqueue pushes a record ID into the import queue. Non-blocking: when the
// queue is full the ID is dropped (import_scan will pick it up on the next
// tick). Dedupe: IDs already in-flight are dropped. After shutdown every
// enqueue is dropped — nothing is left to consume the queue.
func (w *Worker) Enqueue(recordID uint32) {
	select {
	case <-w.stop:
		slog.DebugContext(
			context.Background(),
			"importer stopped, dropping enqueue",
			"record.id", recordID,
		)
		return
	default:
	}

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
		case id := <-w.ch:
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

// lockEntity blocks until this import owns key and returns its release func.
// It must be held across both the "already has a file" precondition read and
// the transfer, or the read is stale by the time the file lands. Exactly one
// key is taken per import — a second acquisition would be a deadlock.
func (w *Worker) lockEntity(key string) func() {
	v, _ := w.entityLocks.LoadOrStore(key, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
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
			if download.PathUnderRoot(rec.SavePath, root) {
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

// probeSource best-effort-probes a source media file before transfer. A nil
// info with a nil error means probing is off or unavailable — never a bad
// file, which is what tells verification to stay out of the way.
func (w *Worker) probeSource(
	ctx context.Context,
	path string,
) (*ffmpeg.Info, error) {
	if !config.Get().FFmpeg.Enabled || w.probe == nil || !w.probe.Available() {
		return nil, nil
	}
	info, err := w.probe.Probe(ctx, path)
	if err != nil {
		slog.WarnContext(ctx, "media probe failed", "file", path, "error", err)
		return nil, err
	}
	return info, nil
}

// verdict verifies one probed source file against what the release claimed and
// what the profile allows, returning the reasons to hold it. always_ask adds
// its own reason only when nothing else objected — it needs no probe, so it
// holds even with ffmpeg off.
func (w *Worker) verdict(
	file string,
	info *ffmpeg.Info,
	probeErr error,
	runtimeMinutes uint16,
	qualityProfile string,
) []schema.HoldReason {
	cfg := config.Get()
	var allowedCodecs []string
	if profile, ok := config.ResolveQualityProfile(qualityProfile); ok {
		allowedCodecs = profile.AllowedCodecs
	}
	reasons := verifyFile(
		file,
		library.Parse(filepath.Base(file)),
		info,
		probeErr,
		uint32(runtimeMinutes),
		allowedCodecs,
		cfg.Library.Probe.MinDurationRatio,
	)
	if len(reasons) == 0 && cfg.Library.Probe.AlwaysAsk {
		reasons = append(reasons, schema.HoldReason{
			File:     file,
			Check:    "always_ask",
			Expected: "manual approval",
		})
	}
	return reasons
}

// hold stops the import and parks the record for a user decision. A hold is an
// outcome, not a failure: it returns nil so the attempt counter stays put.
func (w *Worker) hold(
	ctx context.Context,
	span trace.Span,
	rec *ent.DownloadRecord,
	reasons []schema.HoldReason,
) error {
	if err := w.db.HoldDownloadRecord(ctx, rec.ID, reasons); err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("hold record: %w", err))
	}
	slog.InfoContext(ctx, "import held for review",
		"download_record.id", rec.ID,
		"reasons", len(reasons),
		"check", reasons[0].Check)
	return nil
}

func (w *Worker) importMovieRecord(
	ctx context.Context,
	span trace.Span,
	rec *ent.DownloadRecord,
	libCfg config.LibraryConfig,
) error {
	m := rec.Edges.Movie
	span.SetAttributes(attribute.Int64("movie.id", int64(m.ID)))
	defer w.lockEntity(fmt.Sprintf("movie:%d", m.ID))()

	// A movie holds at most one media file: a grab arriving while one exists
	// either replaces it (record flagged via the manual-search toggle) or
	// fails terminally before any transfer happens.
	existing, err := w.db.ListMediaFilesByMovieID(ctx, m.ID)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("list movie files: %w", err))
	}
	if len(existing) > 0 {
		if !rec.ReplaceExisting {
			return otelx.RecordSpanError(span, ErrMovieHasFile)
		}
		if err := w.replaceMovieFiles(ctx, m.ID, existing); err != nil {
			return otelx.RecordSpanError(span, err)
		}
	}
	var probeInfo *ffmpeg.Info
	var probeErr error
	src, srcErr := library.ResolveMediaFile(rec.SavePath)
	if srcErr == nil {
		probeInfo, probeErr = w.probeSource(ctx, src)
	}
	if !rec.VerificationBypassed {
		reasons := w.verdict(src, probeInfo, probeErr, m.Runtime, m.QualityProfile)
		if len(reasons) > 0 {
			return w.hold(ctx, span, rec, reasons)
		}
	}

	imported, err := w.lib.ImportMovie(ctx, rec.SavePath, m, "")
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
			Probe:        probeInfo,
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
// a replace-flagged grab can re-import over them.
func (w *Worker) replaceMovieFiles(
	ctx context.Context,
	movieID uint32,
	files []*ent.MediaFile,
) error {
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
// replace-flagged grab can re-import over it.
func (w *Worker) replaceEpisodeFile(
	ctx context.Context,
	episodeID uint32,
	mf *ent.MediaFile,
) error {
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
	// Keyed by the show, not the episode: a season-pack record imports into
	// every episode of the show, so an episode key would let a pack run
	// alongside a single-episode record aimed at one of its files.
	defer w.lockEntity(fmt.Sprintf("tvshow:%d", show.ID))()

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
		return w.importSingleEpisode(ctx, span, rec, show, season.Number, ep, libCfg)
	}
	files, err := library.ListVideoFiles(rec.SavePath)
	if err != nil {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("list pack files: %w", err),
		)
	}
	if len(files) <= 1 {
		return w.importSingleEpisode(ctx, span, rec, show, season.Number, ep, libCfg)
	}

	// A pack is verified whole before anything moves: importing half of it and
	// then holding would leave the season split across two states.
	probed := make(map[string]*ffmpeg.Info, len(files))
	var reasons []schema.HoldReason
	for _, f := range files {
		info, err := w.probeSource(ctx, f)
		probed[f] = info
		if !rec.VerificationBypassed {
			reasons = append(reasons, w.verdict(
				f, info, err, show.Runtime, show.QualityProfile,
			)...)
		}
	}
	if len(reasons) > 0 {
		return w.hold(ctx, span, rec, reasons)
	}

	matched, skippedExisting := 0, 0
	for _, f := range files {
		parsed := library.Parse(filepath.Base(f))
		tSeason, target := library.MatchEpisodeInSeason(
			parsed,
			show.Edges.Seasons,
			anime,
		)
		if target == nil {
			slog.WarnContext(ctx, "season pack file matched no episode",
				"file", filepath.Base(f), "tvshow.id", show.ID)
			continue
		}
		mf, err := w.db.FindMediaFileByEpisodeID(ctx, target.ID)
		if err != nil && !ent.IsNotFound(err) {
			slog.WarnContext(ctx, "season pack: media file lookup failed",
				"episode.id", target.ID, "error", err)
			continue
		}
		if mf != nil {
			if !rec.ReplaceExisting {
				skippedExisting++
				slog.InfoContext(
					ctx,
					"season pack: episode already has a file, skipping",
					"episode.id",
					target.ID,
					"file",
					filepath.Base(f),
				)
				continue
			}
			if rErr := w.replaceEpisodeFile(ctx, target.ID, mf); rErr != nil {
				slog.WarnContext(ctx, "season pack replace: clear existing failed",
					"episode.id", target.ID, "error", rErr)
				continue
			}
		}
		probeInfo := probed[f]
		imported, err := w.lib.ImportEpisode(ctx, f, show, tSeason, target)
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
					Probe:        probeInfo,
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
		if skippedExisting > 0 {
			return otelx.RecordSpanError(span, ErrEpisodeHasFile)
		}
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
	libCfg config.LibraryConfig,
) error {
	// An episode holds at most one media file: a grab arriving while one
	// exists either replaces it (record flagged via the manual-search toggle)
	// or fails terminally before any transfer happens.
	mf, err := w.db.FindMediaFileByEpisodeID(ctx, ep.ID)
	if err != nil && !ent.IsNotFound(err) {
		return otelx.RecordSpanError(span, fmt.Errorf("find episode file: %w", err))
	}
	if mf != nil {
		if !rec.ReplaceExisting {
			return otelx.RecordSpanError(span, ErrEpisodeHasFile)
		}
		if rErr := w.replaceEpisodeFile(ctx, ep.ID, mf); rErr != nil {
			return otelx.RecordSpanError(span, rErr)
		}
	}
	var probeInfo *ffmpeg.Info
	var probeErr error
	src, srcErr := library.ResolveMediaFile(rec.SavePath)
	if srcErr == nil {
		probeInfo, probeErr = w.probeSource(ctx, src)
	}
	if !rec.VerificationBypassed {
		reasons := w.verdict(
			src, probeInfo, probeErr, show.Runtime, show.QualityProfile,
		)
		if len(reasons) > 0 {
			return w.hold(ctx, span, rec, reasons)
		}
	}

	imported, err := w.lib.ImportEpisode(ctx, rec.SavePath, show, seasonNumber, ep)
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
				Probe:        probeInfo,
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
	//nolint:sloglint // LogAttrs takes slog.Attr by API design
	slog.LogAttrs(ctx, slog.LevelWarn, "import failed",
		slog.Int("record.id", int(rec.ID)),
		slog.Int("attempts", int(attempts)),
		slog.Bool("terminal", isTerminal),
		slog.String("error", runErr.Error()))
}
