package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// MaxTorrentFileSize caps a .torrent payload at 16 MiB — well above any
// real-world metainfo file but small enough that a misbehaving indexer can't
// stream us out of memory. Exported because the same ceiling has to hold for a
// .torrent an operator uploads through the API, which never passes through the
// fetch path below.
const MaxTorrentFileSize = 16 * 1024 * 1024

// Categorised download-client failures. Handlers map these to 422 with
// friendly messages; anything not matching is a 500 internal error.
var (
	ErrUnsupportedClient = errors.New("unsupported download client type")
	ErrUnreachable       = errors.New("download client unreachable")
	ErrUnauthorized      = errors.New("download client credentials rejected")
	ErrUnexpectedStatus  = errors.New("download client returned unexpected status")
	ErrBadResponse       = errors.New("download client returned malformed response")
	ErrTorrentNotFound   = errors.New("torrent not found in download client")
	ErrUntrustedSource   = errors.New(
		"download URL does not belong to a configured indexer",
	)
	// ErrTorrentAlreadyExists is returned by AddTorrent when the download
	// client refuses the add because the infohash is already present. The
	// caller treats this as a soft skip — no grab_failures increment, no
	// new DownloadRecord — since state has drifted, not a real failure.
	ErrTorrentAlreadyExists = errors.New("torrent already exists in download client")
	// ErrUnsafeTorrentName is returned when a client-supplied torrent name
	// would place the save path outside the configured download directory.
	ErrUnsafeTorrentName = errors.New(
		"torrent name escapes the download path",
	)
	// ErrRecordHeld is returned by the queue verbs for a record parked by
	// import verification. Its download is finished, so pause/resume/cancel
	// have nothing to act on — POST /downloads/{id}/resolve owns it.
	ErrRecordHeld = errors.New("download record is held for review")
	// ErrNotSupported is returned by ListFiles/SetWantedFiles on a download
	// client that has no notion of per-file selection.
	ErrNotSupported = errors.New("file selection not supported")
	// ErrNoWantedFiles is returned by grab when a selective download's
	// keep-set resolves to zero matched episodes — the release's file names
	// share nothing with the wanted episodes, so grabbing it would waste
	// bandwidth on a torrent that can never fill the gap. Raised before the
	// download client is contacted.
	ErrNoWantedFiles = errors.New("release contains no wanted episode file")
)

// PathUnderRoot reports whether path resolves inside root, or is root itself.
// The trailing separator is what makes it a containment test rather than a
// string prefix: without it a root of "/downloads" also matches
// "/downloads-evil".
func PathUnderRoot(path, root string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	return absPath == absRoot ||
		strings.HasPrefix(absPath, absRoot+string(filepath.Separator))
}

// downloadSavePath joins a client-supplied torrent name onto the configured
// download path. The name is attacker-controlled all the way from the tracker
// and the result is later used as an import *source*, so both the name and the
// joined result are checked: filepath.Join cleans as it joins, so a name like
// "../../etc" would otherwise collapse the download root away entirely.
func downloadSavePath(name string) (string, error) {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("%w: %q", ErrUnsafeTorrentName, name)
	}
	root := config.Get().Library.DownloadPath
	path := filepath.Join(root, name)
	if !PathUnderRoot(path, root) {
		return "", fmt.Errorf("%w: %q", ErrUnsafeTorrentName, name)
	}
	return path, nil
}

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

