package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// maxTorrentFileSize caps the pre-fetched .torrent payload at 16 MiB —
// well above any real-world metainfo file but small enough that a
// misbehaving indexer can't stream us out of memory.
const maxTorrentFileSize = 16 * 1024 * 1024

// Categorised download-client failures. Handlers map these to 422 with
// friendly messages; anything not matching is a 500 internal error.
var (
	ErrUnsupportedClient = errors.New("unsupported download client type")
	ErrUnreachable       = errors.New("download client unreachable")
	ErrUnauthorized      = errors.New("download client credentials rejected")
	ErrUnexpectedStatus  = errors.New("download client returned unexpected status")
	ErrBadResponse       = errors.New("download client returned malformed response")
	ErrTorrentNotFound   = errors.New("torrent not found in download client")
	// ErrTorrentAlreadyExists is returned by AddTorrent when the download
	// client refuses the add because the infohash is already present. The
	// caller treats this as a soft skip — no grab_failures increment, no
	// new DownloadRecord — since state has drifted, not a real failure.
	ErrTorrentAlreadyExists = errors.New("torrent already exists in download client")
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/download")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/download")

	grabCounter      metric.Int64Counter
	grabDuration     metric.Float64Histogram
	statusCheckCount metric.Int64Counter
	completedCount   metric.Int64Counter
	testCounter      metric.Int64Counter
	orphanCounter    metric.Int64Counter
)

func init() {
	grabCounter = otelx.Must(meter.Int64Counter(
		"streamline.download.grabs",
		metric.WithDescription("Number of torrent grab attempts"),
	))
	grabDuration = otelx.Must(meter.Float64Histogram(
		"streamline.download.grab.duration",
		metric.WithDescription("Torrent grab latency"),
		metric.WithUnit("s"),
	))
	statusCheckCount = otelx.Must(meter.Int64Counter(
		"streamline.download.status_checks",
		metric.WithDescription("Number of download status poll runs"),
	))
	completedCount = otelx.Must(meter.Int64Counter(
		"streamline.download.completed",
		metric.WithDescription(
			"Number of downloads transitioned to completed/importing",
		),
	))
	testCounter = otelx.Must(meter.Int64Counter(
		"streamline.download.tests",
		metric.WithDescription(
			"Download client connection-test invocations by outcome",
		),
	))
	orphanCounter = otelx.Must(meter.Int64Counter(
		"streamline.download.orphans_purged",
		metric.WithDescription("Number of orphaned download records purged"),
	))

	// Prime instruments with 0 so series appear in the backend before the
	// first real event.
	ctx := context.Background()
	grabCounter.Add(ctx, 0)
	statusCheckCount.Add(ctx, 0)
	completedCount.Add(ctx, 0)
	testCounter.Add(ctx, 0)
	orphanCounter.Add(ctx, 0)
	grabDuration.Record(ctx, 0)
}

// CompletedDownload pairs a finished download record with
// the local path where the torrent client saved the files.
type CompletedDownload struct {
	Record   *ent.DownloadRecord
	SavePath string
}

// Checker is the consumer-facing surface for polling completed downloads.
// jobs.DownloadMonitor accepts it so it can be driven by a fake in tests
// without pulling in the full Manager.
type Checker interface {
	CheckStatus(ctx context.Context) ([]CompletedDownload, error)
	ReconcileEpisodeStatuses(ctx context.Context) error
}

// Downloader is the consumer-facing surface used by HTTP handlers and the
// scheduler. Implemented by the unexported download struct.
type Downloader interface {
	Test(ctx context.Context, p TestParams) error
	TestByName(ctx context.Context, name string) error
	Grab(
		ctx context.Context,
		result indexer.SearchResult,
		movieID uint32,
	) (*ent.DownloadRecord, error)
	GrabEpisode(
		ctx context.Context,
		result indexer.SearchResult,
		episodeID uint32,
	) (*ent.DownloadRecord, error)
	CheckStatus(ctx context.Context) ([]CompletedDownload, error)
	ReconcileEpisodeStatuses(ctx context.Context) error
	RemoveTorrent(
		ctx context.Context,
		downloadClientName string,
		torrentHash string,
	) error
	Queue(ctx context.Context) (QueueSnapshot, error)
	CancelQueueItem(ctx context.Context, recordID uint32) error
	PauseQueueItem(ctx context.Context, recordID uint32) error
	ResumeQueueItem(ctx context.Context, recordID uint32) error
}

