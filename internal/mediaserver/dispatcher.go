package mediaserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/mediaserver")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/mediaserver")

	// refreshes is dimensioned by server so "Plex has been failing every
	// refresh since Tuesday while Jellyfin is fine" is a graph rather than a
	// log grep. This runs after every completed import and every transcode,
	// so a silent failure here is a library that stops updating.
	refreshes metric.Int64Counter
)

func init() {
	refreshes = otelx.Must(meter.Int64Counter(
		"streamline.mediaserver.refreshes",
		metric.WithDescription("Library refresh dispatches by server and outcome"),
	))
}

// countRefresh records one server's outcome for a refresh dispatch. scope is
// what the server was asked to rescan ("folders", "library"), "none" when
// nothing was sent.
func countRefresh(
	ctx context.Context,
	ms config.MediaServerEntry,
	outcome string,
	scope RefreshScope,
) {
	if scope == "" {
		scope = "none"
	}
	refreshes.Add(ctx, 1, metric.WithAttributes(
		attribute.String("server.name", ms.Name),
		attribute.String("server.type", ms.ServerType),
		attribute.String("outcome", outcome),
		attribute.String("scope", string(scope)),
	))
}

// Refresher asks the media servers to rescan what changed under a library
// root. Anything that adds, rewrites, moves or deletes a file under one holds
// it: until the rescan, Plex and Jellyfin keep listing what was there before.
type Refresher interface {
	// kind is "movie", "series", KindMusic or KindBook — Plex scopes its rescan
	// to one section and keys them separately, so the path alone does not say
	// which to poke. paths are the files or folders that changed (a deleted
	// one included); the servers rescan just their folders when they can, and
	// the whole library when none is given.
	RefreshAll(ctx context.Context, kind, libraryPath string, paths ...string) error
}

// KindMusic and KindBook join "movie" and "series" as RefreshAll kinds. Plex
// holds music in "artist" sections and has no book library type at all, so a
// book refresh reaches Jellyfin and Emby only.
const (
	KindMusic = "music"
	KindBook  = "book"

	plexMusicSectionType = "artist"
)

const (
	// refreshWindow is how long the dispatcher collects refreshes before it
	// sends them: the downloads one RSS cycle grabbed tend to finish together,
	// and each used to cost every server its own scan.
	refreshWindow = 30 * time.Second
	// maxBatchFolders caps the folders one batch rescans one by one. Past it
	// the whole library is cheaper than as many partial scans, which Plex
	// takes one request each.
	maxBatchFolders = 50
	// maxBatchLinks caps the trace links a flush span carries back to the
	// requests it serves.
	maxBatchLinks = 32
)

// RefreshStatuses reports the last refresh sent to each server, for the
// settings page.
type RefreshStatuses interface {
	LastRefresh(name string) (RefreshStatus, bool)
}

var _ RefreshStatuses = (*Dispatcher)(nil)

// RefreshStatus is the last refresh a server was sent, for the settings page.
type RefreshStatus struct {
	At      time.Time
	Outcome string
	Scope   RefreshScope
	// Folders counts the folders a ScopeFolders refresh named.
	Folders int
	Error   string
}

// batchKey is one library a refresh is collected for.
type batchKey struct {
	kind, root string
}

// batch is the folders collected for one library; whole means the library
// itself, which covers every folder.
type batch struct {
	whole   bool
	folders map[string]struct{}
}

// Dispatcher fans a rescan across all enabled media servers, read live from
// config per send. With a window it coalesces: RefreshAll queues the folders
// and returns, and one send per server and library carries everything queued
// in the window.
type Dispatcher struct {
	window time.Duration

	mu      sync.Mutex
	pending map[batchKey]*batch
	order   []batchKey
	links   []trace.Link
	timer   *time.Timer

	// flushMu serialises flushes, so Flush at shutdown waits for a timer's
	// flush already sending rather than racing it.
	flushMu sync.Mutex

	statusMu sync.Mutex
	status   map[string]RefreshStatus
}

var _ Refresher = (*Dispatcher)(nil)