// SelectionResolver is the consumer-facing surface for the file_selection
// scheduler job: resolving magnet-sourced selective grabs (selection_state
// pending) once the client reports the torrent's file list, or giving up
// past the selection grace window.
// The count it returns is how many pending records the pass found, which is
// what lets the job pace itself instead of polling at magnet-resolution speed
// on an install with no magnet in flight.
type SelectionResolver interface {
	RunSelectionPass(ctx context.Context) (int, error)
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
		wantedEpisodes []uint32,
	) (*ent.DownloadRecord, error)
	CheckStatus(ctx context.Context) ([]CompletedDownload, error)
	ReconcileEpisodeStatuses(ctx context.Context) error
	RemoveTorrent(
		ctx context.Context,
		downloadClientName string,
		torrentHash string,
		deleteFiles bool,
	) error
	PurgeRecordForHash(ctx context.Context, torrentHash string) error
	ListTorrentFiles(
		ctx context.Context,
		downloadClientName string,
		torrentHash string,
	) ([]TorrentFile, error)
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
	db      db.Store
	builtin Client // engine for client_type "builtin"; nil when not configured

	qmu   sync.Mutex
	qSnap []QueueEntry
	qAt   time.Time
}

// New builds the download manager. builtin may be nil (no engine configured);
// callers must pass an untyped nil, not a typed-nil concrete pointer.
func New(store db.Store, builtin Client) Downloader {
	return &download{db: store, builtin: builtin}
}

const queueRefreshTTL = 2 * time.Second