const (
	// completedRecordRetention is how long completed download_records (with
	// imported_at set) are kept before cleanup deletes them.
	completedRecordRetention = 30 * 24 * time.Hour
	// failedRecordRetention is how long failed download_records are kept.
	failedRecordRetention = 14 * 24 * time.Hour
	// orphanGrace is how long a "downloading" record is spared before a
	// not-found torrent in the client is treated as orphaned — guards a
	// record grabbed shortly before a cleanup tick.
	orphanGrace = 1 * time.Hour
	// monitorOrphanGrace is the equivalent for the frequent download-monitor
	// pass: short enough to clear a download cancelled in the client within a
	// couple of minutes, long enough to ride out the gap between grabbing a
	// torrent and the client listing it.
	monitorOrphanGrace = 2 * time.Minute
)

// Cleaner is the consumer-facing surface for the cleanup scheduler job
// (jobs.Cleanup).
type Cleaner interface {
	PurgeOldRecords(ctx context.Context) error
	PurgeOrphanedTorrents(ctx context.Context) error
}

// Adopter scans enabled download clients for untracked managed-category
// torrents and either auto-imports them (returning those record IDs for the
// caller to enqueue) or files a pending proposal. Driven by the download poll
// job after the completion pass.
type Adopter interface {
	AdoptManualTorrents(ctx context.Context) ([]uint32, error)
}

// download coordinates sending torrents to download clients and tracking
// their progress in the database.
type download struct {
	db db.Store

	qmu   sync.Mutex
	qSnap []QueueEntry
	qAt   time.Time
}

func New(store db.Store) Downloader {
	return &download{db: store}
}

const queueRefreshTTL = 2 * time.Second

// QueueEntry is one in-flight download enriched with live client telemetry.
type QueueEntry struct {
	RecordID     uint32
	Status       string // downloading | importing | paused | error
	Title        string
	Quality      string
	ReleaseGroup string
	Movie        *ent.Movie
	// Episode is set for TV download records (with season + show eager-loaded);
	// nil for movie records. Drives the "<show> · SxxExx" row title.
	Episode        *ent.Episode
	Indexer        string
	DownloadClient string
	Size           int64
	Progress       float64
	DownloadSpeed  int64
	ETA            int64
	FailureReason  string
	CreatedAt      time.Time
}

// QueueSnapshot is the cached live-queue view with its capture time.
type QueueSnapshot struct {
	Items       []QueueEntry
	RefreshedAt time.Time
}

// Queue returns the live download queue from a short-TTL cached snapshot.
// Concurrent callers collapse onto one refresh (double-checked under qmu);
// a failed refresh degrades to the last good snapshot instead of erroring.
func (d *download) Queue(ctx context.Context) (QueueSnapshot, error) {
	d.qmu.Lock()
	defer d.qmu.Unlock()

	if d.qAt.IsZero() || time.Since(d.qAt) >= queueRefreshTTL {
		snap, err := d.refreshQueue(ctx)
		if err != nil {
			if d.qAt.IsZero() {
				return QueueSnapshot{}, err
			}
			slog.WarnContext(ctx,
				"queue refresh failed; serving stale snapshot",
				"error", err, "stale.age", time.Since(d.qAt).String())
		} else {
			d.qSnap, d.qAt = snap, time.Now()
		}
	}
	return QueueSnapshot{Items: d.qSnap, RefreshedAt: d.qAt}, nil
}

