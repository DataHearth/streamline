package book

import (
	"context"
	"slices"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/artwork"
	"github.com/datahearth/streamline/internal/config"
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

// Lookup searches Hardcover for books and series to add. A search for both is
// two requests, one for one kind; either is memoised per query.
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
	var books, series []metadata.BookLookupHit
	if kind != lookupSeries {
		if books, err = p.LookupBooks(ctx, query); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}
	if kind != lookupBook {
		if series, err = p.LookupSeries(ctx, query); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}

	hits := orderHits(query, books, series)
	if len(hits) > lookupCap {
		hits = hits[:lookupCap]
	}
	out := make([]LookupHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, hitFrom(h))
		if h.Type == lookupBook {
			rememberCover(h.HardcoverID, h.ImageURL)
		}
	}
	if err := s.markAdded(ctx, out); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	return out, nil
}

// orderHits puts the series named like the query first, then the books in
// Hardcover's relevance order, then the other series.
func orderHits(
	query string,
	books, series []metadata.BookLookupHit,
) []metadata.BookLookupHit {
	var named, rest []metadata.BookLookupHit
	for _, sr := range series {
		if library.TitleMatches(sr.Title, query) ||
			library.TitlePrefixMatches(sr.Title, query) {
			named = append(named, sr)
		} else {
			rest = append(rest, sr)
		}
	}
	out := make([]metadata.BookLookupHit, 0, len(books)+len(series))
	out = append(out, named...)
	out = append(out, books...)
	return append(out, rest...)
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
	var bookIDs []uint32
	for _, h := range hits {
		if h.Type == lookupBook {
			bookIDs = append(bookIDs, h.HardcoverID)
		}
	}
	var held map[uint32]*ent.Book
	if len(bookIDs) > 0 {
		var err error
		if held, err = s.db.FindBooksByHardcoverIDs(ctx, bookIDs); err != nil {
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
			row, err := s.db.FindSeriesByHardcoverID(ctx, h.HardcoverID)
			if err != nil {
				return err
			}
			if row == nil {
				continue
			}
			h.AlreadyAdded, h.LibraryID = true, row.ID
			if row.Since != nil {
				h.Year = *row.Since
			}
			if h.CoverID, err = s.db.FirstVolumeID(ctx, row.ID); err != nil {
				return err
			}
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
	volumes := orderedVolumes(skeleton.Volumes)
	d := &LookupDetail{
		HardcoverID: skeleton.HardcoverID,
		Type:        lookupSeries,
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

	ids := make([]uint32, 0, detailVolumeIDs)
	for _, v := range volumes[:min(len(volumes), detailVolumeIDs)] {
		ids = append(ids, v.BookHardcoverID)
	}
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
