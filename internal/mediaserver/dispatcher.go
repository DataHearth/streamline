package mediaserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/datahearth/streamline/internal/config"
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

// Dispatcher fans RefreshLibrary across all enabled media servers, read live
// from config per invocation.
type Dispatcher struct{}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{}
}

// RefreshAll fans a rescan across every enabled media server. kind is "movie"
// or "series": Plex rescans one section at a time and the two are configured
// separately, so an episode import must not poke the movie section.
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
		section := ms.LibrarySection
		if kind == "series" {
			section = ms.LibrarySectionTV
		}
		var sectionKey string
		if section != nil {
			sectionKey = *section
		}
		if err := client.RefreshLibrary(ctx, libraryPath, sectionKey); err != nil {
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
