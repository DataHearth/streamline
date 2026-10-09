package book

import (
	"cmp"
	"context"
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
)

// hydrateBatch is how many volumes one Hardcover batch request carries. It
// mirrors the client's own limit, which the service cannot import.
const hydrateBatch = 20

type AddBookParams struct {
	HardcoverID    uint32
	Monitor        string // both | ebook | audiobook | none; empty is both
	QualityProfile string
}

type AddSeriesParams struct {
	HardcoverID    uint32
	Monitor        string // all | future | none; empty is all
	QualityProfile string
}

// seedOptions are the user-side facts a book is created with.
type seedOptions struct {
	language  string
	profile   string
	ebook     bool
	audiobook bool
	position  *float64
	refreshed time.Time
}

// bookSeed turns a Hardcover record into a book row to create. The row's
// titles are derived from its editions when it is written; Title here is only
// the fallback.
func bookSeed(rec *metadata.BookRecord, o seedOptions) db.BookSeed {
	refreshed := o.refreshed
	return db.BookSeed{
		HardcoverID:        rec.HardcoverID,
		Title:              rec.Title,
		AuthorName:         pick.DisplayAuthor(recordCredits(rec.Credits)),
		Kind:               rec.Kind,
		Genre:              rec.Genre,
		Overview:           rec.Description,
		RatingTenths:       ratingTenths(rec.Rating),
		ReleaseYear:        yearPtr(rec.ReleaseYear),
		ReleaseDate:        rec.ReleaseDate,
		PreferredLanguage:  o.language,
		QualityProfile:     o.profile,
		SeriesPosition:     o.position,
		EbookMonitored:     o.ebook,
		AudiobookMonitored: o.audiobook,
		Editions:           dbEditions(rec.Editions),
		Credits:            dbCredits(rec.Credits),
		RefreshedAt:        &refreshed,
	}
}

func bookMetadata(
	rec *metadata.BookRecord,
	setKind bool,
	at time.Time,
) db.BookMetadata {
	m := db.BookMetadata{
		Title:        rec.Title,
		AuthorName:   pick.DisplayAuthor(recordCredits(rec.Credits)),
		Genre:        rec.Genre,
		Overview:     rec.Description,
		RatingTenths: ratingTenths(rec.Rating),
		ReleaseYear:  yearPtr(rec.ReleaseYear),
		ReleaseDate:  rec.ReleaseDate,
		Editions:     dbEditions(rec.Editions),
		Credits:      dbCredits(rec.Credits),
		RefreshedAt:  at,
	}
	if setKind {
		m.Kind = rec.Kind
	}
	return m
}

// AddBook adds one book: a single Hardcover request, one transaction for the
// book with its editions, contributions and people, then the cover and the
// creators' photos in the background.
func (s *Service) AddBook(ctx context.Context, p AddBookParams) (*ent.Book, error) {
	ctx, span := tracer.Start(ctx, "book.add",
		trace.WithAttributes(attribute.Int("hardcover_id", int(p.HardcoverID))))
	defer span.End()

	prov, err := s.provider()
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if p.Monitor == "" {
		p.Monitor = MonitorBoth
	}
	ebook, audiobook, err := slotFlags(p.Monitor)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if err := checkProfile(p.QualityProfile); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	existing, err := s.db.FindBookByHardcoverID(ctx, p.HardcoverID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if existing != nil {
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("%w: hardcover id %d", ErrBookExists, p.HardcoverID),
		)
	}

	recs, err := prov.GetBooks(ctx, []uint32{p.HardcoverID})
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("get book: %w", err))
	}
	if len(recs) == 0 {
		return nil, otelx.RecordSpanError(span, ErrHardcoverNotFound)
	}
	rec := recs[0]

	row, err := s.db.CreateBook(ctx, bookSeed(rec, seedOptions{
		language:  config.Get().Library.BookLanguage,
		profile:   p.QualityProfile,
		ebook:     ebook,
		audiobook: audiobook,
		refreshed: time.Now(),
	}))
	if err != nil {
		if ent.IsConstraintError(err) {
			err = fmt.Errorf("%w: hardcover id %d", ErrBookExists, p.HardcoverID)
		}
		return nil, otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("book.id", int(row.ID)))

	s.fetchArtInBackground(ctx, jobFor(row, rec.CoverURL))
	slog.InfoContext(ctx, "book added",
		"book.id", row.ID, "hardcover_id", row.HardcoverID, "monitor", p.Monitor)
	return row, nil
}

// orderedVolumes sorts a skeleton by position, then book id, and drops a book
// listed twice at one position. Every candidate for a position stays: which
// one is the volume is decided once their records are known.
func orderedVolumes(in []metadata.SeriesVolumeRef) []metadata.SeriesVolumeRef {
	out := slices.Clone(in)
	slices.SortStableFunc(out, func(a, b metadata.SeriesVolumeRef) int {
		return cmp.Or(
			cmp.Compare(a.Position, b.Position),
			cmp.Compare(a.BookHardcoverID, b.BookHardcoverID),
		)
	})
	return slices.Compact(out)
}

