package book

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	entbook "github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/searchwindow"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/scheduler"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

// maxVolumeFallbacks caps the per-volume queries one series pass makes for the
// volumes its single series query did not turn up. A 24-volume backlog would
// otherwise be 24 queries per indexer per pass.
const maxVolumeFallbacks = 3

// MissingSearcher runs one backlog search pass over the wanted book slots.
type MissingSearcher interface {
	SearchMissing(ctx context.Context) error
}

var _ MissingSearcher = (*Service)(nil)

type slotWork struct {
	kind string
	book *ent.Book
}

// SearchMissing searches the indexers for every eligible wanted slot and
// grabs the best accepted release. Standalone books are searched slot by slot;
// the ebook volumes of a series are grouped and searched together, so a
// series costs one query set per pass. Per-slot failures are logged and never
// abort the pass; the error return is for the initial queries, a bad config
// window or ctx cancellation.
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
	volumes, err := s.db.ListEligibleSeriesVolumes(
		ctx, window.MaxGrabFailures, window.NotSearchedSince,
	)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	groups := groupBySeries(volumes)

	total := len(work) + len(groups)
	span.SetAttributes(attribute.Int("eligible.count", total))
	if total == 0 {
		return nil
	}
	if _, ok := config.PickDownloadClient(); !ok {
		slog.InfoContext(ctx, "book missing-search: no enabled download client",
			"eligible", total)
		return nil
	}

	grabbed := 0
	for i, w := range work {
		if err := ctx.Err(); err != nil {
			return otelx.RecordSpanError(span, err)
		}
		scheduler.Progress(ctx, i, total)
		if s.searchSlot(ctx, w.book, w.kind) {
			grabbed++
		}
	}
	for i, g := range groups {
		if err := ctx.Err(); err != nil {
			return otelx.RecordSpanError(span, err)
		}
		scheduler.Progress(ctx, len(work)+i, total)
		grabbed += s.searchSeriesVolumes(ctx, g.series, g.volumes)
	}
	scheduler.Progress(ctx, total, total)
	slog.InfoContext(ctx, "book missing-search pass complete",
		"eligible", total, "grabbed", grabbed)
	return nil
}

type seriesGroup struct {
	series  *ent.BookSeries
	volumes []*ent.Book
}

func groupBySeries(volumes []*ent.Book) []seriesGroup {
	var groups []seriesGroup
	at := map[uint32]int{}
	for _, v := range volumes {
		sr := v.Edges.Series
		if sr == nil {
			continue
		}
		i, ok := at[sr.ID]
		if !ok {
			i = len(groups)
			at[sr.ID] = i
			groups = append(groups, seriesGroup{series: sr})
		}
		groups[i].volumes = append(groups[i].volumes, v)
	}
	return groups
}

// searchSlot reports whether a release was grabbed. last_search_at is stamped
// whenever the indexers answered, so the cooldown advances on an empty result
// too; a failed search stamps nothing and counts no strike. Rejected releases
// are kept for the UI and never grabbed here.
func (s *Service) searchSlot(ctx context.Context, b *ent.Book, kind string) bool {
	ctx, span := tracer.Start(ctx, "book.search_missing.slot",
		trace.WithAttributes(
			attribute.Int("book.id", int(b.ID)),
			attribute.String("book.kind", kind),
		))
	defer span.End()

	releases, err := s.slotReleases(ctx, b, kind, true)
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
	sortReleases(releases)
	return s.grabBest(ctx, span, b, kind, namingBook(releases, b))
}

// namingBook keeps the releases whose name is this book by this author. An
// indexer answers a search with whatever loosely matches it, and an automatic
// grab has no person to notice that "Author - Other Book" is not the book.
func namingBook(releases []ReleaseResult, b *ent.Book) []ReleaseResult {
	return slices.DeleteFunc(slices.Clone(releases), func(r ReleaseResult) bool {
		creator, title, ok := library.SplitCreatorTitle(r.Title)
		if !ok ||
			(b.AuthorName != "" && !library.TitleMatchesStrict(creator, b.AuthorName)) {
			return true
		}
		return !library.TitleNamesSameWork(title, b.Title) &&
			(b.OriginalTitle == "" || !library.TitleNamesSameWork(title, b.OriginalTitle))
	})
}