func (d *download) refreshQueue(ctx context.Context) ([]QueueEntry, error) {
	ctx, span := tracer.Start(ctx, "download.queue")
	defer span.End()

	records, err := d.db.ListActiveDownloadRecords(ctx)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	entries := make([]QueueEntry, len(records))
	var wg sync.WaitGroup
	for i, rec := range records {
		entries[i] = baseQueueEntry(rec)
		dc, ok := config.FindDownloadClient(rec.DownloadClientName)
		if rec.Status != downloadrecord.StatusDownloading ||
			!ok || rec.TorrentHash == "" {
			continue
		}
		i, rec, dc := i, rec, dc
		wg.Go(func() {
			client, err := d.buildClient(dc)
			if err != nil {
				slog.WarnContext(ctx, "queue: build client failed",
					"client", dc.Name, "error", err)
				return
			}
			t, err := client.GetTorrent(ctx, rec.TorrentHash)
			if err != nil {
				if errors.Is(err, ErrTorrentNotFound) {
					slog.WarnContext(ctx,
						"queue: torrent gone from client",
						"hash", rec.TorrentHash, "error", err)
				} else {
					slog.DebugContext(ctx,
						"queue: get torrent failed",
						"hash", rec.TorrentHash, "error", err)
				}
				return
			}
			entries[i].Progress = t.Progress
			entries[i].DownloadSpeed = t.DownloadSpeed
			entries[i].ETA = t.ETA
			entries[i].Status = liveQueueStatus(t.Status)
		})
	}
	wg.Wait()
	sort.SliceStable(entries, func(a, b int) bool {
		return entries[a].CreatedAt.After(entries[b].CreatedAt)
	})
	return entries, nil
}

func baseQueueEntry(rec *ent.DownloadRecord) QueueEntry {
	e := QueueEntry{
		RecordID:      rec.ID,
		Status:        "downloading",
		Title:         rec.Title,
		Quality:       rec.Quality,
		ReleaseGroup:  rec.ReleaseGroup,
		Movie:         rec.Edges.Movie,
		Episode:       rec.Edges.Episode,
		Size:          rec.Size,
		FailureReason: rec.FailureReason,
		CreatedAt:     rec.CreateTime,
	}
	if rec.Status == downloadrecord.StatusImporting {
		e.Status = "importing"
		e.Progress = 1.0
	}
	e.Indexer = rec.IndexerName
	e.DownloadClient = rec.DownloadClientName
	return e
}

func liveQueueStatus(s TorrentStatus) string {
	switch s {
	case StatusPaused:
		return "paused"
	case StatusError:
		return "error"
	default:
		return "downloading"
	}
}

// CancelQueueItem removes the torrent (and its partial files) from the
// client, deletes the record, and reverts the movie to "wanted" when it has
// no file. A NotFound record propagates so the handler can 404.
func (d *download) CancelQueueItem(ctx context.Context, recordID uint32) error {
	rec, err := d.db.FindActiveDownloadRecordByID(ctx, recordID)
	if err != nil {
		return err
	}
	if dc, ok := config.FindDownloadClient(
		rec.DownloadClientName,
	); ok &&
		rec.TorrentHash != "" {
		if client, berr := d.buildClient(dc); berr == nil {
			if rerr := client.RemoveTorrent(
				ctx, rec.TorrentHash, true); rerr != nil {
				slog.WarnContext(ctx, "cancel: remove torrent failed",
					"hash", rec.TorrentHash, "error", rerr)
			}
		}
	}
	if err := d.db.DeleteDownloadRecord(ctx, recordID); err != nil {
		return fmt.Errorf("delete download record: %w", err)
	}
	if m := rec.Edges.Movie; m != nil {
		if err := d.db.RevertMovieToWantedIfNoFile(ctx, m.ID); err != nil {
			return fmt.Errorf("revert movie: %w", err)
		}
	}
	return nil
}

