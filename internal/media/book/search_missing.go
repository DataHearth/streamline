package book

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/media/searchwindow"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/scheduler"
)

var slotKinds = []string{
	string(mediafile.BookKindEbook),
	string(mediafile.BookKindAudiobook),
}

// MissingSearcher runs one backlog search pass over the wanted book slots.
type MissingSearcher interface {
	SearchMissing(ctx context.Context) error
}

var _ MissingSearcher = (*Service)(nil)

type slotWork struct {
	kind string
	book *ent.Book
}

// SearchMissing searches the indexers for every eligible wanted slot, ebook
// and audiobook separately, and grabs the best accepted release. Per-slot
// failures are logged and never abort the pass; the error return is for the
// initial queries, a bad config window or ctx cancellation.
func (s *Service) SearchMissing(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "book.search_missing")
	defer span.End()

	window, err := searchwindow.Current(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	var work []slotWork
	for _, kind := range slotKinds {
		books, err := s.db.ListEligibleBookSlotsForSync(
			ctx, kind, window.MaxGrabFailures, window.NotSearchedSince,
		)
		if err != nil {
			return otelx.RecordSpanError(span, err)
		}
		for _, b := range books {
			work = append(work, slotWork{kind: kind, book: b})
		}
	}
	span.SetAttributes(attribute.Int("eligible.count", len(work)))
	if len(work) == 0 {
		return nil
	}
	if _, ok := config.PickDownloadClient(); !ok {
		slog.InfoContext(ctx, "book missing-search: no enabled download client",
			"eligible", len(work))
		return nil
	}

	grabbed := 0
	for i, w := range work {
		if err := ctx.Err(); err != nil {
			return otelx.RecordSpanError(span, err)
		}
		scheduler.Progress(ctx, i, len(work))
		if s.searchSlot(ctx, w.book, w.kind) {
			grabbed++
		}
	}
	scheduler.Progress(ctx, len(work), len(work))
	slog.InfoContext(ctx, "book missing-search pass complete",
		"eligible", len(work), "grabbed", grabbed)
	return nil
}

// searchSlot reports whether a release was grabbed. last_search_at is stamped
// whenever the indexers answered, so the cooldown advances on an empty result
// too; a failed search stamps nothing and counts no strike. Rejected releases
// are kept by SearchBookReleases for the UI and never grabbed here.
func (s *Service) searchSlot(ctx context.Context, b *ent.Book, kind string) bool {
	ctx, span := tracer.Start(ctx, "book.search_missing.slot",
		trace.WithAttributes(
			attribute.Int("book.id", int(b.ID)),
			attribute.String("book.kind", kind),
		))
	defer span.End()

	releases, err := s.SearchBookReleases(ctx, b.ID, kind)
	if err != nil {
		otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "book missing-search: search failed",
			"book.id", b.ID, "book.kind", kind, "error", err)
		return false
	}
	if err := s.db.SetBookSlotLastSearchAt(ctx, b.ID, kind, time.Now()); err != nil {
		otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "book missing-search: stamp last_search_at failed",
			"book.id", b.ID, "book.kind", kind, "error", err)
	}

	var pick *ReleaseResult
	for i := range releases {
		if !releases[i].Rejected {
			pick = &releases[i]
			break
		}
	}
	if pick == nil {
		return false
	}

	if err := s.GrabBookRelease(
		ctx, b.ID, GrabParams{Kind: kind, Result: pick.SearchResult},
	); err != nil {
		otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "book missing-search: grab failed",
			"book.id", b.ID, "book.kind", kind, "error", err)
		if !searchwindow.TransportFailure(err) {
			if e := s.db.IncrementBookSlotGrabFailures(ctx, b.ID, kind); e != nil {
				slog.WarnContext(
					ctx,
					"book missing-search: bump grab_failures failed",
					"book.id",
					b.ID,
					"book.kind",
					kind,
					"error",
					e,
				)
			}
		}
		return false
	}
	if err := s.db.ResetBookSlotGrabFailures(ctx, b.ID, kind); err != nil {
		slog.WarnContext(ctx, "book missing-search: reset grab_failures failed",
			"book.id", b.ID, "book.kind", kind, "error", err)
	}
	return true
}