// grabBest grabs the first accepted release of a sorted list, counts a strike
// on a refusal that is about the release, and clears the strikes on success.
func (s *Service) grabBest(
	ctx context.Context,
	span trace.Span,
	b *ent.Book,
	kind string,
	releases []ReleaseResult,
) bool {
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

// volumeReleases groups what a series query returned by the volume each
// release names. Releases that name no volume, another work, or a pack of
// volumes are dropped: a pack is a collection and never grabbed.
func volumeReleases(
	found []indexer.SearchResult,
	series *ent.BookSeries,
) map[float64][]indexer.SearchResult {
	out := map[float64][]indexer.SearchResult{}
	for _, r := range found {
		p := library.ParseBookRelease(r.Title)
		if p.Volume == nil || p.Collection {
			continue
		}
		if !library.TitleNamesSameWork(p.VolumePrefix, series.Title) &&
			(series.OriginalTitle == "" ||
				!library.TitleNamesSameWork(p.VolumePrefix, series.OriginalTitle)) {
			continue
		}
		out[*p.Volume] = append(out[*p.Volume], r)
	}
	return out
}

// bestForVolume judges the candidate releases of one volume and returns them
// sorted, best first.
func bestForVolume(
	v *ent.Book,
	candidates []indexer.SearchResult,
) ([]ReleaseResult, bool) {
	profile, ok := ProfileFor(v)
	if !ok {
		return nil, false
	}
	out := make([]ReleaseResult, 0, len(candidates))
	for _, r := range candidates {
		res := judge(v, slotEbook, profile, r, true)
		if res.Slot == slotEbook {
			out = append(out, res)
		}
	}
	sortReleases(out)
	return out, true
}

// searchSeriesVolumes searches the wanted volumes of one series with a single
// series query, parsing the volume out of each release; each volume takes the
// best accepted match. At most maxVolumeFallbacks volumes the query missed get
// a query of their own. It returns how many volumes it grabbed.
func (s *Service) searchSeriesVolumes(
	ctx context.Context,
	series *ent.BookSeries,
	vols []*ent.Book,
) int {
	ctx, span := tracer.Start(ctx, "book.search_series_missing",
		trace.WithAttributes(
			attribute.Int("series.id", int(series.ID)),
			attribute.Int("series.wanted_volumes", len(vols)),
		))
	defer span.End()

	for _, v := range vols {
		v.Edges.Series = series
	}
	found, err := s.indexers.SearchBook(
		ctx, series.AuthorName, series.Title, 0, mediafile.BookKindEbook,
	)
	if err != nil {
		otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "series missing-search: search failed",
			"series.id", series.ID, "error", err)
		return 0
	}
	byVolume := volumeReleases(found, series)

	grabbed := 0
	var missed []*ent.Book
	for _, v := range vols {
		if v.SeriesPosition == nil {
			continue
		}
		if err := s.db.SetBookSlotLastSearchAt(
			ctx,
			v.ID,
			slotEbook,
			time.Now(),
		); err != nil {
			slog.WarnContext(
				ctx,
				"series missing-search: stamp last_search_at failed",
				"book.id",
				v.ID,
				"error",
				err,
			)
		}
		releases, ok := bestForVolume(v, byVolume[*v.SeriesPosition])
		if !ok {
			slog.WarnContext(ctx, "series missing-search: no quality profile",
				"book.id", v.ID)
			continue
		}
		if s.grabBest(ctx, span, v, slotEbook, releases) {
			grabbed++
			continue
		}
		missed = append(missed, v)
	}
	for i, v := range missed {
		if i == maxVolumeFallbacks {
			break
		}
		query := fmt.Sprintf("%s %s", series.Title, positionLabel(*v.SeriesPosition))
		more, err := s.indexers.SearchBook(
			ctx, series.AuthorName, query, 0, mediafile.BookKindEbook,
		)
		if err != nil {
			otelx.RecordSpanError(span, err)
			slog.WarnContext(ctx, "series missing-search: volume search failed",
				"book.id", v.ID, "error", err)
			continue
		}
		releases, ok := bestForVolume(
			v,
			volumeReleases(more, series)[*v.SeriesPosition],
		)
		if ok && s.grabBest(ctx, span, v, slotEbook, releases) {
			grabbed++
		}
	}
	span.SetAttributes(attribute.Int("series.grabbed", grabbed))
	return grabbed
}

// wantedSlot reports a slot a search could act on: monitored, wanted, and on a
// book that is hydrated and released.
func wantedSlot(b *ent.Book, kind string, now time.Time) bool {
	if b.LastRefreshedAt == nil || unreleased(b, now) {
		return false
	}
	if kind == slotAudiobook {
		return b.AudiobookMonitored &&
			b.AudiobookStatus == entbook.AudiobookStatusWanted
	}
	return b.EbookMonitored && b.EbookStatus == entbook.EbookStatusWanted
}

// SearchNowBook searches, in the background, every monitored wanted slot of a
// book (or the one asked for) and grabs the best accepted release of each. It
// returns how many slots the pass will search.
func (s *Service) SearchNowBook(
	ctx context.Context,
	id uint32,
	kind string,
) (uint32, error) {
	ctx, span := tracer.Start(ctx, "book.search_now",
		trace.WithAttributes(attribute.Int("book.id", int(id))))
	defer span.End()

	kinds := slotKinds
	if kind != "" {
		if !validSlotKind(kind) {
			return 0, otelx.RecordSpanError(span, ErrInvalidSlotKind)
		}
		kinds = []string{kind}
	}
	b, err := s.GetBook(ctx, id)
	if err != nil {
		return 0, otelx.RecordSpanError(span, err)
	}
	now := time.Now()
	var slots []string
	for _, k := range kinds {
		if wantedSlot(b, k, now) {
			slots = append(slots, k)
		}
	}
	if len(slots) == 0 {
		return 0, nil
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		defer observability.RecoverPanic(bg, "book.search_now", nil)
		for _, k := range slots {
			s.searchSlot(bg, b, k)
		}
	}()
	return numeric.SaturateU32(len(slots)), nil
}

// SearchNowSeries searches, in the background, the wanted volumes of a series
// with one series query and returns how many volumes the pass will search.
func (s *Service) SearchNowSeries(ctx context.Context, id uint32) (uint32, error) {
	ctx, span := tracer.Start(ctx, "book.search_now_series",
		trace.WithAttributes(attribute.Int("series.id", int(id))))
	defer span.End()

	series, err := s.GetSeries(ctx, id)
	if err != nil {
		return 0, otelx.RecordSpanError(span, err)
	}
	now := time.Now()
	var wanted []*ent.Book
	for _, v := range series.Edges.Volumes {
		if wantedSlot(v, slotEbook, now) && v.SeriesPosition != nil {
			wanted = append(wanted, v)
		}
	}
	if len(wanted) == 0 {
		return 0, nil
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		defer observability.RecoverPanic(bg, "book.search_now_series", nil)
		s.searchSeriesVolumes(bg, series, wanted)
	}()
	return numeric.SaturateU32(len(wanted)), nil
}