func (d *download) PauseQueueItem(ctx context.Context, recordID uint32) error {
	return d.queueClientAction(ctx, recordID, Client.PauseTorrent)
}

func (d *download) ResumeQueueItem(ctx context.Context, recordID uint32) error {
	return d.queueClientAction(ctx, recordID, Client.ResumeTorrent)
}

// queueClientAction loads an in-flight record and applies a torrent-level
// client verb (pause/resume) to it. NotFound propagates for the handler 404.
func (d *download) queueClientAction(
	ctx context.Context,
	recordID uint32,
	fn func(Client, context.Context, string) error,
) error {
	rec, err := d.db.FindActiveDownloadRecordByID(ctx, recordID)
	if err != nil {
		return err
	}
	dc, ok := config.FindDownloadClient(rec.DownloadClientName)
	if !ok || rec.TorrentHash == "" {
		return fmt.Errorf("download record %d has no torrent", recordID)
	}
	client, err := d.buildClient(dc)
	if err != nil {
		return err
	}
	return fn(client, ctx, rec.TorrentHash)
}

// Grab picks the highest-priority enabled download client, sends the torrent,
// creates a DownloadRecord in "downloading" status, and flips the movie's status
// so UI + sync logic see the transition.
func (d *download) Grab(
	ctx context.Context,
	result indexer.SearchResult,
	movieID uint32,
) (*ent.DownloadRecord, error) {
	return d.grab(ctx, result, movieID, 0)
}

// GrabEpisode mirrors Grab for a TV episode. Episode status transitions are
// owned by the caller (the TV missing searcher), so this does not touch episode
// rows.
func (d *download) GrabEpisode(
	ctx context.Context,
	result indexer.SearchResult,
	episodeID uint32,
) (*ent.DownloadRecord, error) {
	return d.grab(ctx, result, 0, episodeID)
}

// grab is the shared torrent-grab path. Exactly one of movieID/episodeID is
// non-zero; it drives the span/log naming, which DownloadRecord field links the
// record, and (movies only) whether the Movie status is flipped.
func (d *download) grab(
	ctx context.Context,
	result indexer.SearchResult,
	movieID, episodeID uint32,
) (*ent.DownloadRecord, error) {
	spanName, mediaAttr := "download.grab", attribute.Int64(
		"movie.id",
		int64(movieID),
	)
	if episodeID != 0 {
		spanName, mediaAttr = "download.grab_episode", attribute.Int64(
			"episode.id",
			int64(episodeID),
		)
	}
	ctx, span := tracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String("release.title", result.Title),
			attribute.Int64("release.size", result.Size),
			mediaAttr,
		),
	)
	defer span.End()

	start := time.Now()
	outcome := "success"
	clientName := "unknown"
	defer func() {
		attrs := metric.WithAttributes(
			attribute.String("outcome", outcome),
			attribute.String("download_client.name", clientName),
		)
		grabDuration.Record(ctx, time.Since(start).Seconds(), attrs)
		grabCounter.Add(ctx, 1, attrs)
	}()

	dc, ok := config.PickDownloadClient()
	if !ok {
		outcome = "no_client"
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("no enabled download client configured"),
		)
	}
	clientName = dc.Name
	span.SetAttributes(
		attribute.String("download_client.name", dc.Name),
		attribute.String("download_client.type", dc.ClientType),
	)

	client, err := d.buildClient(dc)
	if err != nil {
		outcome = "build_client_failed"
		return nil, otelx.RecordSpanError(span, err)
	}

	src, err := resolveTorrentSource(ctx, result.Download)
	if err != nil {
		outcome = "fetch_torrent_failed"
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("fetch torrent: %w", err),
		)
	}
	hash, err := client.AddTorrent(ctx, src)
	if err != nil {
		if errors.Is(err, ErrTorrentAlreadyExists) {
			outcome = "already_exists"
			return nil, otelx.RecordSpanError(span, err)
		}
		outcome = "add_torrent_failed"
		return nil, otelx.RecordSpanError(span, fmt.Errorf("add torrent: %w", err))
	}
	span.SetAttributes(attribute.String("torrent.hash", hash))

	record, err := d.db.CreateDownloadRecord(ctx, db.CreateDownloadRecordParams{
		Title:              result.Title,
		Size:               result.Size,
		TorrentHash:        hash,
		Status:             downloadrecord.StatusDownloading,
		MovieID:            movieID,
		EpisodeID:          episodeID,
		DownloadClientName: dc.Name,
		IndexerName:        result.Indexer,
	})
	if err != nil {
		outcome = "db_record_failed"
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("create download record: %w", err),
		)
	}

	if movieID != 0 {
		if err := d.db.UpdateMovieStatus(
			ctx,
			movieID,
			movie.StatusDownloading,
		); err != nil {
			outcome = "movie_update_failed"
			return nil, otelx.RecordSpanError(
				span,
				fmt.Errorf("update movie status: %w", err),
			)
		}
	}

	logAttrs := []any{"title", result.Title, "hash", hash, "client", dc.Name}
	if episodeID != 0 {
		logAttrs = append(logAttrs, "episode.id", episodeID)
	}
	slog.InfoContext(ctx, "torrent grabbed", logAttrs...)
	return record, nil
}