// QueueEntry is one in-flight download enriched with live client telemetry.
type QueueEntry struct {
	RecordID uint32
	Status   string // downloading | importing | paused | error | held
	// HoldReasons is set only on a held entry: the checks import verification
	// failed, which is what the resolve dialog shows.
	HoldReasons  []schema.HoldReason
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
	// SelectionState mirrors download_record.selection_state.
	SelectionState string
	// SelectedFiles is the kept indexes when SelectionState is "applied"; nil
	// otherwise.
	SelectedFiles []int
	SelectedBytes int64
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
		RecordID:       rec.ID,
		Status:         "downloading",
		Title:          rec.Title,
		Quality:        rec.Quality,
		ReleaseGroup:   rec.ReleaseGroup,
		Movie:          rec.Edges.Movie,
		Episode:        rec.Edges.Episode,
		Size:           rec.Size,
		FailureReason:  rec.FailureReason,
		CreatedAt:      rec.CreateTime,
		SelectionState: string(rec.SelectionState),
		SelectedFiles:  rec.SelectedFiles,
		SelectedBytes:  rec.SelectedBytes,
	}
	switch rec.Status {
	case downloadrecord.StatusImporting:
		e.Status = "importing"
		e.Progress = 1.0
	case downloadrecord.StatusHeld:
		e.Status = "held"
		e.Progress = 1.0
		e.HoldReasons = rec.HoldReasons
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
// no file. A NotFound record propagates so the handler can 404, a held one
// yields ErrRecordHeld so it can 409.
func (d *download) CancelQueueItem(ctx context.Context, recordID uint32) error {
	rec, err := d.db.FindActiveDownloadRecordByID(ctx, recordID)
	if err != nil {
		return err
	}
	if rec.Status == downloadrecord.StatusHeld {
		return ErrRecordHeld
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
// client verb (pause/resume) to it. NotFound propagates for the handler 404,
// ErrRecordHeld for its 409.
func (d *download) queueClientAction(
	ctx context.Context,
	recordID uint32,
	fn func(Client, context.Context, string) error,
) error {
	rec, err := d.db.FindActiveDownloadRecordByID(ctx, recordID)
	if err != nil {
		return err
	}
	if rec.Status == downloadrecord.StatusHeld {
		return ErrRecordHeld
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
	return d.grab(ctx, result, movieID, 0, nil)
}

// GrabEpisode mirrors Grab for a TV episode. Episode status transitions are
// owned by the caller (the TV missing searcher), so this does not touch episode
// rows. wantedEpisodes is the set of episode IDs this release is grabbed to
// fill/replace — nil or empty means non-selective (grab everything). It
// gates the Flow A keep-set resolution in grab.
func (d *download) GrabEpisode(
	ctx context.Context,
	result indexer.SearchResult,
	episodeID uint32,
	wantedEpisodes []uint32,
) (*ent.DownloadRecord, error) {
	return d.grab(ctx, result, 0, episodeID, wantedEpisodes)
}

// pendingSelection carries Flow A's resolved keep-set from resolveTorrentSource
// through CreateDownloadRecord to the post-add SetWantedFiles confirmation.
type pendingSelection struct {
	files []int
	bytes int64
}

// grab is the shared torrent-grab path. Exactly one of movieID/episodeID is
// non-zero; it drives the span/log naming, which DownloadRecord field links the
// record, and (movies only) whether the Movie status is flipped.
func (d *download) grab(
	ctx context.Context,
	result indexer.SearchResult,
	movieID, episodeID uint32,
	wantedEpisodes []uint32,
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

	if err := checkReleaseSource(result.Download); err != nil {
		outcome = "untrusted_source"
		return nil, otelx.RecordSpanError(span, err)
	}

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

	// spec §4.6: a re-grab landing on a hash already tracked by a live record
	// means "I also want these episodes", not "add a duplicate torrent" —
	// checked before any client contact, and before Flow A's own decode runs,
	// since a widen recomputes the keep-set itself over the union.
	//
	// The whole block is behind the feature flag, not just the widen arm: with
	// selective_files off there is nothing to widen, and the pre-check's own
	// duplicate rejection is behavior this feature introduced. Off has to be
	// bit-for-bit the pre-feature path, where AddTorrent is what detects a
	// duplicate hash.
	if localHash := localInfoHash(src); config.Get().Download.SelectiveFiles &&
		localHash != "" {
		live, lerr := d.db.FindWidenableDownloadRecordByHash(ctx, localHash)
		if lerr != nil {
			// Not fatal — the pre-check is an optimization and AddTorrent still
			// rejects a genuine duplicate — but silence here looks exactly like
			// "no record found", which is the wrong conclusion to reach quietly.
			slog.DebugContext(ctx,
				"grab: widenable record lookup failed; whole-torrent grab",
				"hash", localHash, "error", lerr)
		}
		if live != nil {
			// Widening only ever applies to a record Flow A already put through
			// selective resolution (pending/applied) with something new actually
			// being asked for — every other combination (a plain whole-torrent
			// grab, an unsupported/skipped client) is an ordinary duplicate hit.
			// Gating on selection_state rather than "has WantedEpisodes" is what
			// keeps a record whose client never resolved a selection (its state
			// is skipped or unsupported) from reaching widenSelection at all;
			// GrabEpisode writes WantedEpisodes unconditionally, so that field
			// says nothing about whether a keep-set exists to widen.
			if len(wantedEpisodes) > 0 &&
				(live.SelectionState == downloadrecord.SelectionStatePending ||
					live.SelectionState == downloadrecord.SelectionStateApplied) {
				rec, werr := d.widenSelection(ctx, live, wantedEpisodes)
				switch {
				case werr == nil:
					outcome = "widened"
				case errors.Is(werr, ErrTorrentAlreadyExists):
					outcome = "already_exists"
				default:
					outcome = "widen_failed"
				}
				return rec, werr
			}
			if live.Status != downloadrecord.StatusCompleted {
				outcome = "already_exists"
				return nil, otelx.RecordSpanError(span, ErrTorrentAlreadyExists)
			}
			// Completed and not widen-eligible: FindWidenableDownloadRecordByHash
			// matches it but it is not "live" for dedupe purposes — see that
			// function's doc. Fall through to a normal grab.
		}
	}

	// Flow A (spec §4.2): resolve which files the release actually needs to
	// serve wantedEpisodes before the torrent is ever added. Decode/lookup
	// errors fall through to a whole-torrent grab — selection is an
	// optimization, never the reason a grab fails; only a zero match is (it
	// means the release cannot fill the gap at all, so grabbing it wastes
	// bandwidth with zero client contact).
	var selection *pendingSelection
	selectivePending := false
	if config.Get().Download.SelectiveFiles && len(wantedEpisodes) > 0 {
		// A client that can resolve a magnet's metainfo without admitting the
		// torrent (spec §4.2) lets a selective magnet grab take the exact
		// .torrent path below instead of the pending fallback. Prefetch failure
		// is not fatal — it's the same "fall through to today's behavior" rule
		// as a decode failure just below.
		if len(src.Bytes) == 0 && src.Magnet != "" {
			if fetcher, ok := client.(MagnetMetadataFetcher); ok {
				fetched, ferr := fetcher.FetchMagnetMetadata(ctx, src.Magnet)
				if ferr != nil {
					slog.WarnContext(ctx,
						"grab: magnet metadata prefetch failed; pending selection",
						"title", result.Title, "error", ferr)
				} else {
					src.Bytes, src.Magnet = fetched, ""
				}
			}
		}
		switch {
		case len(src.Bytes) > 0:
			files, ferr := decodeTorrentFiles(src.Bytes)
			if ferr != nil {
				slog.DebugContext(
					ctx,
					"grab: decode torrent files failed; whole-torrent grab",
					"title",
					result.Title,
					"error",
					ferr,
				)
			} else {
				show, serr := d.db.TVShowForEpisode(ctx, episodeID)
				if serr != nil {
					slog.DebugContext(
						ctx,
						"grab: tv show lookup failed; whole-torrent grab",
						"episode.id",
						episodeID,
						"error",
						serr,
					)
				} else {
					keep, keptBytes, matched := computeKeepSet(
						files, show.Edges.Seasons, show.Type == tvshow.TypeAnime,
						wantedEpisodes,
					)
					videoTotal := countVideoCandidates(files)
					switch {
					case matched == 0:
						outcome = "no_wanted_files"
						return nil, otelx.RecordSpanError(span, fmt.Errorf(
							"%w: %s", ErrNoWantedFiles, result.Title))
					case len(keep) == len(files) || matched == videoTotal:
						// Every candidate serves a wanted episode: not selective.
					default:
						src.WantedFiles, src.Selective = keep, true
						selection = &pendingSelection{files: keep, bytes: keptBytes}
					}
				}
			}
		default:
			// Magnet: no bytes to inspect until the client resolves metadata,
			// so the keep-set can't be computed here. Mark the grab selective
			// (so no client starts pulling everything before that resolves)
			// and park the record pending — RunSelectionPass (spec §4.5) owns
			// the keep-set once the client's ListFiles has something to report.
			src.Selective = true
			selectivePending = true
		}
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

	recordParams := db.CreateDownloadRecordParams{
		Title:              result.Title,
		Size:               result.Size,
		TorrentHash:        hash,
		Status:             downloadrecord.StatusDownloading,
		MovieID:            movieID,
		EpisodeID:          episodeID,
		DownloadClientName: dc.Name,
		IndexerName:        result.Indexer,
		WantedEpisodes:     wantedEpisodes,
	}
	switch {
	case selection != nil:
		recordParams.SelectionState = downloadrecord.SelectionStateApplied
		recordParams.SelectedFiles = selection.files
		recordParams.SelectedBytes = selection.bytes
	case selectivePending:
		recordParams.SelectionState = downloadrecord.SelectionStatePending
	}
	record, err := d.db.CreateDownloadRecord(ctx, recordParams)
	if err != nil {
		outcome = "db_record_failed"
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("create download record: %w", err),
		)
	}

	// Uniform post-add confirmation (spec §4.4). What this call *is* differs by
	// client: for qBittorrent it is the application itself (its API only takes
	// a file selection after the torrent is admitted), for the builtin engine,
	// Transmission and Deluge the selection already landed at add time and this
	// re-set is idempotent. ErrNotSupported flips the record so the SPA stops
	// promising a selective download the client can't honour; any other error
	// leaves it applied, with the keep-set the create already stored — the
	// selection pass's confirmation and a later widen both correct it.
	if selection != nil {
		switch serr := client.SetWantedFiles(ctx, hash, selection.files); {
		case errors.Is(serr, ErrNotSupported):
			if uerr := d.db.SetDownloadRecordSelection(
				ctx, record.ID, downloadrecord.SelectionStateUnsupported, nil, 0,
			); uerr != nil {
				slog.WarnContext(ctx, "grab: mark selection unsupported failed",
					"record.id", record.ID, "error", uerr)
			}
		case serr != nil:
			slog.WarnContext(ctx, "grab: confirm selected files failed",
				"record.id", record.ID, "hash", hash, "error", serr)
		default:
			if uerr := d.db.SetDownloadRecordSelection(
				ctx, record.ID, downloadrecord.SelectionStateApplied,
				selection.files, selection.bytes,
			); uerr != nil {
				slog.WarnContext(ctx, "grab: confirm selection failed",
					"record.id", record.ID, "error", uerr)
			}
		}
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

// extractBtihFromMagnet pulls the btih infohash out of a magnet URI,
// lowercased. Used both as qBittorrent's pre-2.15 AddTorrent fallback (its
// envelope-less "Ok." response carries no hash) and by localInfoHash below.
func extractBtihFromMagnet(magnet string) string {
	parts := strings.SplitAfter(magnet, "btih:")
	if len(parts) < 2 {
		return ""
	}
	return strings.ToLower(strings.SplitN(parts[1], "&", 2)[0])
}

// localInfoHash derives a torrent's infohash from src without contacting the
// download client, lowercased to match how records store torrent_hash (every
// Client implementation lowercases the hash it returns from AddTorrent). A
// magnet's btih is read straight off the URI; bytes are parsed the same way
// qBittorrent's own pre-2.15 fallback does. A decode failure here yields ""
// rather than an error — like Flow A's own decode gate, the hash check is an
// optimization, never a reason to fail a grab that would otherwise succeed.
func localInfoHash(src TorrentSource) string {
	if src.Magnet != "" {
		return extractBtihFromMagnet(src.Magnet)
	}
	if len(src.Bytes) == 0 {
		return ""
	}
	mi, err := metainfo.Load(bytes.NewReader(src.Bytes))
	if err != nil {
		return ""
	}
	return strings.ToLower(mi.HashInfoBytes().HexString())
}

// unionEpisodes merges newIDs into existing, returning the deduplicated union
// (existing order preserved, new IDs appended) and the subset of newIDs not
// already present — the delta widenSelection marks downloading, since
// existing already ran that step at its original grab.
func unionEpisodes(existing, newIDs []uint32) (union, added []uint32) {
	seen := make(map[uint32]bool, len(existing)+len(newIDs))
	union = append(union, existing...)
	for _, id := range existing {
		seen[id] = true
	}
	for _, id := range newIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		union = append(union, id)
		added = append(added, id)
	}
	return union, added
}

// widenSelection handles a re-grab that landed on a hash a live selective
// record already tracks (spec §4.6): instead of a duplicate torrent, it
// folds wantedEpisodes into the record's tracked set and asks the client for
// whatever those episodes' files add to the keep-set. Never bumps
// grab_failures — the release was found and the client already has (or will
// have) the bytes, so nothing here is a failure the caller should count.
func (d *download) widenSelection(
	ctx context.Context,
	live *ent.DownloadRecord,
	wantedEpisodes []uint32,
) (*ent.DownloadRecord, error) {
	ctx, span := tracer.Start(ctx, "download.widen_selection",
		trace.WithAttributes(
			attribute.Int64("download_record.id", int64(live.ID)),
		),
	)
	defer span.End()

	union, added := unionEpisodes(live.WantedEpisodes, wantedEpisodes)

	dc, ok := config.FindDownloadClient(live.DownloadClientName)
	if !ok {
		return nil, otelx.RecordSpanError(span, fmt.Errorf(
			"download client %q not found", live.DownloadClientName,
		))
	}
	client, err := d.buildClient(dc)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}

	// Validate before writing anything: a client that cannot report a file
	// list cannot be widened at all, and that has to be known before any DB
	// write commits the wider intent. The entry gate in grab already excludes
	// any record whose selection_state isn't pending/applied, so
	// ErrNotSupported here means that state went stale against what the client
	// really supports; either way it is the same as an ordinary duplicate hit,
	// nothing to widen.
	clientFiles, err := client.ListFiles(ctx, live.TorrentHash)
	switch {
	case errors.Is(err, ErrNotSupported):
		return nil, otelx.RecordSpanError(span, ErrTorrentAlreadyExists)
	case err != nil:
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("list files: %w", err),
		)
	}

	if len(clientFiles) == 0 {
		// Metadata not yet available (e.g. a magnet still resolving) — the
		// phase-4 pending-selection pass finishes the job once it is.
		if err := d.db.SetDownloadRecordWantedEpisodes(
			ctx, live.ID, union,
		); err != nil {
			return nil, otelx.RecordSpanError(
				span, fmt.Errorf("widen wanted episodes: %w", err),
			)
		}
		live.WantedEpisodes = union
		if err := d.db.SetDownloadRecordSelection(
			ctx, live.ID, downloadrecord.SelectionStatePending, nil, 0,
		); err != nil {
			return nil, otelx.RecordSpanError(
				span, fmt.Errorf("mark selection pending: %w", err),
			)
		}
		live.SelectionState = downloadrecord.SelectionStatePending
		return live, nil
	}

	// wantedEpisodes is non-empty (the caller's gate), so its first ID always
	// names an episode on the show this record belongs to.
	show, err := d.db.TVShowForEpisode(ctx, wantedEpisodes[0])
	if err != nil {
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("tv show lookup: %w", err),
		)
	}
	files := make([]metaFile, len(clientFiles))
	for i, f := range clientFiles {
		files[i] = metaFile{Index: f.Index, Path: f.Path, Size: f.Size}
	}
	keep, keptBytes, matched := computeKeepSet(
		files, show.Edges.Seasons, show.Type == tvshow.TypeAnime, union,
	)
	if matched == 0 {
		// The union matches nothing in the client's actual file list — never
		// apply a keep-set that deselects every video file. Pre-feature
		// behavior: treat this exactly like an ordinary duplicate hit, and
		// commit nothing.
		slog.WarnContext(ctx, "widen: keep-set matched no wanted episode",
			"record.id", live.ID, "hash", live.TorrentHash,
			"files", len(files), "wanted_episodes", union)
		return nil, otelx.RecordSpanError(span, ErrTorrentAlreadyExists)
	}

	if err := d.db.SetDownloadRecordWantedEpisodes(
		ctx, live.ID, union,
	); err != nil {
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("widen wanted episodes: %w", err),
		)
	}
	live.WantedEpisodes = union

	switch serr := client.SetWantedFiles(ctx, live.TorrentHash, keep); {
	case errors.Is(serr, ErrNotSupported):
		if uerr := d.db.SetDownloadRecordSelection(
			ctx, live.ID, downloadrecord.SelectionStateUnsupported, nil, 0,
		); uerr != nil {
			slog.WarnContext(ctx, "widen: mark selection unsupported failed",
				"record.id", live.ID, "error", uerr)
		}
		live.SelectionState = downloadrecord.SelectionStateUnsupported
	case serr != nil:
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("set wanted files: %w", serr),
		)
	default:
		if uerr := d.db.SetDownloadRecordSelection(
			ctx, live.ID, downloadrecord.SelectionStateApplied, keep, keptBytes,
		); uerr != nil {
			slog.WarnContext(ctx, "widen: confirm selection failed",
				"record.id", live.ID, "error", uerr)
		}
		live.SelectionState = downloadrecord.SelectionStateApplied
	}

	// added is the delta this grab contributed; the episodes already in
	// live.WantedEpisodes were marked at their original grab. The guard
	// inside MarkEpisodeDownloading (moves only from "wanted") is what keeps
	// this safe for a replace target already sitting at "available".
	for _, id := range added {
		if _, merr := d.db.MarkEpisodeDownloading(ctx, id); merr != nil {
			slog.WarnContext(ctx, "widen: mark episode downloading failed",
				"episode.id", id, "error", merr)
		}
	}

	if live.Status == downloadrecord.StatusCompleted {
		if err := d.db.UpdateDownloadRecordStatus(
			ctx, live.ID, downloadrecord.StatusDownloading,
		); err != nil {
			return nil, otelx.RecordSpanError(
				span, fmt.Errorf("revert record to downloading: %w", err),
			)
		}
		live.Status = downloadrecord.StatusDownloading
	}

	slog.InfoContext(ctx, "widened selective grab", "record.id", live.ID,
		"hash", live.TorrentHash, "added", len(added))
	return live, nil
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

			pending := record.SelectionState ==
				downloadrecord.SelectionStatePending
			// A pending record has no keep-set yet, so a client scoping
			// progress to wanted files can report it complete with nothing
			// downloaded. Importing then would import an empty payload.
			// RunSelectionPass either resolves it or gives up, and the record
			// leaves pending either way.
			if pending ||
				(torrent.Status != StatusSeeding &&
					torrent.Status != StatusCompleted) {
				// Still in flight: mirror the torrent's paused state onto the
				// episode badges (paused vs downloading) so the UI reflects it.
				// A pending selection is never "paused" here even when the
				// client reports it stopped at metadata (spec §4.2) — that's
				// RunSelectionPass's window to resolve, not a state the user
				// paused.
				paused := torrent.Status == StatusPaused && !pending
				if serr := d.db.SyncSeasonDownloadStateForRecord(
					ctx, record.ID, paused,
				); serr != nil {
					slog.WarnContext(ctx, "sync paused episode state failed",
						"record.id", record.ID, "error", serr)
				}
				return
			}

			contentPath, err := downloadSavePath(torrent.Name)
			if err != nil {
				slog.WarnContext(ctx,
					"refusing torrent with unsafe name",
					"id", record.ID,
					"name", torrent.Name,
					"error", err,
				)
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
			if err := d.db.MarkRecordEpisodesImporting(
				ctx, record.ID,
			); err != nil {
				slog.WarnContext(ctx,
					"mark episodes importing failed",
					"id", record.ID, "error", err)
			}
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

// PurgeRecordForHash drops the live record tracking torrentHash, if any. It is
// the deliberate counterpart to the monitor's orphan sweep: that one waits out
// monitorOrphanGrace because a torrent missing from a client's listing may just
// be a client that hasn't caught up, while a removal issued through streamline
// itself is known to be final the moment it succeeds. No record for the hash is
// not an error — a torrent added out-of-band has none.
func (d *download) PurgeRecordForHash(
	ctx context.Context,
	torrentHash string,
) error {
	ctx, span := tracer.Start(ctx, "download.purge_record_for_hash")
	defer span.End()

	rec, err := d.db.FindLiveDownloadRecordByHash(ctx, torrentHash)
	if err != nil {
		return otelx.RecordSpanError(
			span, fmt.Errorf("find record for %s: %w", torrentHash, err),
		)
	}
	if rec == nil {
		return nil
	}
	d.purgeOrphanedRecord(ctx, rec)
	return d.ReconcileEpisodeStatuses(ctx)
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
// when KeepTorrentSeeding=false after a successful import, where deleteFiles is
// false — the library already holds the copy/hardlink and the torrent contents
// are the source. A rejected hold passes true: nothing was imported, so leaving
// the files behind would just orphan them.
func (d *download) RemoveTorrent(
	ctx context.Context,
	clientName string,
	hash string,
	deleteFiles bool,
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
	return client.RemoveTorrent(ctx, hash, deleteFiles)
}

func (d *download) ListTorrentFiles(
	ctx context.Context,
	clientName string,
	hash string,
) ([]TorrentFile, error) {
	ctx, span := tracer.Start(ctx, "download.list_torrent_files",
		trace.WithAttributes(attribute.String("torrent.hash", hash)))
	defer span.End()

	dc, ok := config.FindDownloadClient(clientName)
	if !ok {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("download client %q not found", clientName),
		)
	}
	client, err := d.buildClient(dc)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	files, err := client.ListFiles(ctx, hash)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	return files, nil
}

// buildBaseURL composes scheme://host:port for download client requests.
func buildBaseURL(host string, port uint16, useSSL bool) string {
	scheme := "http"
	if useSSL {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

// checkReleaseSource rejects a release link that is neither a magnet nor an
// http(s) URL addressing a configured indexer. An empty link is not this
// function's concern — resolveTorrentSource reports that with its own error.
//
// Called both at the top of grab, so a client-supplied URL is refused before
// any other work, and inside resolveTorrentSource, so the guard also sits at
// the fetch it protects.
func checkReleaseSource(dl string) error {
	if dl == "" || strings.HasPrefix(dl, "magnet:") {
		return nil
	}
	u, err := url.Parse(dl)
	if err != nil {
		return fmt.Errorf("%w: unparseable URL", ErrUntrustedSource)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf(
			"%w: unsupported scheme %q", ErrUntrustedSource, u.Scheme,
		)
	}
	if !fromConfiguredIndexer(u) {
		return ErrUntrustedSource
	}
	return nil
}

// fromConfiguredIndexer reports whether u addresses one of the enabled
// indexers. The grab body is client-supplied, so without this check an
// authenticated caller picks any address the server can reach — LAN hosts,
// loopback, cloud metadata. Blocking private ranges is not usable as the guard
// here: in a self-hosted install the indexer is itself normally on a private
// address, so the configured set is the trust boundary instead.
func fromConfiguredIndexer(u *url.URL) bool {
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	for _, ix := range config.EnabledIndexers() {
		if strings.EqualFold(u.Hostname(), ix.Host) &&
			port == strconv.Itoa(int(ix.Port)) {
			return true
		}
	}
	return false
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
	if err := checkReleaseSource(dl); err != nil {
		return TorrentSource{}, err
	}
	// Both errors below render the release link, which authenticates the
	// download: Jackett puts a `jackett_apikey` query parameter on it and
	// Prowlarr an `apikey` (its X-Api-Key header covers the search API, not the
	// download links it hands back). grab records whatever comes out of here on
	// the download.grab span, so an unredacted one exports the key.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dl, nil)
	if err != nil {
		return TorrentSource{}, otelx.RedactTransportError(err)
	}
	resp, err := otelx.HTTPClient.Do(req)
	if err != nil {
		return TorrentSource{}, fmt.Errorf(
			"%w: %w", ErrUnreachable, otelx.RedactTransportError(err),
		)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return TorrentSource{}, fmt.Errorf(
			"indexer returned status %d", resp.StatusCode,
		)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxTorrentFileSize+1))
	if err != nil {
		return TorrentSource{}, fmt.Errorf("read torrent body: %w", err)
	}
	if int64(len(body)) > MaxTorrentFileSize {
		return TorrentSource{}, fmt.Errorf(
			"torrent file exceeds %d byte cap", MaxTorrentFileSize,
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
	case "builtin":
		if d.builtin == nil {
			return nil, fmt.Errorf(
				"%w: builtin engine not running (restart required)",
				ErrUnsupportedClient,
			)
		}
		return d.builtin, nil
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
