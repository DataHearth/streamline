package book

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/artwork"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/book/pick"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
)

const (
	lookupBook   = "book"
	lookupSeries = "series"
	lookupAll    = "all"

	lookupCap        = 20
	lookupCovers     = 3
	detailVolumeIDs  = 6
	detailGenreLimit = 3
)

// LookupHit is one hit of the add flow. Kind, original title, ongoing and
// volume count are best effort on a search hit and filled by the detail.
type LookupHit struct {
	HardcoverID   uint32
	Type          string
	Title         string
	OriginalTitle string
	Author        string
	Kind          string
	Year          uint16
	Volumes       uint32
	Ongoing       *bool
	AlreadyAdded  bool
	// LibraryID is the Book or BookSeries id of a hit already in the library.
	LibraryID uint32
	// CoverID is the lowest-position volume of an added series, whose poster
	// is the series' cover.
	CoverID uint32
	// Slug is a series' hardcover.app/series path segment.
	Slug string
	// CoverBookIDs are the Hardcover ids of the books adding a series would
	// pick at its first positions, whose covers stand for it.
	CoverBookIDs []uint32
	// MatchedTitle is the book title, in whatever language, through which a
	// series was found, when it is not the series' own name.
	MatchedTitle string
}

// LookupEdition is one edition a title would be added with.
type LookupEdition struct {
	Language  string
	Format    string
	Publisher string
	Year      uint16
	Original  bool
}

// LookupDetail is a hit with what the detail query adds to it.
type LookupDetail struct {
	LookupHit
	Overview string
	Genres   []string
	Pages    uint16
	Editions []LookupEdition
	// VolumeBookIDs are the Hardcover ids of the first volumes of a series,
	// whose covers the panel shows.
	VolumeBookIDs []uint32
}

// Lookup searches Hardcover for books and series to add. Books alone are one
// request. Series are three: the book search, because a series is also found
// through any of its books whose title, in whatever language, is the query; the
// series search; and one batch reading every candidate series. Each is
// memoised by the provider; the library join is read fresh every time.
func (s *Service) Lookup(
	ctx context.Context,
	query, kind string,
) ([]LookupHit, error) {
	ctx, span := tracer.Start(ctx, "book.lookup",
		trace.WithAttributes(attribute.String("lookup.type", kind)))
	defer span.End()

	p, err := s.provider()
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	books, err := p.LookupBooks(ctx, query)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	var top, rest []LookupHit
	if kind != lookupBook {
		series, err := p.LookupSeries(ctx, query)
		if err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		if top, rest, err = seriesHits(ctx, p, query, books, series); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}

	limit := max(lookupCap, len(top))
	out := make([]LookupHit, 0, limit)
	out = append(out, top...)
	if kind != lookupSeries {
		for _, b := range authorMatchedFirst(query, books) {
			if len(out) == limit {
				break
			}
			out = append(out, hitFrom(b))
			rememberCover(b.HardcoverID, b.ImageURL)
		}
	}
	out = append(out, rest[:min(len(rest), limit-len(out))]...)
	if err := s.markAdded(ctx, out); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	return out, nil
}

// seriesHits splits a lookup's series in two. top is the series named like the
// query, then the ones reached through a book whose title is the query, in the
// books' order: they are what the user typed, so the cap never drops them.
// rest is the other series the series search found. A series is listed once,
// at its first place, and one with no primary volume (Hardcover's empty
// translated duplicates) not at all.
func seriesHits(
	ctx context.Context,
	p metadata.BookProvider,
	query string,
	books, series []metadata.BookLookupHit,
) ([]LookupHit, []LookupHit, error) {
	reached := seriesReached(query, books)
	ids := make([]uint32, 0, len(series)+len(reached))
	for _, sr := range series {
		ids = append(ids, sr.HardcoverID)
	}
	for _, r := range reached {
		ids = append(ids, r.id)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) == 0 {
		return nil, nil, nil
	}
	recs, err := p.GetSeriesLookup(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[uint32]*metadata.SeriesLookupRecord, len(recs))
	for i := range recs {
		byID[recs[i].HardcoverID] = &recs[i]
	}

	var top, rest []LookupHit
	searched := map[uint32]metadata.BookLookupHit{}
	placed := map[uint32]bool{}
	for _, sr := range series {
		rec := byID[sr.HardcoverID]
		if placed[sr.HardcoverID] || (rec != nil && rec.PrimaryBooks == 0) {
			continue
		}
		searched[sr.HardcoverID] = sr
		if library.TitleMatches(sr.Title, query) ||
			library.TitlePrefixMatches(sr.Title, query) {
			placed[sr.HardcoverID] = true
			top = append(top, seriesHit(sr, rec))
		}
	}
	for _, r := range reached {
		rec := byID[r.id]
		if placed[r.id] || rec == nil || rec.PrimaryBooks == 0 {
			continue
		}
		base, ok := searched[r.id]
		if !ok {
			base = metadata.BookLookupHit{HardcoverID: r.id, Type: lookupSeries}
		}
		h := seriesHit(base, rec)
		if !library.TitleMatches(r.via, h.Title) {
			h.MatchedTitle = r.via
		}
		placed[r.id] = true
		top = append(top, h)
	}
	for _, sr := range series {
		if _, ok := searched[sr.HardcoverID]; !ok || placed[sr.HardcoverID] {
			continue
		}
		placed[sr.HardcoverID] = true
		rest = append(rest, seriesHit(sr, byID[sr.HardcoverID]))
	}
	return top, rest, nil
}