// firstPerPosition keeps the lowest book id at each position of an ordered
// skeleton, for callers that have no records to choose with.
func firstPerPosition(vols []metadata.SeriesVolumeRef) []metadata.SeriesVolumeRef {
	return slices.CompactFunc(
		slices.Clone(vols),
		func(a, b metadata.SeriesVolumeRef) bool { return a.Position == b.Position },
	)
}

// positionWinners picks, at each position of an ordered skeleton, the
// non-compilation record with the most readers; the lowest book id wins a
// tie. A position whose candidates are all compilations or unknown has none.
func positionWinners(
	vols []metadata.SeriesVolumeRef,
	byID map[uint32]*metadata.BookRecord,
) map[uint32]bool {
	best := map[float64]uint32{}
	for _, v := range vols {
		rec := byID[v.BookHardcoverID]
		if rec == nil || rec.Compilation {
			continue
		}
		if cur, ok := best[v.Position]; !ok ||
			rec.UsersCount > byID[cur].UsersCount {
			best[v.Position] = v.BookHardcoverID
		}
	}
	out := make(map[uint32]bool, len(best))
	for _, id := range best {
		out[id] = true
	}
	return out
}

// seriesCredits lists a series' people: its author, the makers of its first
// volume, and the translators of that volume's editions per language.
func seriesCredits(
	sk *metadata.SeriesRecord,
	lead *metadata.BookRecord,
) []metadata.BookCredit {
	var out []metadata.BookCredit
	add := func(c metadata.BookCredit) {
		if c.AuthorHardcoverID == 0 || c.Name == "" {
			return
		}
		if slices.ContainsFunc(out, func(x metadata.BookCredit) bool {
			return x.AuthorHardcoverID == c.AuthorHardcoverID && x.Role == c.Role
		}) {
			return
		}
		out = append(out, c)
	}
	add(metadata.BookCredit{
		AuthorHardcoverID: sk.AuthorHardcoverID,
		Name:              sk.AuthorName,
		ImageURL:          sk.AuthorImageURL,
		Role:              metadata.RoleAuthor,
	})
	if lead == nil {
		return out
	}
	for _, c := range lead.Credits {
		add(c)
	}
	return out
}

// translatorSeeds lists one translator per language from a volume's editions.
func translatorSeeds(lead *metadata.BookRecord, start uint8) []db.CreditSeed {
	if lead == nil {
		return nil
	}
	var out []db.CreditSeed
	seen := map[string]bool{}
	for _, e := range lead.Editions {
		if e.Translator == "" || e.TranslatorID == 0 || seen[e.Language] {
			continue
		}
		seen[e.Language] = true
		out = append(out, db.CreditSeed{
			AuthorHardcoverID: e.TranslatorID,
			Name:              e.Translator,
			Role:              metadata.RoleTranslator,
			Language:          e.Language,
			Order:             orderOf(int(start) + len(out)),
		})
	}
	return out
}

func stubSeed(
	sk *metadata.SeriesRecord,
	v metadata.SeriesVolumeRef,
	kind, author, language, profile string,
	ebook bool,
) db.BookSeed {
	pos := v.Position
	return db.BookSeed{
		HardcoverID:       v.BookHardcoverID,
		Title:             fmt.Sprintf("%s #%s", sk.Name, positionLabel(v.Position)),
		AuthorName:        author,
		Kind:              kind,
		PreferredLanguage: language,
		QualityProfile:    profile,
		SeriesPosition:    &pos,
		EbookMonitored:    ebook,
	}
}

// seriesFields are the values a series row is written with from its skeleton
// and first volume.
type seriesFields struct {
	title, original, overview, author, kind string
	ongoing                                 bool
	credits                                 []db.CreditSeed
}

func seriesFieldsFrom(
	sk *metadata.SeriesRecord,
	lead *metadata.BookRecord,
) seriesFields {
	f := seriesFields{
		title:    sk.Name,
		overview: sk.Description,
		author:   sk.AuthorName,
		kind:     metadata.BookKindNovel,
		ongoing:  !sk.Completed,
	}
	credits := dbCredits(seriesCredits(sk, lead))
	f.credits = append(
		credits,
		translatorSeeds(lead, orderOf(len(credits)))...)
	if lead == nil {
		return f
	}
	f.kind = lead.Kind
	if lead.OriginalTitle != "" && lead.OriginalTitle != sk.Name {
		f.original = lead.OriginalTitle
	}
	if f.author == "" {
		f.author = pick.DisplayAuthor(recordCredits(lead.Credits))
	}
	return f
}

