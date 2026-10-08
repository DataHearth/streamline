package music

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/media/searchwindow"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/scheduler"
)

// MissingSearcher runs one backlog search pass over the wanted albums.
type MissingSearcher interface {
	SearchMissing(ctx context.Context) error
}

var _ MissingSearcher = (*Service)(nil)

// SearchMissing searches the indexers for every eligible wanted album and
// grabs the best accepted release. Per-album failures are logged and never
// abort the pass; the error return is for the initial query, a bad config
// window or ctx cancellation.
func (s *Service) SearchMissing(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "music.search_missing")
	defer span.End()

	window, err := searchwindow.Current(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	albums, err := s.db.ListEligibleAlbumsForSync(
		ctx, window.MaxGrabFailures, window.NotSearchedSince,
	)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("eligible.count", len(albums)))
	if len(albums) == 0 {
		return nil
	}
	if _, ok := config.PickDownloadClient(); !ok {
		slog.InfoContext(ctx, "music missing-search: no enabled download client",
			"eligible", len(albums))
		return nil
	}

	grabbed := 0
	for i, a := range albums {
		if err := ctx.Err(); err != nil {
			return otelx.RecordSpanError(span, err)
		}
		scheduler.Progress(ctx, i, len(albums))
		if s.searchAlbum(ctx, a) {
			grabbed++
		}
	}
	scheduler.Progress(ctx, len(albums), len(albums))
	slog.InfoContext(ctx, "music missing-search pass complete",
		"eligible", len(albums), "grabbed", grabbed)
	return nil
}

// searchAlbum reports whether a release was grabbed. last_search_at is
// stamped whenever the indexers answered, so the cooldown advances on an empty
// result too; a failed search stamps nothing and counts no strike.
func (s *Service) searchAlbum(ctx context.Context, a *ent.Album) bool {
	ctx, span := tracer.Start(ctx, "music.search_missing.album",
		trace.WithAttributes(attribute.Int("album.id", int(a.ID))))
	defer span.End()

	releases, err := s.SearchAlbumReleases(ctx, a.ID)
	if err != nil {
		otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "music missing-search: search failed",
			"album.id", a.ID, "error", err)
		return false
	}
	if err := s.db.SetAlbumLastSearchAt(ctx, a.ID, time.Now()); err != nil {
		otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "music missing-search: stamp last_search_at failed",
			"album.id", a.ID, "error", err)
	}
	if len(releases) == 0 {
		return false
	}

	if err := s.GrabAlbumRelease(ctx, a.ID, releases[0].Result); err != nil {
		otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "music missing-search: grab failed",
			"album.id", a.ID, "error", err)
		if !searchwindow.TransportFailure(err) {
			if e := s.db.IncrementAlbumGrabFailures(ctx, a.ID); e != nil {
				slog.WarnContext(
					ctx,
					"music missing-search: bump grab_failures failed",
					"album.id",
					a.ID,
					"error",
					e,
				)
			}
		}
		return false
	}
	if err := s.db.ResetAlbumGrabFailures(ctx, a.ID); err != nil {
		slog.WarnContext(ctx, "music missing-search: reset grab_failures failed",
			"album.id", a.ID, "error", err)
	}
	return true
}
