package mediaserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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

// countRefresh records one server's outcome for a refresh dispatch.
func countRefresh(ctx context.Context, ms config.MediaServerEntry, outcome string) {
	refreshes.Add(ctx, 1, metric.WithAttributes(
		attribute.String("server.name", ms.Name),
		attribute.String("server.type", ms.ServerType),
		attribute.String("outcome", outcome),
	))
}

// Refresher asks the media servers to rescan a library root. Anything that adds,
// rewrites, moves or deletes a file under one holds it: until the rescan, Plex
// and Jellyfin keep listing what was there before.
type Refresher interface {
	// kind is "movie", "series", KindMusic or KindBook — Plex scopes its rescan
	// to one section and keys them separately, so the path alone does not say
	// which to poke.
	RefreshAll(ctx context.Context, kind, libraryPath string) error
}

// KindMusic and KindBook join "movie" and "series" as RefreshAll kinds. Plex
// holds music in "artist" sections and has no book library type at all, so a
// book refresh reaches Jellyfin and Emby only.
const (
	KindMusic = "music"
	KindBook  = "book"

	plexMusicSectionType = "artist"
)

// plexTypedRefresher is what a Plex client offers beyond Server: a rescan
// scoped to the sections of one type when no section key is configured.
type plexTypedRefresher interface {
	RefreshLibraryOfType(ctx context.Context, libraryPath, sectionType string) error
}

// Dispatcher fans RefreshLibrary across all enabled media servers, read live
// from config per invocation.
type Dispatcher struct{}

var _ Refresher = (*Dispatcher)(nil)

func NewDispatcher() *Dispatcher {
	return &Dispatcher{}
}

// RefreshInBackground runs RefreshAll off the caller's path, for request
// handlers: an unreachable server holds each call for otelx.HTTPClient's whole
// timeout, once per Plex section when none is configured. The ctx is detached
// from cancellation, since the request finishes long before the rescan does.
func RefreshInBackground(
	ctx context.Context,
	r Refresher,
	kind, libraryPath string,
) {
	if r == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		defer observability.RecoverPanic(ctx, "mediaserver.refresh", nil)
		if err := r.RefreshAll(ctx, kind, libraryPath); err != nil {
			slog.WarnContext(ctx, "media server refresh reported errors",
				"media.kind", kind, "error", err)
		}
	}()
}

// RefreshAll fans a rescan across every enabled media server. kind is "movie",
// "series", KindMusic or KindBook: Plex rescans one section at a time and they
// are configured separately, so an episode import must not poke the movie
// section.
func (d *Dispatcher) RefreshAll(
	ctx context.Context,
	kind, libraryPath string,
) error {
	ctx, span := tracer.Start(ctx, "mediaserver.refresh_all",
		trace.WithAttributes(
			attribute.String("media.kind", kind),
			attribute.String("library.path", libraryPath),
		))
	defer span.End()

	servers := config.EnabledMediaServers()
	var errs []error
	for _, ms := range servers {
		if kind == KindBook && ms.ServerType == "plex" {
			countRefresh(ctx, ms, "skipped")
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
			countRefresh(ctx, ms, "client_error")
			continue
		}
		if err := refreshOne(ctx, client, ms, kind, libraryPath); err != nil {
			slog.WarnContext(
				ctx,
				"media server refresh failed",
				"name",
				ms.Name,
				"error",
				err,
			)
			errs = append(errs, fmt.Errorf("%s: %w", ms.Name, err))
			countRefresh(ctx, ms, "error")
			continue
		}
		countRefresh(ctx, ms, "ok")
	}
	return errors.Join(errs...)
}

func refreshOne(
	ctx context.Context,
	client Server,
	ms config.MediaServerEntry,
	kind, libraryPath string,
) error {
	switch kind {
	case KindMusic:
		if typed, ok := client.(plexTypedRefresher); ok {
			return typed.RefreshLibraryOfType(ctx, libraryPath, plexMusicSectionType)
		}
		return client.RefreshLibrary(ctx, libraryPath, "")
	case KindBook:
		return client.RefreshLibrary(ctx, libraryPath, "")
	}
	section := ms.LibrarySection
	if kind == "series" {
		section = ms.LibrarySectionTV
	}
	var sectionKey string
	if section != nil {
		sectionKey = *section
	}
	return client.RefreshLibrary(ctx, libraryPath, sectionKey)
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