// NewDispatcher returns a dispatcher coalescing refreshes over refreshWindow.
// Flush sends what it holds; call it at shutdown.
func NewDispatcher() *Dispatcher {
	return newDispatcher(refreshWindow)
}

// newDispatcher with a zero window sends every refresh synchronously.
func newDispatcher(window time.Duration) *Dispatcher {
	return &Dispatcher{
		window:  window,
		pending: map[batchKey]*batch{},
		status:  map[string]RefreshStatus{},
	}
}

// RefreshInBackground runs RefreshAll off the caller's path, for request
// handlers: a Refresher that sends synchronously holds each call for
// otelx.HTTPClient's whole timeout on an unreachable server. The ctx is
// detached from cancellation, since the request finishes long before the
// rescan does.
func RefreshInBackground(
	ctx context.Context,
	r Refresher,
	kind, libraryPath string,
	paths ...string,
) {
	if r == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		defer observability.RecoverPanic(ctx, "mediaserver.refresh", nil)
		if err := r.RefreshAll(ctx, kind, libraryPath, paths...); err != nil {
			slog.WarnContext(ctx, "media server refresh reported errors",
				"media.kind", kind, "error", err)
		}
	}()
}

// RefreshAll asks every enabled media server to rescan the folders of paths
// under libraryPath, or the whole library when paths is empty. kind is
// "movie", "series", KindMusic or KindBook: Plex rescans one section at a
// time and they are configured separately, so an episode import must not poke
// the movie section.
//
// The paths are folded into folders here, while they still describe the
// change: a file's folder, a deleted one's nearest surviving parent. With a
// window the call only queues them and returns nil; the flush logs what the
// servers answer.
func (d *Dispatcher) RefreshAll(
	ctx context.Context,
	kind, libraryPath string,
	paths ...string,
) error {
	folders, whole := scanFolders(libraryPath, paths)
	if d.window <= 0 {
		return d.dispatch(ctx, kind, libraryPath, folders, whole)
	}
	d.enqueue(ctx, batchKey{kind: kind, root: libraryPath}, folders, whole)
	return nil
}