type reachedSeries struct {
	id  uint32
	via string
}

// seriesReached lists, in the books' order, every series of each book whose
// title or an alternative title is the query, with that title.
func seriesReached(query string, books []metadata.BookLookupHit) []reachedSeries {
	var out []reachedSeries
	seen := map[uint32]bool{}
	for _, b := range books {
		via := titleNaming(query, b)
		if via == "" {
			continue
		}
		for _, id := range b.SeriesIDs {
			if !seen[id] {
				seen[id] = true
				out = append(out, reachedSeries{id: id, via: via})
			}
		}
	}
	return out
}

// titleNaming is the first of a book's title and alternative titles equal to
// the query once folded, else the first holding every word of it; empty when
// none does.
func titleNaming(query string, b metadata.BookLookupHit) string {
	titles := append([]string{b.Title}, b.AlternativeTitles...)
	for _, t := range titles {
		if library.TitleMatchesStrict(t, query) {
			return t
		}
	}
	q := queryTokens(query)
	if len(q) == 0 {
		return ""
	}
	for _, t := range titles {
		if titleCovers(queryTokens(t), q) {
			return t
		}
	}
	return ""
}

// seriesHit is a series hit with what the batched read adds to its search
// document: the primary volume count, the slug and the cover books. rec is nil
// when the batch did not return the series.
func seriesHit(
	base metadata.BookLookupHit,
	rec *metadata.SeriesLookupRecord,
) LookupHit {
	h := hitFrom(base)
	if rec == nil {
		return h
	}
	h.Title = cmp.Or(rec.Name, h.Title)
	h.Author = cmp.Or(h.Author, rec.AuthorName)
	h.Volumes = rec.PrimaryBooks
	h.Slug = cmp.Or(rec.Slug, h.Slug)
	h.CoverBookIDs = coverBooks(rec.Volumes)
	return h
}

// coverBooks is the book adding the series would pick at each of its first
// positions, up to lookupCovers of them, each cover remembered for the proxy.
func coverBooks(vols []metadata.SeriesLookupVolume) []uint32 {
	cands := make([]volumeCandidate, 0, len(vols))
	urls := make(map[uint32]string, len(vols))
	for _, v := range vols {
		cands = append(cands, volumeCandidate{
			position:    v.Position,
			bookID:      v.BookHardcoverID,
			users:       v.UsersCount,
			compilation: v.Compilation,
		})
		urls[v.BookHardcoverID] = v.ImageURL
	}
	slices.SortStableFunc(cands, func(a, b volumeCandidate) int {
		return cmp.Or(
			cmp.Compare(a.position, b.position),
			cmp.Compare(a.bookID, b.bookID),
		)
	})
	win := winnersByPosition(cands)
	win = win[:min(len(win), lookupCovers)]
	for _, id := range win {
		rememberCover(id, urls[id])
	}
	return win
}