// CheckStatus polls download clients for all "downloading" records
// and returns any that have completed.
func (d *download) CheckStatus(ctx context.Context) ([]CompletedDownload, error) {
	ctx, span := tracer.Start(ctx, "download.check_status")
	defer span.End()

	records, err := d.db.ListDownloadingRecordsWithMovie(ctx)
	if err != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("query downloading records: %w", err),
		)
	}
	span.SetAttributes(attribute.Int("tracked.count", len(records)))

	var (
		mu        sync.Mutex
		completed []CompletedDownload
		wg        sync.WaitGroup
	)

	for _, record := range records {
		dc, ok := config.FindDownloadClient(record.DownloadClientName)
		if !ok {
			slog.WarnContext(ctx,
				"download client missing for active record",
				"record.id", record.ID,
				"client", record.DownloadClientName)
			continue
		}

		wg.Go(func() {
			client, err := d.buildClient(dc)
			if err != nil {
				slog.WarnContext(ctx,
					"failed to build client",
					"client", dc.Name,
					"error", err,
				)
				return
			}

			torrent, err := client.GetTorrent(ctx, record.TorrentHash)
			if err != nil {
				switch {
				case errors.Is(err, ErrTorrentNotFound) &&
					time.Since(record.CreateTime) >= monitorOrphanGrace:
					// Cancelled/removed in the client: drop the orphaned record
					// so its media reverts instead of being stuck "downloading".
					d.purgeOrphanedRecord(ctx, record)
				case errors.Is(err, ErrTorrentNotFound):
					slog.WarnContext(ctx,
						"torrent gone from client (within grace)",
						"hash", record.TorrentHash)
				default:
					slog.DebugContext(ctx,
						"failed to get torrent status",
						"hash", record.TorrentHash, "error", err)
				}
				return
			}

			if torrent.Status != StatusSeeding && torrent.Status != StatusCompleted {
				// Still in flight: mirror the torrent's paused state onto the
				// episode badges (paused vs downloading) so the UI reflects it.
				if serr := d.db.SyncSeasonDownloadStateForRecord(
					ctx, record.ID, torrent.Status == StatusPaused,
				); serr != nil {
					slog.WarnContext(ctx, "sync paused episode state failed",
						"record.id", record.ID, "error", serr)
				}
				return
			}

			err = d.db.UpdateDownloadRecordStatus(
				ctx,
				record.ID,
				downloadrecord.StatusImporting,
			)
			if err != nil {
				slog.WarnContext(ctx,
					"failed to update record",
					"id", record.ID,
					"error", err,
				)
				return
			}
			contentPath := filepath.Join(
				config.Get().Library.DownloadPath, torrent.Name,
			)
			if err := d.db.SetDownloadRecordSavePath(
				ctx,
				record.ID,
				contentPath,
			); err != nil {
				slog.WarnContext(
					ctx,
					"persist save_path failed",
					"id",
					record.ID,
					"error",
					err,
				)
			}

			mu.Lock()
			completed = append(completed, CompletedDownload{
				Record:   record,
				SavePath: contentPath,
			})
			mu.Unlock()
		})
	}

	wg.Wait()
	span.SetAttributes(attribute.Int("completed.count", len(completed)))
	statusCheckCount.Add(ctx, 1)
	if len(completed) > 0 {
		completedCount.Add(ctx, int64(len(completed)))
	}
	return completed, nil
}

