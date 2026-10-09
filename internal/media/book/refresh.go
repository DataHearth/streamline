package book

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/media/book/pick"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/scheduler"
)

const (
	// metadataMinRefreshInterval is how long a title is left alone between
	// scheduled refreshes. A manual run waives it.
	metadataMinRefreshInterval = 24 * time.Hour

	// refreshBookLimit and refreshSeriesLimit cap one scheduled tick: a hundred
	// books are five Hardcover requests, five series about as many again.
	refreshBookLimit   = 100
	refreshSeriesLimit = 5
)

// MetadataRefresher re-pulls provider metadata for stale titles.
type MetadataRefresher interface {
	RefreshStale(ctx context.Context) error
}

var _ MetadataRefresher = (*Service)(nil)

// RefreshBook re-reads one book (one Hardcover request) and writes its data
// back; monitoring, profile, preferred language and a corrected kind stay as
// the user left them.
func (s *Service) RefreshBook(ctx context.Context, id uint32) (*ent.Book, error) {
	ctx, span := tracer.Start(ctx, "book.refresh",
		trace.WithAttributes(attribute.Int("book.id", int(id))))
	defer span.End()

	prov, err := s.provider()
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	row, err := s.GetBook(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	recs, err := prov.GetBooksFresh(ctx, []uint32{row.HardcoverID})
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("get book: %w", err))
	}
	if len(recs) == 0 {
		return nil, otelx.RecordSpanError(span, ErrHardcoverNotFound)
	}
	if err := s.applyRecord(ctx, row, recs[0]); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	out, err := s.GetBook(ctx, id)
	return out, otelx.RecordSpanError(span, err)
}

// applyRecord writes a fresh Hardcover record onto a stored book. A stub, whose
// kind and series choices have not been made yet, gets them now.
func (s *Service) applyRecord(
	ctx context.Context,
	row *ent.Book,
	rec *metadata.BookRecord,
) error {
	stub := row.LastRefreshedAt == nil
	if err := s.db.ApplyBookMetadata(
		ctx, row.ID, bookMetadata(rec, stub, time.Now()),
	); err != nil {
		return err
	}
	if series := row.Edges.Series; stub && series != nil {
		if err := s.settleVolume(ctx, row, series, rec); err != nil {
			return err
		}
	}
	fresh, err := s.db.FindBookByID(ctx, row.ID)
	if err != nil {
		return err
	}
	s.fetchArtInBackground(ctx, jobFor(fresh, rec.CoverURL))
	return nil
}

// refreshBooks re-reads up to a batch of standalone books in one request. A
// book Hardcover no longer knows is stamped so it is not picked again today.
func (s *Service) refreshBooks(ctx context.Context, books []*ent.Book) error {
	prov, err := s.provider()
	if err != nil {
		return err
	}
	ids := make([]uint32, 0, len(books))
	for _, b := range books {
		ids = append(ids, b.HardcoverID)
	}
	recs, err := prov.GetBooksFresh(ctx, ids)
	if err != nil {
		return err
	}
	byID := make(map[uint32]*metadata.BookRecord, len(recs))
	for _, r := range recs {
		byID[r.HardcoverID] = r
	}
	for _, b := range books {
		rec := byID[b.HardcoverID]
		if rec == nil {
			if err := s.db.MarkBookRefreshed(ctx, b.ID, time.Now()); err != nil {
				return err
			}
			continue
		}
		full, err := s.db.FindBookByID(ctx, b.ID)
		if err != nil {
			return err
		}
		if err := s.applyRecord(ctx, full, rec); err != nil {
			return err
		}
	}
	return nil
}

