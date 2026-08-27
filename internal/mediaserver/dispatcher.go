package mediaserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/datahearth/streamline/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("github.com/datahearth/streamline/internal/mediaserver")

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
		}
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