// purgeOrphanedRecord drops a "downloading" record whose torrent has vanished
// from the client (cancelled out-of-band) and reverts its movie to wanted.
// Episode records are reconciled by ReconcileEpisodeStatuses instead, which
// also covers the season-pack siblings a single record can't reach.
func (d *download) purgeOrphanedRecord(
	ctx context.Context,
	rec *ent.DownloadRecord,
) {
	slog.WarnContext(ctx,
		"purging orphaned download record; torrent gone from client",
		"record.id", rec.ID, "hash", rec.TorrentHash)
	if err := d.db.DeleteDownloadRecord(ctx, rec.ID); err != nil {
		slog.WarnContext(ctx, "purge orphan: delete record failed",
			"record.id", rec.ID, "error", err)
		return
	}
	if m := rec.Edges.Movie; m != nil {
		if err := d.db.RevertMovieToWantedIfNoFile(ctx, m.ID); err != nil {
			slog.WarnContext(ctx, "purge orphan: revert movie failed",
				"movie.id", m.ID, "error", err)
		}
	}
}

// ReconcileEpisodeStatuses reverts episodes stranded in "downloading" with no
// active download record — chiefly the season-pack fan-out left behind when a
// pack's single record is cancelled or lost. Runs on the download-monitor tick
// so stuck rows self-heal rather than requiring a manual reset.
func (d *download) ReconcileEpisodeStatuses(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "download.reconcile_episode_statuses")
	defer span.End()

	n, err := d.db.RevertOrphanedDownloadingEpisodes(ctx)
	if err != nil {
		return otelx.RecordSpanError(
			span, fmt.Errorf("revert orphaned downloading episodes: %w", err),
		)
	}
	span.SetAttributes(attribute.Int("episodes.reverted", n))
	if n > 0 {
		slog.InfoContext(ctx, "reconciled stranded downloading episodes",
			"reverted", n)
	}
	return nil
}

// PurgeOldRecords deletes completed records past completedRecordRetention and
// failed records past failedRecordRetention. Both deletes run independently;
// one failing does not block the other. Errors are joined.
func (d *download) PurgeOldRecords(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "download.purge_old_records")
	defer span.End()

	now := time.Now()
	compN, compErr := d.db.DeleteCompletedDownloadRecordsBefore(
		ctx, now.Add(-completedRecordRetention),
	)
	failN, failErr := d.db.DeleteFailedDownloadRecordsBefore(
		ctx, now.Add(-failedRecordRetention),
	)

	span.SetAttributes(attribute.Int("cleanup.deleted_count", compN+failN))
	if total := compN + failN; total > 0 {
		slog.InfoContext(ctx, "cleanup deleted download records",
			"completed", compN, "failed", failN)
	}
	return errors.Join(compErr, failErr)
}

