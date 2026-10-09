package book

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/media/book/pick"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
)

var (
	meter = otel.Meter("github.com/datahearth/streamline/internal/media/book")

	hydrations metric.Int64Counter
)

func init() {
	hydrations = otelx.Must(meter.Int64Counter(
		"streamline.books.hydrations",
		metric.WithDescription("Series volume stubs filled in, by outcome"),
	))
	hydrations.Add(context.Background(), 0)
}

// budgetLow reports that Hardcover's day has only the scan reserve left, which
// background work leaves to adds, refreshes and approvals.
func (s *Service) budgetLow() bool {
	b, ok := s.metadata.(metadata.Budgeter)
	return ok && b.Remaining() <= b.ScanReserve()
}

// HydrateSeriesInBackground starts the process-wide worker that fills series
// volume stubs, twenty per Hardcover request, unless one is already running. A
// stub is never searched, grabbed or imported before it is hydrated, and a
// restart loses nothing: the metadata refresh job selects stubs first and
// calls this again.
func (s *Service) HydrateSeriesInBackground(ctx context.Context) {
	if s.metadata == nil || !s.hydrating.CompareAndSwap(false, true) {
		return
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		defer s.hydrating.Store(false)
		defer observability.RecoverPanic(bg, "book.hydrate_series", nil)
		s.hydrateAll(bg)
	}()
}

func (s *Service) hydrateAll(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "book.hydrate_series")
	defer span.End()

	total := 0
	for !s.budgetLow() {
		stubs, err := s.db.ListHydrationStubs(ctx, hydrateBatch)
		if err != nil {
			otelx.RecordSpanError(span, err)
			slog.ErrorContext(
				ctx,
				"series hydration: listing stubs failed",
				"error",
				err,
			)
			hydrations.Add(
				ctx,
				1,
				metric.WithAttributes(attribute.String("outcome", "error")),
			)
			return
		}
		if len(stubs) == 0 {
			break
		}
		done, err := s.hydrateStubs(ctx, stubs)
		total += done
		if err != nil {
			otelx.RecordSpanError(span, err)
			outcome := "error"
			if errors.Is(err, metadata.ErrRateLimited) {
				outcome = "rate_limited"
			}
			hydrations.Add(
				ctx,
				1,
				metric.WithAttributes(attribute.String("outcome", outcome)),
			)
			slog.WarnContext(
				ctx,
				"series hydration stopped; the next refresh resumes it",
				"hydrated",
				total,
				"error",
				err,
			)
			return
		}
		if done == 0 {
			break
		}
	}
	span.SetAttributes(attribute.Int("hydrated", total))
	if total > 0 {
		slog.InfoContext(ctx, "series hydration complete", "hydrated", total)
	}
}

// hydrateStubs fills up to a batch of never-hydrated books from one Hardcover
// request and reports how many it settled. A stub Hardcover no longer knows,
// or that turns out to be a compilation, is deleted when it is a series volume
// holding no file; any other is stamped so it is not picked again.
func (s *Service) hydrateStubs(ctx context.Context, stubs []*ent.Book) (int, error) {
	prov, err := s.provider()
	if err != nil {
		return 0, err
	}
	ids := make([]uint32, 0, len(stubs))
	for _, b := range stubs {
		ids = append(ids, b.HardcoverID)
	}
	recs, err := prov.GetBooksFresh(ctx, ids)
	if err != nil {
		return 0, err
	}
	byID := make(map[uint32]*metadata.BookRecord, len(recs))
	for _, r := range recs {
		byID[r.HardcoverID] = r
	}

	var (
		done int
		jobs []artJob
		drop []uint32
	)
	now := time.Now()
	for _, stub := range stubs {
		rec := byID[stub.HardcoverID]
		switch {
		case rec == nil || (rec.Compilation && stub.Edges.Series != nil):
			if stub.Edges.Series != nil {
				drop = append(drop, stub.ID)
			} else if err := s.db.MarkBookRefreshed(ctx, stub.ID, now); err != nil {
				return done, err
			}
			done++
			hydrations.Add(
				ctx,
				1,
				metric.WithAttributes(attribute.String("outcome", "dropped")),
			)
			continue
		}
		if err := s.db.ApplyBookMetadata(
			ctx,
			stub.ID,
			bookMetadata(rec, true, now),
		); err != nil {
			return done, err
		}
		if series := stub.Edges.Series; series != nil {
			if err := s.settleVolume(ctx, stub, series, rec); err != nil {
				return done, err
			}
		}
		jobs = append(jobs, artJob{bookID: stub.ID, cover: rec.CoverURL})
		done++
		hydrations.Add(
			ctx,
			1,
			metric.WithAttributes(attribute.String("outcome", "hydrated")),
		)
	}
	if err := s.db.DeleteBooks(ctx, drop); err != nil {
		return done, err
	}
	for _, seriesID := range seriesOf(stubs) {
		if err := s.db.RefreshSeriesStats(ctx, seriesID); err != nil {
			slog.WarnContext(ctx, "series stats not computed",
				"series.id", seriesID, "error", err)
		}
	}
	s.fetchArtInBackground(ctx, jobs...)
	return done, nil
}

func seriesOf(books []*ent.Book) []uint32 {
	var out []uint32
	for _, b := range books {
		if s := b.Edges.Series; s != nil && !slices.Contains(out, s.ID) {
			out = append(out, s.ID)
		}
	}
	return out
}

// settleVolume applies the series' choices to a volume that has just been
// hydrated: its monitoring under the series policy, measured against the date
// the series was added, and the series' chosen edition for the ebook slot.
func (s *Service) settleVolume(
	ctx context.Context,
	stub *ent.Book,
	series *ent.BookSeries,
	rec *metadata.BookRecord,
) error {
	row, err := s.db.FindBookByID(ctx, stub.ID)
	if err != nil {
		return err
	}
	if series.EditionLanguage != "" {
		ed, ok := pick.SlotIn(
			entEditionViews(row.Edges.Editions),
			slotEbook, series.EditionLanguage, series.EditionPublisher,
		)
		if ok &&
			(row.Edges.EbookEdition == nil || row.Edges.EbookEdition.ID != ed.ID) {
			if err := s.db.SetBookSlotEdition(
				ctx,
				row.ID,
				slotEbook,
				ed.ID,
			); err != nil {
				return err
			}
			row.Edges.EbookEdition = nil
		}
	}
	wants := pick.MonitorsVolume(
		string(series.Monitor),
		rec.ReleaseDate,
		series.CreateTime,
	)
	if wants &&
		len(pick.OfFormat(entEditionViews(row.Edges.Editions), slotEbook)) == 0 {
		wants = false
	}
	if wants != row.EbookMonitored {
		return s.db.SetBookSlot(ctx, row.ID, slotEbook, wants)
	}
	return nil
}
