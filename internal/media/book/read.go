package book

import (
	"context"
	"log/slog"
	"math"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/otelx"
)

const maxProgress = 100

// ListParams are the shelf filters and the page asked for.
type ListParams struct {
	Status string
	Author string
	Format string
	Kinds  []string
	Query  string
	Sort   string
	Desc   bool
	Page   uint32
	Limit  uint32
}

func (p ListParams) shelf() db.ShelfParams {
	page := max(p.Page, 1)
	return db.ShelfParams{
		Status: p.Status,
		Author: p.Author,
		Format: p.Format,
		Kinds:  p.Kinds,
		Query:  p.Query,
		Sort:   p.Sort,
		Desc:   p.Desc,
		Limit:  p.Limit,
		Offset: (page - 1) * p.Limit,
	}
}

// ShelfPage is one page of the shelf and what it needs to be drawn: the
// number of matching items and the live progress of the books on it that are
// downloading.
type ShelfPage struct {
	Rows     []db.ShelfRow
	Total    uint32
	Progress map[uint32]float64
}

func (s *Service) List(ctx context.Context, p ListParams) (ShelfPage, error) {
	ctx, span := tracer.Start(ctx, "book.list")
	defer span.End()

	rows, total, err := s.db.ListShelf(ctx, p.shelf())
	if err != nil {
		return ShelfPage{}, otelx.RecordSpanError(span, err)
	}
	page := ShelfPage{Rows: rows, Total: total, Progress: map[uint32]float64{}}
	var downloading []uint32
	for _, r := range rows {
		if r.Type == lookupBook && r.Status == "downloading" {
			downloading = append(downloading, r.ID)
		}
	}
	for id, slots := range s.Progress(ctx, downloading) {
		var sum float64
		for _, v := range slots {
			sum += v
		}
		if len(slots) > 0 {
			page.Progress[id] = math.Round(sum / float64(len(slots)))
		}
	}
	return page, nil
}

func (s *Service) Counts(ctx context.Context, p ListParams) (db.ShelfCounts, error) {
	ctx, span := tracer.Start(ctx, "book.counts")
	defer span.End()

	c, err := s.db.ShelfCountsFor(ctx, p.shelf())
	if err != nil {
		return db.ShelfCounts{}, otelx.RecordSpanError(span, err)
	}
	return c, nil
}

func (s *Service) GetBook(ctx context.Context, id uint32) (*ent.Book, error) {
	ctx, span := tracer.Start(ctx, "book.get",
		trace.WithAttributes(attribute.Int("book.id", int(id))))
	defer span.End()

	row, err := s.db.FindBookByID(ctx, id)
	if ent.IsNotFound(err) {
		err = ErrBookNotFound
	}
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	return row, nil
}

func (s *Service) GetSeries(
	ctx context.Context,
	id uint32,
) (*ent.BookSeries, error) {
	ctx, span := tracer.Start(ctx, "book.get_series",
		trace.WithAttributes(attribute.Int("series.id", int(id))))
	defer span.End()

	row, err := s.db.FindSeriesByID(ctx, id)
	if ent.IsNotFound(err) {
		err = ErrSeriesNotFound
	}
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	return row, nil
}

// Progress reports, per book and slot, how far the download in flight has got
// as a whole percentage. The queue is the short-TTL snapshot /activity shares,
// and it is only asked for when some record is in flight. A queue that cannot
// be read leaves the slots without a figure; it is not an error.
func (s *Service) Progress(
	ctx context.Context,
	bookIDs []uint32,
) map[uint32]map[string]float64 {
	out := map[uint32]map[string]float64{}
	if len(bookIDs) == 0 || s.download == nil {
		return out
	}
	refs, err := s.db.ListActiveBookRecords(ctx, bookIDs)
	if err != nil {
		slog.WarnContext(ctx, "book progress: listing records failed", "error", err)
		return out
	}
	if len(refs) == 0 {
		return out
	}
	snap, err := s.download.Queue(ctx)
	if err != nil {
		slog.WarnContext(ctx, "book progress: live queue unavailable", "error", err)
		return out
	}
	byRecord := make(map[uint32]float64, len(snap.Items))
	for _, e := range snap.Items {
		byRecord[e.RecordID] = e.Progress
	}
	for _, ref := range refs {
		p, ok := byRecord[ref.RecordID]
		if !ok {
			continue
		}
		if out[ref.BookID] == nil {
			out[ref.BookID] = map[string]float64{}
		}
		out[ref.BookID][ref.Kind] = math.Min(math.Round(p*maxProgress), maxProgress)
	}
	return out
}