// PurgeOrphanedTorrents deletes "downloading" records whose torrent is no
// longer in the client (gone out-of-band) and older than orphanGrace,
// reverting the movie to "wanted" so it can be re-grabbed. Transient
// client errors never delete a record. Only a failure to list records is
// returned; per-record failures are logged and skipped.
func (d *download) PurgeOrphanedTorrents(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "download.purge_orphaned_torrents")
	defer span.End()

	records, err := d.db.ListDownloadingRecordsWithMovie(ctx)
	if err != nil {
		return otelx.RecordSpanError(
			span, fmt.Errorf("list downloading records: %w", err),
		)
	}

	purged := 0
	for _, rec := range records {
		dc, ok := config.FindDownloadClient(rec.DownloadClientName)
		if !ok || rec.TorrentHash == "" {
			continue
		}
		client, err := d.buildClient(dc)
		if err != nil {
			slog.DebugContext(ctx, "orphan scan: build client failed",
				"client", dc.Name, "error", err)
			continue
		}
		if _, err := client.GetTorrent(ctx, rec.TorrentHash); err != nil {
			if !errors.Is(err, ErrTorrentNotFound) {
				slog.DebugContext(ctx, "orphan scan: get torrent failed",
					"hash", rec.TorrentHash, "error", err)
				continue
			}
			if time.Since(rec.CreateTime) < orphanGrace {
				continue
			}
			slog.WarnContext(ctx,
				"orphaned download record; torrent gone from client",
				"record.id", rec.ID, "hash", rec.TorrentHash)
			if err := d.db.DeleteDownloadRecord(ctx, rec.ID); err != nil {
				slog.WarnContext(ctx, "orphan scan: delete record failed",
					"record.id", rec.ID, "error", err)
				continue
			}
			if m := rec.Edges.Movie; m != nil {
				if err := d.db.RevertMovieToWantedIfNoFile(
					ctx, m.ID,
				); err != nil {
					slog.WarnContext(ctx, "orphan scan: revert movie failed",
						"movie.id", m.ID, "error", err)
				}
			}
			purged++
		}
	}

	span.SetAttributes(attribute.Int("orphans.purged", purged))
	if purged > 0 {
		orphanCounter.Add(ctx, int64(purged))
		slog.InfoContext(ctx, "orphan scan purged records",
			"count", purged)
	}
	return nil
}

// RemoveTorrent wraps the download client's remove call. Used by importer.Worker
// when KeepTorrentSeeding=false after a successful import. Files are never
// deleted from the client's side — the library already holds the copy/hardlink
// and the torrent contents are the source.
func (d *download) RemoveTorrent(
	ctx context.Context,
	clientName string,
	hash string,
) error {
	ctx, span := tracer.Start(ctx, "download.remove_torrent",
		trace.WithAttributes(attribute.String("torrent.hash", hash)))
	defer span.End()

	dc, ok := config.FindDownloadClient(clientName)
	if !ok {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("download client %q not found", clientName),
		)
	}
	client, err := d.buildClient(dc)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	return client.RemoveTorrent(ctx, hash, false)
}

