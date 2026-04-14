package mediaserver

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/datahearth/streamline/internal/config"
)

// PlayOnStatus describes the resolution outcome for one media server.
type PlayOnStatus string

const (
	StatusResolved    PlayOnStatus = "resolved"
	StatusFallback    PlayOnStatus = "fallback"
	StatusUnavailable PlayOnStatus = "unavailable"
)

// PlayOnResult is one server's resolved link or fallback for a single movie.
type PlayOnResult struct {
	Name       string
	ServerType string
	URL        string
	Fallback   bool
	Status     PlayOnStatus
}

// DeepLinker resolves per-server deep links for a movie. Used by
// GET /movies/{id}/play-on. The enabled-server set is read live from config.
type DeepLinker struct {
	builder func(config.MediaServerEntry) (Server, error)
}

func NewDeepLinker(
	builder func(config.MediaServerEntry) (Server, error),
) *DeepLinker {
	if builder == nil {
		builder = BuildServer
	}
	return &DeepLinker{builder: builder}
}

// Resolve fans out to every enabled media server in parallel, resolving a movie
// deep link per server. Per-server errors never propagate; they become
// PlayOnResult entries with Status = unavailable so the UI can render them.
func (l *DeepLinker) Resolve(
	ctx context.Context,
	tmdbID uint32,
	title string,
	year uint16,
) []PlayOnResult {
	return l.resolveAll(
		ctx,
		func(ctx context.Context, ms config.MediaServerEntry) PlayOnResult {
			return l.linkResult(
				ctx,
				ms,
				ErrMovieNotFound,
				func(c Server) (string, error) {
					return c.MovieDeepLink(
						ctx,
						deref(ms.LibrarySection),
						tmdbID,
						title,
						year,
					)
				},
			)
		},
	)
}

// ResolveTV is the series counterpart of Resolve.
func (l *DeepLinker) ResolveTV(
	ctx context.Context,
	tvdbID uint32,
	title string,
	year uint16,
) []PlayOnResult {
	return l.resolveAll(
		ctx,
		func(ctx context.Context, ms config.MediaServerEntry) PlayOnResult {
			return l.linkResult(
				ctx,
				ms,
				ErrShowNotFound,
				func(c Server) (string, error) {
					return c.TVShowDeepLink(
						ctx,
						deref(ms.LibrarySectionTV),
						tvdbID,
						title,
						year,
					)
				},
			)
		},
	)
}

// resolveAll fans out one per-server resolver across every enabled media server.
func (l *DeepLinker) resolveAll(
	ctx context.Context,
	one func(context.Context, config.MediaServerEntry) PlayOnResult,
) []PlayOnResult {
	servers := config.EnabledMediaServers()
	out := make([]PlayOnResult, len(servers))
	var wg sync.WaitGroup
	for i, ms := range servers {
		wg.Go(func() { out[i] = one(ctx, ms) })
	}
	wg.Wait()
	return out
}

// linkResult builds the server client and maps one deep-link call to a
// PlayOnResult. notFound is the sentinel that triggers the web-root fallback.
func (l *DeepLinker) linkResult(
	ctx context.Context,
	ms config.MediaServerEntry,
	notFound error,
	link func(Server) (string, error),
) PlayOnResult {
	base := PlayOnResult{Name: ms.Name, ServerType: ms.ServerType}
	client, err := l.builder(ms)
	if err != nil {
		slog.WarnContext(ctx, "play-on: build client failed",
			"server.name", ms.Name, "error", err)
		base.Status = StatusUnavailable
		return base
	}
	url, err := link(client)
	switch {
	case err == nil:
		base.URL = url
		base.Status = StatusResolved
	case errors.Is(err, notFound):
		base.URL = strings.TrimRight(ms.Host, "/") + "/web/"
		base.Fallback = true
		base.Status = StatusFallback
	default:
		slog.WarnContext(ctx, "play-on: deep link failed",
			"server.name", ms.Name, "error", err)
		base.Status = StatusUnavailable
	}
	return base
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