// authorMatchedFirst moves the books whose credited author is named in the
// query and whose title covers the rest of it ahead of the others, keeping
// Hardcover's order within both groups. Hardcover ranks on text alone, so a
// compilation titled "Ted Chiang - Exhalation Stories" credited to someone else
// outranks the book by the author the user typed.
func authorMatchedFirst(
	query string,
	books []metadata.BookLookupHit,
) []metadata.BookLookupHit {
	q := queryTokens(query)
	matched := make([]metadata.BookLookupHit, 0, len(books))
	rest := make([]metadata.BookLookupHit, 0, len(books))
	for _, b := range books {
		if creditedAndTitled(q, b) {
			matched = append(matched, b)
		} else {
			rest = append(rest, b)
		}
	}
	return append(matched, rest...)
}

func creditedAndTitled(q []string, b metadata.BookLookupHit) bool {
	title := queryTokens(b.Title)
	for name := range strings.SplitSeq(b.Author, " & ") {
		a := queryTokens(name)
		if len(a) == 0 {
			continue
		}
		for i := 0; i+len(a) <= len(q); i++ {
			if !slices.Equal(q[i:i+len(a)], a) {
				continue
			}
			remainder := slices.Concat(q[:i], q[i+len(a):])
			if len(remainder) > 0 && titleCovers(title, remainder) {
				return true
			}
		}
	}
	return false
}

func titleCovers(title, tokens []string) bool {
	for _, t := range tokens {
		if !slices.Contains(title, t) {
			return false
		}
	}
	return true
}

func queryTokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func hitFrom(h metadata.BookLookupHit) LookupHit {
	out := LookupHit{
		HardcoverID: h.HardcoverID,
		Type:        h.Type,
		Title:       h.Title,
		Author:      h.Author,
		Year:        h.Year,
		Volumes:     h.Volumes,
		Ongoing:     h.Ongoing,
		Slug:        h.Slug,
	}
	if len(h.Genres) > 0 {
		out.Kind = metadata.ClassifyBookKind(h.Genres, "")
	}
	return out
}

func rememberCover(hardcoverID uint32, url string) {
	if url == "" {
		return
	}
	artwork.Remember(
		artwork.KindBooks,
		strconv.FormatUint(uint64(hardcoverID), 10),
		artwork.Source{URL: url},
	)
}

// markAdded fills the library join: whether a hit is already held, under which
// id, and for a series the poster to show.
func (s *Service) markAdded(ctx context.Context, hits []LookupHit) error {
	var bookIDs, seriesIDs []uint32
	for _, h := range hits {
		switch h.Type {
		case lookupBook:
			bookIDs = append(bookIDs, h.HardcoverID)
		case lookupSeries:
			seriesIDs = append(seriesIDs, h.HardcoverID)
		}
	}
	var (
		held   map[uint32]*ent.Book
		series map[uint32]db.HeldSeries
		err    error
	)
	if len(bookIDs) > 0 {
		if held, err = s.db.FindBooksByHardcoverIDs(ctx, bookIDs); err != nil {
			return err
		}
	}
	if len(seriesIDs) > 0 {
		if series, err = s.db.FindSeriesByHardcoverIDs(ctx, seriesIDs); err != nil {
			return err
		}
	}
	for i := range hits {
		h := &hits[i]
		switch h.Type {
		case lookupBook:
			if b, ok := held[h.HardcoverID]; ok {
				h.AlreadyAdded, h.LibraryID = true, b.ID
			}
		case lookupSeries:
			row, ok := series[h.HardcoverID]
			if !ok {
				continue
			}
			h.AlreadyAdded, h.LibraryID = true, row.Series.ID
			if row.Series.Since != nil {
				h.Year = *row.Series.Since
			}
			h.CoverID = row.FirstVolumeID
		}
	}
	return nil
}

// LookupDetail returns the full detail of a book or series. A book is one
// Hardcover request; a series is two: its skeleton, then one batch carrying
// its first volumes, whose covers the panel shows. Memoised for ten minutes,
// so an add right after a highlight, and a reviewer's repeated expand, cost
// nothing.
func (s *Service) LookupDetail(
	ctx context.Context,
	kind string,
	hardcoverID uint32,
) (*LookupDetail, error) {
	ctx, span := tracer.Start(ctx, "book.lookup_detail",
		trace.WithAttributes(attribute.String("lookup.type", kind)))
	defer span.End()

	key := kind + ":" + strconv.FormatUint(uint64(hardcoverID), 10)
	cached, ok := s.details.get(key)
	if !ok {
		p, err := s.provider()
		if err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		if kind == lookupSeries {
			cached, err = s.seriesDetail(ctx, p, hardcoverID)
		} else {
			cached, err = s.bookDetail(ctx, p, hardcoverID)
		}
		if err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		s.details.put(key, cached)
	}
	out := *cached
	hits := []LookupHit{out.LookupHit}
	if err := s.markAdded(ctx, hits); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	out.LookupHit = hits[0]
	return &out, nil
}