func (d *Dispatcher) enqueue(
	ctx context.Context,
	key batchKey,
	folders []string,
	whole bool,
) {
	d.mu.Lock()
	defer d.mu.Unlock()
	b, ok := d.pending[key]
	if !ok {
		b = &batch{folders: map[string]struct{}{}}
		d.pending[key] = b
		d.order = append(d.order, key)
	}
	switch {
	case b.whole:
	case whole:
		b.whole, b.folders = true, nil
	default:
		for _, f := range folders {
			b.folders[f] = struct{}{}
		}
		if len(b.folders) > maxBatchFolders {
			b.whole, b.folders = true, nil
		}
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() &&
		len(d.links) < maxBatchLinks {
		d.links = append(d.links, trace.Link{SpanContext: sc})
	}
	if d.timer == nil {
		d.timer = time.AfterFunc(d.window, func() {
			ctx := context.Background()
			defer observability.RecoverPanic(ctx, "mediaserver.flush", nil)
			d.flush(ctx)
		})
	}
}

// Flush sends everything queued now, and waits for a flush already under way.
func (d *Dispatcher) Flush(ctx context.Context) {
	d.flush(ctx)
}

func (d *Dispatcher) flush(ctx context.Context) {
	d.flushMu.Lock()
	defer d.flushMu.Unlock()

	d.mu.Lock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	pending, order, links := d.pending, d.order, d.links
	d.pending, d.order, d.links = map[batchKey]*batch{}, nil, nil
	d.mu.Unlock()
	if len(order) == 0 {
		return
	}

	ctx, span := tracer.Start(ctx, "mediaserver.flush",
		trace.WithLinks(links...),
		trace.WithAttributes(attribute.Int("batch.count", len(order))))
	defer span.End()

	var errs []error
	for _, key := range order {
		b := pending[key]
		var folders []string
		if !b.whole {
			folders = pruneNested(b.folders)
		}
		if err := d.dispatch(ctx, key.kind, key.root, folders, b.whole); err != nil {
			slog.WarnContext(ctx, "media server refresh reported errors",
				"media.kind", key.kind, "error", err)
			errs = append(errs, err)
		}
	}
	_ = otelx.RecordSpanError(span, errors.Join(errs...))
}

// LastRefresh returns the last refresh sent to the named server since the
// process started.
func (d *Dispatcher) LastRefresh(name string) (RefreshStatus, bool) {
	d.statusMu.Lock()
	defer d.statusMu.Unlock()
	st, ok := d.status[name]
	return st, ok
}

func (d *Dispatcher) record(name string, st RefreshStatus) {
	d.statusMu.Lock()
	defer d.statusMu.Unlock()
	d.status[name] = st
}

// dispatch sends one library's rescan to every enabled server.
func (d *Dispatcher) dispatch(
	ctx context.Context,
	kind, libraryPath string,
	folders []string,
	whole bool,
) error {
	ctx, span := tracer.Start(ctx, "mediaserver.refresh_all",
		trace.WithAttributes(
			attribute.String("media.kind", kind),
			attribute.String("library.path", libraryPath),
			attribute.Int("refresh.folders", len(folders)),
		))
	defer span.End()
	if whole {
		folders = nil
	}

	servers := config.EnabledMediaServers()
	var errs []error
	for _, ms := range servers {
		if kind == KindBook && ms.ServerType == "plex" {
			countRefresh(ctx, ms, "skipped", "")
			continue
		}
		client, err := BuildServer(ms)
		if err != nil {
			slog.WarnContext(
				ctx,
				"build media server client failed",
				"name",
				ms.Name,
				"error",
				err,
			)
			errs = append(errs, fmt.Errorf("%s: %w", ms.Name, err))
			countRefresh(ctx, ms, "client_error", "")
			continue
		}
		req := refreshRequest(ms, kind, libraryPath, folders)
		scope, err := client.RefreshLibrary(ctx, req)
		st := RefreshStatus{At: time.Now(), Outcome: "ok", Scope: scope}
		if scope == ScopeFolders {
			st.Folders = len(req.Folders)
		}
		if err != nil {
			slog.WarnContext(
				ctx,
				"media server refresh failed",
				"name",
				ms.Name,
				"error",
				err,
			)
			errs = append(errs, fmt.Errorf("%s: %w", ms.Name, err))
			countRefresh(ctx, ms, "error", scope)
			st.Outcome, st.Error = "error", err.Error()
			d.record(ms.Name, st)
			continue
		}
		countRefresh(ctx, ms, "ok", scope)
		d.record(ms.Name, st)
	}
	return otelx.RecordSpanError(span, errors.Join(errs...))
}

// refreshRequest builds one server's request: the root and folders in its own
// view, and for Plex the section configured for the kind.
func refreshRequest(
	ms config.MediaServerEntry,
	kind, libraryPath string,
	folders []string,
) RefreshRequest {
	req := RefreshRequest{Root: mapServerPath(ms.PathMappings, libraryPath)}
	for _, f := range folders {
		req.Folders = append(req.Folders, mapServerPath(ms.PathMappings, f))
	}
	switch kind {
	case KindMusic:
		req.SectionType = plexMusicSectionType
	case KindBook:
	default:
		section := ms.LibrarySection
		if kind == "series" {
			section = ms.LibrarySectionTV
		}
		if section != nil {
			req.SectionKey = *section
		}
	}
	return req
}

func BuildServer(ms config.MediaServerEntry) (Server, error) {
	return buildServer(
		ms.ServerType,
		ms.Host,
		config.SecretValue(ms.APIKey, ms.APIKeyFile),
	)
}

// buildServer constructs the concrete Server for a type. Shared by BuildServer
// (config-backed) and the Test path (TestParams-backed).
func buildServer(serverType, host, apiKey string) (Server, error) {
	switch serverType {
	case "plex":
		return NewPlex(host, apiKey), nil
	case "jellyfin", "emby":
		return NewJellyfin(host, apiKey), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidServerType, serverType)
	}
}