// buildBaseURL composes scheme://host:port for download client requests.
func buildBaseURL(host string, port uint16, useSSL bool) string {
	scheme := "http"
	if useSSL {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

// resolveTorrentSource turns an indexer download link into the payload
// Client.AddTorrent expects. magnet: links pass through; http(s) URLs are
// fetched in-process so download clients that can't reach the indexer
// (Docker/VPN sandboxes) still get the .torrent bytes.
func resolveTorrentSource(ctx context.Context, dl string) (TorrentSource, error) {
	if dl == "" {
		return TorrentSource{}, fmt.Errorf("empty download URL")
	}
	if strings.HasPrefix(dl, "magnet:") {
		return TorrentSource{Magnet: dl}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dl, nil)
	if err != nil {
		return TorrentSource{}, err
	}
	resp, err := otelx.HTTPClient.Do(req)
	if err != nil {
		return TorrentSource{}, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return TorrentSource{}, fmt.Errorf(
			"indexer returned status %d for %s", resp.StatusCode, dl,
		)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTorrentFileSize+1))
	if err != nil {
		return TorrentSource{}, fmt.Errorf("read torrent body: %w", err)
	}
	if int64(len(body)) > maxTorrentFileSize {
		return TorrentSource{}, fmt.Errorf(
			"torrent file exceeds %d byte cap", maxTorrentFileSize,
		)
	}
	if len(body) == 0 {
		return TorrentSource{}, fmt.Errorf("indexer returned empty body")
	}
	return TorrentSource{Bytes: body}, nil
}

// buildClient creates a download.Client from a config download-client entry.
// Transmission (HTTP Basic) and Deluge (Web UI password) authenticate by
// password only; qBittorrent additionally supports an API key.
func (d *download) buildClient(dc config.DownloadClientEntry) (Client, error) {
	baseURL := buildBaseURL(dc.Host, dc.Port, dc.UseSSL)
	switch dc.ClientType {
	case "qbittorrent":
		switch dc.AuthMethod {
		case "api_key":
			return NewQBittorrentAPIKey(
				baseURL,
				config.SecretValue(dc.APIKey, dc.APIKeyFile),
			), nil
		default:
			return NewQBittorrentPassword(
				baseURL,
				dc.Username,
				config.SecretValue(dc.Password, dc.PasswordFile),
			), nil
		}
	case "transmission":
		return NewTransmission(
			baseURL,
			dc.Username,
			config.SecretValue(dc.Password, dc.PasswordFile),
		), nil
	case "deluge":
		return NewDeluge(
			baseURL,
			config.SecretValue(dc.Password, dc.PasswordFile),
		), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedClient, dc.ClientType)
	}
}

// TestParams describes ad-hoc credentials for a connection test that has
// not yet been persisted as a config download-client entry.
type TestParams struct {
	ClientType string
	Host       string
	Port       uint16
	UseSSL     bool
	AuthMethod string
	Username   string
	Password   string
	APIKey     string
}

// Test runs a connection check against the supplied params without
// touching the database. Returns ErrUnsupportedClient when the type isn't
// implemented or one of the typed transport errors when the probe fails.
func (d *download) Test(ctx context.Context, p TestParams) error {
	ctx, span := tracer.Start(ctx, "download.test",
		trace.WithAttributes(
			attribute.String("download_client.type", p.ClientType),
			attribute.String("download_client.host", p.Host),
		),
	)
	defer span.End()

	record := func(outcome string) {
		testCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("client_type", p.ClientType),
			attribute.String("outcome", outcome),
		))
	}

	client, err := d.buildClient(config.DownloadClientEntry{
		ClientType: p.ClientType,
		Host:       p.Host,
		Port:       p.Port,
		UseSSL:     p.UseSSL,
		AuthMethod: p.AuthMethod,
		Username:   p.Username,
		Password:   p.Password,
		APIKey:     p.APIKey,
	})
	if err != nil {
		record("unsupported")
		return otelx.RecordSpanError(span, err)
	}
	if err := client.TestConnection(ctx); err != nil {
		record("error")
		return otelx.RecordSpanError(span, err)
	}
	record("success")
	return nil
}

// TestByName loads the named download client from config and runs Test
// against its credentials. Returns ErrDownloadClientNotFound when the entry
// is missing.
func (d *download) TestByName(ctx context.Context, name string) error {
	ctx, span := tracer.Start(ctx, "download.test_by_name",
		trace.WithAttributes(attribute.String("download_client.name", name)),
	)
	defer span.End()

	dc, ok := config.FindDownloadClient(name)
	if !ok {
		return otelx.RecordSpanError(span, config.ErrDownloadClientNotFound)
	}
	return d.Test(ctx, TestParams{
		ClientType: dc.ClientType,
		Host:       dc.Host,
		Port:       dc.Port,
		UseSSL:     dc.UseSSL,
		AuthMethod: dc.AuthMethod,
		Username:   dc.Username,
		Password:   config.SecretValue(dc.Password, dc.PasswordFile),
		APIKey:     config.SecretValue(dc.APIKey, dc.APIKeyFile),
	})
}
