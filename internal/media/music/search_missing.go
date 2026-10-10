package music

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"

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
// window or ctx cancellation. An unreleased or unhydrated album is not
// eligible: the first is not missing yet, the second has no tracks to import
// into.
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