// RefreshSeries re-reads a series: its skeleton, then every volume in batches
// of twenty, which is one request for the skeleton plus one per twenty
// volumes. Volumes new to the skeleton are added under the series policy and
// edition, a volume already in the library as a standalone book is adopted, and
// a volume of another series is skipped.
func (s *Service) RefreshSeries(
	ctx context.Context,
	id uint32,
) (*ent.BookSeries, error) {
	ctx, span := tracer.Start(ctx, "book.refresh_series",
		trace.WithAttributes(attribute.Int("series.id", int(id))))
	defer span.End()

	prov, err := s.provider()
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	row, err := s.GetSeries(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	sk, err := prov.GetSeries(ctx, row.HardcoverID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("get series: %w", err))
	}
	if sk == nil {
		return nil, otelx.RecordSpanError(span, ErrHardcoverNotFound)
	}

	vols := orderedVolumes(sk.Volumes)
	ids := make([]uint32, 0, len(vols))
	for _, v := range vols {
		ids = append(ids, v.BookHardcoverID)
	}
	recs, err := prov.GetBooksFresh(ctx, ids)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("get volumes: %w", err))
	}
	byID := make(map[uint32]*metadata.BookRecord, len(recs))
	for _, r := range recs {
		byID[r.HardcoverID] = r
	}
	held := make(map[uint32]*ent.Book, len(row.Edges.Volumes))
	for _, v := range row.Edges.Volumes {
		held[v.HardcoverID] = v
	}

	now := time.Now()
	lang := cmpOrString(row.EditionLanguage, config.Get().Library.BookLanguage)
	var (
		lead     *metadata.BookRecord
		fresh    []db.BookSeed
		freshRec = map[uint32]*metadata.BookRecord{}
		jobs     []artJob
	)
	for _, v := range vols {
		rec := byID[v.BookHardcoverID]
		if rec == nil || rec.Compilation {
			continue
		}
		if lead == nil {
			lead = rec
		}
		if b, ok := held[v.BookHardcoverID]; ok {
			b.Edges.Series = row
			if err := s.applyRecord(ctx, b, rec); err != nil {
				return nil, otelx.RecordSpanError(span, err)
			}
			continue
		}
		pos := v.Position
		fresh = append(fresh, bookSeed(rec, seedOptions{
			language: lang,
			profile:  row.QualityProfile,
			ebook: pick.MonitorsVolume(
				string(row.Monitor),
				rec.ReleaseDate,
				row.CreateTime,
			),
			position:  &pos,
			refreshed: now,
		}))
		freshRec[rec.HardcoverID] = rec
	}
	if len(fresh) > 0 {
		counts, err := s.db.AddSeriesVolumes(ctx, id, row.QualityProfile, fresh)
		if err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		slog.InfoContext(ctx, "series refresh found volumes",
			"series.id", id, "created", counts.Created,
			"adopted", counts.Adopted, "skipped", counts.Skipped)
		for _, seed := range fresh {
			b, err := s.db.FindBookByHardcoverID(ctx, seed.HardcoverID)
			if err != nil {
				return nil, otelx.RecordSpanError(span, err)
			}
			if b == nil || b.Edges.Series == nil || b.Edges.Series.ID != id {
				continue
			}
			rec := freshRec[seed.HardcoverID]
			if err := s.settleVolume(ctx, b, row, rec); err != nil {
				return nil, otelx.RecordSpanError(span, err)
			}
			jobs = append(jobs, artJob{bookID: b.ID, cover: rec.CoverURL})
		}
	}

	f := seriesFieldsFrom(sk, lead)
	if err := s.db.ApplySeriesMetadata(ctx, id, db.SeriesMetadata{
		Title:         f.title,
		OriginalTitle: f.original,
		Overview:      f.overview,
		AuthorName:    f.author,
		Ongoing:       f.ongoing,
		Credits:       f.credits,
		RefreshedAt:   now,
	}); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if err := s.db.RefreshSeriesStats(ctx, id); err != nil {
		slog.WarnContext(
			ctx,
			"series stats not computed",
			"series.id",
			id,
			"error",
			err,
		)
	}
	out, err := s.GetSeries(ctx, id)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	jobs = append(jobs, artJob{authors: creatorAuthors(out.Edges.Contributions)})
	s.fetchArtInBackground(ctx, jobs...)
	return out, nil
}

func cmpOrString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// RefreshStale is the scheduled metadata refresh. In one tick it hydrates the
// series stubs first, then the oldest-refreshed standalone books in batches of
// twenty, then the oldest series, each bounded so a tick stays inside the
// request budget. It stops when only the scan reserve is left or at the first
// rate limit, and returns nil either way: the rest waits for the next tick.
// Without a Hardcover key there is nothing to refresh from.
func (s *Service) RefreshStale(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "book.refresh_stale")
	defer span.End()

	if s.metadata == nil {
		return nil
	}
	cutoff := time.Now()
	if !scheduler.Manual(ctx) {
		cutoff = cutoff.Add(-metadataMinRefreshInterval)
	}
	stubs, err := s.db.ListHydrationStubs(ctx, refreshBookLimit)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	books, err := s.db.ListStaleStandaloneBooks(ctx, cutoff, refreshBookLimit)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	series, err := s.db.ListStaleSeries(ctx, cutoff, refreshSeriesLimit)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	total := len(stubs) + len(books) + len(series)
	span.SetAttributes(attribute.Int("refresh.candidate_count", total))

	done, failed := 0, 0
	stop := func(err error) bool {
		if errors.Is(err, metadata.ErrRateLimited) {
			slog.WarnContext(ctx, "book refresh stopped: hardcover rate limited",
				"refreshed", done, "error", err)
			return true
		}
		return false
	}
	step := func(n int, err error, what string) (halt bool) {
		if err == nil {
			done += n
			return false
		}
		if stop(err) {
			return true
		}
		slog.WarnContext(ctx, "book refresh failed", "step", what, "error", err)
		failed += n
		return false
	}

	for chunk := range slices.Chunk(stubs, hydrateBatch) {
		if s.budgetLow() {
			return nil
		}
		scheduler.Progress(ctx, done+failed, total)
		n, err := s.hydrateStubs(ctx, chunk)
		if step(n, err, "hydrate") {
			return nil
		}
	}
	for chunk := range slices.Chunk(books, hydrateBatch) {
		if s.budgetLow() {
			return nil
		}
		scheduler.Progress(ctx, done+failed, total)
		if step(len(chunk), s.refreshBooks(ctx, chunk), "books") {
			return nil
		}
	}
	for _, sr := range series {
		if s.budgetLow() {
			return nil
		}
		scheduler.Progress(ctx, done+failed, total)
		_, err := s.RefreshSeries(ctx, sr.ID)
		if step(1, err, "series") {
			return nil
		}
	}
	scheduler.Progress(ctx, total, total)

	span.SetAttributes(
		attribute.Int("refresh.refreshed_count", done),
		attribute.Int("refresh.skipped_count", failed),
	)
	slog.InfoContext(ctx, "book metadata refresh complete",
		"refreshed", done, "skipped", failed)
	return nil
}