// AddSeries is two-phase. Phase one is synchronous and costs two Hardcover
// requests, the skeleton and the first batch of volumes: one transaction
// writes the series, those volumes fully hydrated and a stub for every other
// position, and the series is returned with hydrating set. Phase two is the
// hydration worker, which fills the stubs in batches.
func (s *Service) AddSeries(
	ctx context.Context,
	p AddSeriesParams,
) (*ent.BookSeries, error) {
	ctx, span := tracer.Start(ctx, "book.add_series",
		trace.WithAttributes(attribute.Int("hardcover_id", int(p.HardcoverID))))
	defer span.End()

	prov, err := s.provider()
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if p.Monitor == "" {
		p.Monitor = PolicyAll
	}
	if !validPolicy(p.Monitor) {
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("%w: %q", ErrInvalidMonitor, p.Monitor),
		)
	}
	if err := checkProfile(p.QualityProfile); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	existing, err := s.db.FindSeriesByHardcoverID(ctx, p.HardcoverID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if existing != nil {
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("%w: hardcover id %d", ErrSeriesExists, p.HardcoverID),
		)
	}

	sk, err := prov.GetSeries(ctx, p.HardcoverID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("get series: %w", err))
	}
	if sk == nil {
		return nil, otelx.RecordSpanError(span, ErrHardcoverNotFound)
	}
	vols := orderedVolumes(sk.Volumes)
	firstN := min(len(vols), hydrateBatch)
	for firstN < len(vols) && vols[firstN].Position == vols[firstN-1].Position {
		firstN++
	}
	var firstIDs []uint32
	for _, v := range vols[:firstN] {
		firstIDs = append(firstIDs, v.BookHardcoverID)
	}
	recs, err := prov.GetBooks(ctx, firstIDs)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("get volumes: %w", err))
	}
	byID := make(map[uint32]*metadata.BookRecord, len(recs))
	for _, r := range recs {
		byID[r.HardcoverID] = r
	}

	now := time.Now()
	lang := config.Get().Library.BookLanguage
	win := positionWinners(vols[:firstN], byID)
	var (
		seeds   []db.BookSeed
		lead    *metadata.BookRecord
		covers  = map[uint32]string{}
		stubsOf = vols[firstN:]
	)
	for _, v := range vols[:firstN] {
		rec := byID[v.BookHardcoverID]
		if rec == nil || !win[v.BookHardcoverID] {
			continue
		}
		pos := v.Position
		seeds = append(seeds, bookSeed(rec, seedOptions{
			language:  lang,
			profile:   p.QualityProfile,
			ebook:     pick.MonitorsVolume(p.Monitor, rec.ReleaseDate, now),
			position:  &pos,
			refreshed: now,
		}))
		covers[rec.HardcoverID] = rec.CoverURL
		if lead == nil {
			lead = rec
		}
	}
	f := seriesFieldsFrom(sk, lead)
	for _, v := range stubsOf {
		seeds = append(seeds, stubSeed(
			sk, v, f.kind, f.author, lang, p.QualityProfile, p.Monitor == PolicyAll,
		))
	}

	params := db.CreateSeriesParams{
		HardcoverID:     sk.HardcoverID,
		Title:           f.title,
		OriginalTitle:   f.original,
		Overview:        f.overview,
		AuthorName:      f.author,
		Kind:            f.kind,
		QualityProfile:  p.QualityProfile,
		Ongoing:         f.ongoing,
		Monitor:         p.Monitor,
		EditionLanguage: lang,
		RefreshedAt:     now,
		Credits:         f.credits,
		Volumes:         seeds,
	}
	if lead != nil {
		if ed, ok := pick.Slot(
			recordEditionViews(lead.Editions),
			"ebook",
			lang,
		); ok {
			params.EditionLanguage, params.EditionPublisher = ed.Language, ed.Publisher
		}
	}
	row, counts, err := s.db.CreateSeries(ctx, params)
	if err != nil {
		if ent.IsConstraintError(err) {
			err = fmt.Errorf("%w: hardcover id %d", ErrSeriesExists, p.HardcoverID)
		}
		return nil, otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(
		attribute.Int("series.id", int(row.ID)),
		attribute.Int("series.volumes", len(seeds)),
	)
	if err := s.db.RefreshSeriesStats(ctx, row.ID); err != nil {
		slog.WarnContext(ctx, "series stats not computed",
			"series.id", row.ID, "error", err)
	}
	if counts.Skipped > 0 {
		slog.InfoContext(
			ctx,
			"series volumes skipped: they belong to another series",
			"series.id",
			row.ID,
			"skipped",
			counts.Skipped,
		)
	}

	row, err = s.db.FindSeriesByID(ctx, row.ID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	jobs := make([]artJob, 0, len(row.Edges.Volumes)+1)
	jobs = append(jobs, artJob{authors: creatorAuthors(row.Edges.Contributions)})
	for _, v := range row.Edges.Volumes {
		if url := covers[v.HardcoverID]; url != "" {
			jobs = append(jobs, artJob{bookID: v.ID, cover: url})
		}
	}
	s.fetchArtInBackground(ctx, jobs...)
	if len(stubsOf) > 0 {
		s.HydrateSeriesInBackground(ctx)
	}
	slog.InfoContext(ctx, "series added",
		"series.id", row.ID, "hardcover_id", row.HardcoverID,
		"volumes", len(seeds), "stubs", len(stubsOf), "adopted", counts.Adopted)
	return row, nil
}