func (s *Service) bookDetail(
	ctx context.Context,
	p metadata.BookProvider,
	id uint32,
) (*LookupDetail, error) {
	recs, err := p.GetBooks(ctx, []uint32{id})
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, ErrHardcoverNotFound
	}
	rec := recs[0]
	rememberCover(rec.HardcoverID, rec.CoverURL)

	title, original := pick.Titles(
		recordEditionViews(
			rec.Editions,
		),
		config.Get().Library.BookLanguage,
		rec.Title,
	)
	d := &LookupDetail{
		HardcoverID:   rec.HardcoverID,
		Type:          lookupBook,
		Title:         title,
		OriginalTitle: original,
		Author:        pick.DisplayAuthor(recordCredits(rec.Credits)),
		Kind:          rec.Kind,
		Year:          rec.ReleaseYear,
		Overview:      rec.Description,
		Genres:        rec.Genres[:min(len(rec.Genres), detailGenreLimit)],
		Pages:         pagesOf(rec.Editions),
	}
	for _, e := range rec.Editions {
		d.Editions = append(d.Editions, LookupEdition{
			Language:  e.Language,
			Format:    e.Format,
			Publisher: e.Publisher,
			Year:      e.Year,
			Original:  e.Original,
		})
	}
	return d, nil
}

// pagesOf is the page count of the original-language ebook (or physical)
// edition, else the largest stated.
func pagesOf(eds []metadata.EditionRecord) uint16 {
	var most, original uint16
	for _, e := range eds {
		if e.Format != metadata.FormatEbook || e.Pages == 0 {
			continue
		}
		most = max(most, e.Pages)
		if e.Original && original == 0 {
			original = e.Pages
		}
	}
	if original != 0 {
		return original
	}
	return most
}

func (s *Service) seriesDetail(
	ctx context.Context,
	p metadata.BookProvider,
	id uint32,
) (*LookupDetail, error) {
	skeleton, err := p.GetSeries(ctx, id)
	if err != nil {
		return nil, err
	}
	if skeleton == nil {
		return nil, ErrHardcoverNotFound
	}
	volumes := skeletonWinners(orderedVolumes(skeleton.Volumes))
	d := &LookupDetail{
		HardcoverID: skeleton.HardcoverID,
		Type:        lookupSeries,
		Slug:        skeleton.Slug,
		Title:       skeleton.Name,
		Author:      skeleton.AuthorName,
		Volumes:     cmpOr(skeleton.PrimaryBooks, skeleton.Books),
		Overview:    skeleton.Description,
	}
	ongoing := !skeleton.Completed
	d.Ongoing = &ongoing
	if len(volumes) == 0 {
		return d, nil
	}

	ids := volumes[:min(len(volumes), detailVolumeIDs)]
	recs, err := p.GetBooks(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint32]*metadata.BookRecord, len(recs))
	for _, r := range recs {
		byID[r.HardcoverID] = r
		rememberCover(r.HardcoverID, r.CoverURL)
	}
	d.VolumeBookIDs = ids
	first := byID[ids[0]]
	if first == nil {
		return d, nil
	}
	d.Kind = first.Kind
	d.Genres = first.Genres[:min(len(first.Genres), detailGenreLimit)]
	if d.Author == "" {
		d.Author = pick.DisplayAuthor(recordCredits(first.Credits))
	}
	type pair struct{ lang, publisher string }
	var seen []pair
	for _, e := range first.Editions {
		k := pair{e.Language, e.Publisher}
		if e.Format != metadata.FormatEbook || slices.Contains(seen, k) {
			continue
		}
		seen = append(seen, k)
		d.Editions = append(d.Editions, LookupEdition{
			Language:  e.Language,
			Format:    metadata.FormatEbook,
			Publisher: e.Publisher,
			Year:      e.Year,
			Original:  e.Original,
		})
	}
	return d, nil
}

func cmpOr(a, b uint32) uint32 {
	if a != 0 {
		return a
	}
	return b
}
