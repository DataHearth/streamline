package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

const (
	hcLookupBooksPerPage  = 12
	hcLookupSeriesPerPage = 8
	hcScanSearchPerPage   = 20
	hcTopGenres           = 3
	hcMaxCredits          = 24
)

// hcID decodes an id that Hardcover's search documents carry as a string and
// its GraphQL rows carry as a number.
type hcID uint32

//nolint:unparam // json.Unmarshaler
func (i *hcID) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*i = 0
		return nil
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		// A document with an id we cannot read is skipped, not fatal to the
		// whole search.
		*i = 0
		return nil
	}
	*i = hcID(n)
	return nil
}

type hcSearchHits[D any] struct {
	Search struct {
		Results struct {
			Hits []struct {
				Document D `json:"document"`
			} `json:"hits"`
		} `json:"results"`
	} `json:"search"`
}

const hcSearchBooksQuery = `
query ($q: String!, $per: Int!) {
  search(query: $q, query_type: "Book", per_page: $per, page: 1) { results }
}`

const hcSearchSeriesQuery = `
query ($q: String!, $per: Int!) {
  search(query: $q, query_type: "Series", per_page: $per, page: 1) { results }
}`

type hcBookDoc struct {
	ID          hcID     `json:"id"`
	Title       string   `json:"title"`
	AuthorNames []string `json:"author_names"`
	ReleaseYear uint16   `json:"release_year"`
	Image       hcImage  `json:"image"`
	Genres      []string `json:"genres"`
}

type hcSeriesDoc struct {
	ID                hcID     `json:"id"`
	Name              string   `json:"name"`
	AuthorName        string   `json:"author_name"`
	BooksCount        uint32   `json:"books_count"`
	PrimaryBooksCount uint32   `json:"primary_books_count"`
	IsCompleted       *bool    `json:"is_completed"`
	Genres            []string `json:"genres"`
}

func (h *Hardcover) searchBookDocs(
	ctx context.Context,
	q string,
	per int,
) ([]hcBookDoc, error) {
	var payload hcSearchHits[hcBookDoc]
	if err := h.query(
		ctx, hcSearchBooksQuery, map[string]any{"q": q, "per": per}, &payload,
	); err != nil {
		return nil, err
	}
	docs := make([]hcBookDoc, 0, len(payload.Search.Results.Hits))
	for _, hit := range payload.Search.Results.Hits {
		if hit.Document.ID != 0 {
			docs = append(docs, hit.Document)
		}
	}
	return docs, nil
}

// SearchBooks is the bulk-import scanner's title search.
func (h *Hardcover) SearchBooks(
	ctx context.Context,
	query string,
) ([]BookSearchResult, error) {
	ctx, span := tracer.Start(ctx, "metadata.hardcover.search_books")
	defer span.End()

	docs, err := h.searchBookDocs(ctx, query, hcScanSearchPerPage)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	results := make([]BookSearchResult, 0, len(docs))
	for _, d := range docs {
		r := BookSearchResult{
			HardcoverID: uint32(d.ID),
			Title:       d.Title,
			Year:        d.ReleaseYear,
		}
		if len(d.AuthorNames) > 0 {
			r.Author = d.AuthorNames[0]
		}
		results = append(results, r)
	}
	return results, nil
}

func normaliseQuery(q string) string {
	return strings.Join(strings.Fields(strings.ToLower(q)), " ")
}

// LookupBooks is one request per distinct query, memoised for hcLookupTTL.
func (h *Hardcover) LookupBooks(
	ctx context.Context,
	query string,
) ([]BookLookupHit, error) {
	ctx, span := tracer.Start(ctx, "metadata.hardcover.lookup_books")
	defer span.End()

	key := "book:" + normaliseQuery(query)
	if hits, ok := h.lookups.get(key); ok {
		return hits, nil
	}
	docs, err := h.searchBookDocs(ctx, query, hcLookupBooksPerPage)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	hits := make([]BookLookupHit, 0, len(docs))
	for _, d := range docs {
		hit := BookLookupHit{
			HardcoverID: uint32(d.ID),
			Type:        "book",
			Title:       d.Title,
			Year:        d.ReleaseYear,
			Genres:      d.Genres,
			ImageURL:    string(d.Image),
		}
		if len(d.AuthorNames) > 0 {
			hit.Author = strings.Join(
				d.AuthorNames[:min(len(d.AuthorNames), 3)],
				" & ",
			)
		}
		hits = append(hits, hit)
	}
	h.lookups.put(key, hits, hcLookupTTL)
	return hits, nil
}

// LookupSeries is one request per distinct query, memoised for hcLookupTTL.
func (h *Hardcover) LookupSeries(
	ctx context.Context,
	query string,
) ([]BookLookupHit, error) {
	ctx, span := tracer.Start(ctx, "metadata.hardcover.lookup_series")
	defer span.End()

	key := "series:" + normaliseQuery(query)
	if hits, ok := h.lookups.get(key); ok {
		return hits, nil
	}
	var payload hcSearchHits[hcSeriesDoc]
	if err := h.query(
		ctx,
		hcSearchSeriesQuery,
		map[string]any{"q": query, "per": hcLookupSeriesPerPage},
		&payload,
	); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	hits := make([]BookLookupHit, 0, len(payload.Search.Results.Hits))
	for _, raw := range payload.Search.Results.Hits {
		d := raw.Document
		if d.ID == 0 {
			continue
		}
		hit := BookLookupHit{
			HardcoverID: uint32(d.ID),
			Type:        "series",
			Title:       d.Name,
			Author:      d.AuthorName,
			Volumes:     cmpOr(d.PrimaryBooksCount, d.BooksCount),
			Genres:      d.Genres,
		}
		if d.IsCompleted != nil {
			ongoing := !*d.IsCompleted
			hit.Ongoing = &ongoing
		}
		hits = append(hits, hit)
	}
	h.lookups.put(key, hits, hcLookupTTL)
	return hits, nil
}

func cmpOr(a, b uint32) uint32 {
	if a != 0 {
		return a
	}
	return b
}

const hcBookByISBNQuery = `
query ($isbn: String!) {
  editions(where: {isbn_13: {_eq: $isbn}}, limit: 1) { book_id }
}`

func (h *Hardcover) BookByISBN(ctx context.Context, isbn string) (uint32, error) {
	ctx, span := tracer.Start(ctx, "metadata.hardcover.book_by_isbn")
	defer span.End()

	var payload struct {
		Editions []struct {
			BookID uint32 `json:"book_id"`
		} `json:"editions"`
	}
	if err := h.query(
		ctx,
		hcBookByISBNQuery,
		map[string]any{"isbn": isbn},
		&payload,
	); err != nil {
		return 0, otelx.RecordSpanError(span, err)
	}
	if len(payload.Editions) == 0 {
		return 0, nil
	}
	return payload.Editions[0].BookID, nil
}

// hcBooksQuery is the workhorse: one request for up to hcBookBatch books with
// their editions. The three edition aliases are one GraphQL document, and the
// depth stays at 3 (books > editions > language|publisher), which is
// Hardcover's limit. Book-level contributors come from the cached_contributors
// jsonb, a scalar, so no contributions join is needed.
const hcBooksQuery = `
query ($ids: [Int!]!) {
  books(where: {id: {_in: $ids}}) {
    id title description release_date release_year rating users_count compilation
    cached_image cached_contributors cached_tags
    book_series { position series { id name } }
    digital: editions(
      where: {reading_format_id: {_in: [2, 4]}}
      order_by: {users_count: desc_nulls_last}, limit: 40) { ...ed }
    physical: editions(
      where: {_or: [{reading_format_id: {_eq: 1}}, {reading_format_id: {_is_null: true}}]}
      order_by: {users_count: desc_nulls_last}, limit: 20) { ...ed }
    earliest: editions(order_by: {release_date: asc_nulls_last}, limit: 1) { ...ed }
  }
}
fragment ed on editions {
  id title release_date release_year pages audio_seconds users_count
  reading_format_id isbn_13 asin cached_contributors
  language { code2 }
  publisher { name }
}`

// hcContributionsQuery is the join form of the book-level contributors, used
// only for books whose cached_contributors blob carried none.
const hcContributionsQuery = `
query ($ids: [Int!]!) {
  books(where: {id: {_in: $ids}}) {
    id
    contributions(order_by: {id: asc}, limit: 24) {
      contribution
      author { id name cached_image }
    }
  }
}`

type hcContributor struct {
	Author struct {
		ID          hcID    `json:"id"`
		Name        string  `json:"name"`
		CachedImage hcImage `json:"cachedImage"`
		SnakeImage  hcImage `json:"cached_image"`
		Image       hcImage `json:"image"`
	} `json:"author"`
	Contribution *string `json:"contribution"`
}

func (c hcContributor) image() string {
	for _, i := range []hcImage{c.Author.CachedImage, c.Author.SnakeImage, c.Author.Image} {
		if i != "" {
			return string(i)
		}
	}
	return ""
}

type hcEditionRow struct {
	ID                 uint32          `json:"id"`
	Title              string          `json:"title"`
	ReleaseDate        string          `json:"release_date"`
	ReleaseYear        *int            `json:"release_year"`
	Pages              *int            `json:"pages"`
	AudioSeconds       *int            `json:"audio_seconds"`
	UsersCount         *int            `json:"users_count"`
	ReadingFormatID    *int            `json:"reading_format_id"`
	ISBN13             *string         `json:"isbn_13"`
	ASIN               *string         `json:"asin"`
	CachedContributors json.RawMessage `json:"cached_contributors"`
	Language           *struct {
		Code2 *string `json:"code2"`
	} `json:"language"`
	Publisher *struct {
		Name *string `json:"name"`
	} `json:"publisher"`
}

func (r hcEditionRow) raw() RawEdition {
	e := RawEdition{
		HardcoverID: r.ID,
		Title:       strings.TrimSpace(r.Title),
		ISBN13:      derefStr(r.ISBN13),
		ASIN:        derefStr(r.ASIN),
		ReleaseDate: parseHCDate(r.ReleaseDate),
	}
	if r.Language != nil && r.Language.Code2 != nil {
		e.Language = strings.ToLower(strings.TrimSpace(*r.Language.Code2))
	}
	if r.Publisher != nil && r.Publisher.Name != nil {
		e.Publisher = cleanPublisher(*r.Publisher.Name)
	}
	if r.ReadingFormatID != nil {
		e.ReadingFormatID = *r.ReadingFormatID
	}
	if r.ReleaseYear != nil {
		e.Year = numeric.SaturateU16(*r.ReleaseYear)
	}
	if r.Pages != nil && *r.Pages > 0 {
		e.Pages = numeric.SaturateU16(*r.Pages)
	}
	if r.AudioSeconds != nil && *r.AudioSeconds > 0 {
		e.DurationSeconds = numeric.SaturateU32(*r.AudioSeconds)
	}
	if r.UsersCount != nil && *r.UsersCount > 0 {
		e.Popularity = numeric.SaturateU32(*r.UsersCount)
	}
	for _, c := range decodeContributors(r.CachedContributors) {
		role, ok := RoleForContribution(derefStr(c.Contribution))
		switch {
		case !ok:
		case role == RoleNarrator && e.Narrator == "":
			e.Narrator = c.Author.Name
		case role == RoleTranslator && e.Translator == "":
			e.Translator = c.Author.Name
			e.TranslatorID = uint32(c.Author.ID)
		}
	}
	return e
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// contributorsGap reports a cached_contributors blob that cannot name the
// people: absent, not an array, or an entry without an id. An empty array is
// Hardcover saying there are none, which is no gap.
func contributorsGap(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var entries []hcContributor
	if err := json.Unmarshal(raw, &entries); err != nil {
		return true
	}
	for _, c := range entries {
		if c.Author.ID == 0 {
			return true
		}
	}
	return false
}

func decodeContributors(raw json.RawMessage) []hcContributor {
	if len(raw) == 0 {
		return nil
	}
	var out []hcContributor
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

type hcTag struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// decodeGenres lists the Genre tags by descending count. A blob that is not
// the expected shape gives none, and the kind falls back to novel.
func decodeGenres(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var tags map[string][]hcTag
	if err := json.Unmarshal(raw, &tags); err != nil {
		return nil
	}
	genres := slices.Clone(tags["Genre"])
	slices.SortStableFunc(genres, func(a, b hcTag) int { return b.Count - a.Count })
	out := make([]string, 0, len(genres))
	for _, g := range genres {
		if t := strings.TrimSpace(g.Tag); t != "" {
			out = append(out, t)
		}
	}
	return out
}

type hcBookRow struct {
	ID                 uint32          `json:"id"`
	Title              string          `json:"title"`
	Description        *string         `json:"description"`
	ReleaseDate        string          `json:"release_date"`
	ReleaseYear        *int            `json:"release_year"`
	Rating             *float64        `json:"rating"`
	UsersCount         *int            `json:"users_count"`
	Compilation        *bool           `json:"compilation"`
	CachedImage        hcImage         `json:"cached_image"`
	CachedContributors json.RawMessage `json:"cached_contributors"`
	CachedTags         json.RawMessage `json:"cached_tags"`
	BookSeries         []struct {
		Position *float64 `json:"position"`
		Series   struct {
			ID   uint32 `json:"id"`
			Name string `json:"name"`
		} `json:"series"`
	} `json:"book_series"`
	Digital  []hcEditionRow `json:"digital"`
	Physical []hcEditionRow `json:"physical"`
	Earliest []hcEditionRow `json:"earliest"`
}

func (r hcBookRow) record() *BookRecord {
	rec := &BookRecord{
		HardcoverID: r.ID,
		Title:       strings.TrimSpace(r.Title),
		Description: derefStr(r.Description),
		ReleaseDate: parseHCDate(r.ReleaseDate),
		CoverURL:    string(r.CachedImage),
	}
	switch {
	case r.ReleaseYear != nil && *r.ReleaseYear > 0:
		rec.ReleaseYear = numeric.SaturateU16(*r.ReleaseYear)
	case rec.ReleaseDate != nil:
		rec.ReleaseYear = numeric.SaturateU16(rec.ReleaseDate.Year())
	}
	if r.Rating != nil && *r.Rating > 0 {
		rec.Rating = *r.Rating
	}
	if r.UsersCount != nil && *r.UsersCount > 0 {
		rec.UsersCount = numeric.SaturateU32(*r.UsersCount)
	}
	rec.Compilation = r.Compilation != nil && *r.Compilation

	genres := decodeGenres(r.CachedTags)
	if len(genres) > 0 {
		rec.Genre = genres[0]
		rec.Genres = genres[:min(len(genres), hcTopGenres)]
	}
	for _, bs := range r.BookSeries {
		if bs.Series.ID == 0 {
			continue
		}
		rec.Series = append(rec.Series, BookSeriesRef{
			SeriesHardcoverID: bs.Series.ID,
			Name:              bs.Series.Name,
			Position:          bs.Position,
		})
	}

	rec.Credits = creditsFrom(decodeContributors(r.CachedContributors))
	rec.creditsGap = contributorsGap(r.CachedContributors)

	digital := rawEditions(r.Digital)
	physical := rawEditions(r.Physical)
	var earliest *RawEdition
	if len(r.Earliest) > 0 {
		e := r.Earliest[0].raw()
		earliest = &e
	}
	rec.Editions, rec.OriginalLanguage = SelectEditions(
		digital, physical, earliest, rec.ReleaseYear,
	)
	if earliest != nil && earliest.Title != "" && earliest.Title != rec.Title {
		rec.OriginalTitle = earliest.Title
	}
	rec.Kind = ClassifyBookKind(genres, rec.OriginalLanguage)
	return rec
}

func rawEditions(rows []hcEditionRow) []RawEdition {
	out := make([]RawEdition, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.raw())
	}
	return out
}

// creditsFrom keeps the makers, once per (author, role), in Hardcover's order.
func creditsFrom(in []hcContributor) []BookCredit {
	var out []BookCredit
	for _, c := range in {
		role, ok := RoleForContribution(derefStr(c.Contribution))
		if !ok || !IsMakerRole(role) || c.Author.ID == 0 || c.Author.Name == "" {
			continue
		}
		credit := BookCredit{
			AuthorHardcoverID: uint32(c.Author.ID),
			Name:              strings.TrimSpace(c.Author.Name),
			ImageURL:          c.image(),
			Role:              role,
		}
		if slices.ContainsFunc(out, func(x BookCredit) bool {
			return x.AuthorHardcoverID == credit.AuthorHardcoverID && x.Role == role
		}) {
			continue
		}
		out = append(out, credit)
		if len(out) == hcMaxCredits {
			break
		}
	}
	return out
}

// isTimeout reports a Hardcover cut-off or a client timeout, the failures a
// smaller batch can get past.
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out")
}

// fetchBooks runs the batch query for ids, halving the batch and retrying
// when Hardcover times out. Each retry is a request of the day's budget.
func (h *Hardcover) fetchBooks(
	ctx context.Context,
	ids []uint32,
) ([]*BookRecord, error) {
	var payload struct {
		Books []hcBookRow `json:"books"`
	}
	err := h.query(ctx, hcBooksQuery, map[string]any{"ids": ids}, &payload)
	if err != nil {
		if len(ids) > 1 && ctx.Err() == nil && isTimeout(err) {
			mid := len(ids) / 2
			left, lerr := h.fetchBooks(ctx, ids[:mid])
			if lerr != nil {
				return nil, lerr
			}
			right, rerr := h.fetchBooks(ctx, ids[mid:])
			if rerr != nil {
				return nil, rerr
			}
			return append(left, right...), nil
		}
		return nil, err
	}
	records := make([]*BookRecord, 0, len(payload.Books))
	var noCredits []uint32
	for _, row := range payload.Books {
		rec := row.record()
		records = append(records, rec)
		if len(rec.Credits) == 0 && rec.creditsGap {
			noCredits = append(noCredits, rec.HardcoverID)
		}
	}
	if len(noCredits) > 0 {
		if err := h.fillCredits(ctx, records, noCredits); err != nil {
			if ctx.Err() != nil || errors.Is(err, ErrRateLimited) {
				return nil, err
			}
			slog.WarnContext(
				ctx,
				"hardcover contributors not read; keeping the books without credits",
				"books",
				len(noCredits),
				"error",
				err,
			)
		}
	}
	return records, nil
}

// fillCredits reads the contributors of books whose blob carried none through
// the join form. It is one more request, only for those books.
func (h *Hardcover) fillCredits(
	ctx context.Context,
	records []*BookRecord,
	ids []uint32,
) error {
	var payload struct {
		Books []struct {
			ID            uint32          `json:"id"`
			Contributions []hcContributor `json:"contributions"`
		} `json:"books"`
	}
	if err := h.query(
		ctx, hcContributionsQuery, map[string]any{"ids": ids}, &payload,
	); err != nil {
		return err
	}
	byID := make(map[uint32][]BookCredit, len(payload.Books))
	for _, b := range payload.Books {
		byID[b.ID] = creditsFrom(b.Contributions)
	}
	for _, r := range records {
		if c, ok := byID[r.HardcoverID]; ok {
			r.Credits = c
		}
	}
	return nil
}

func (h *Hardcover) GetBooks(
	ctx context.Context,
	ids []uint32,
) ([]*BookRecord, error) {
	return h.getBooks(ctx, ids, true)
}

func (h *Hardcover) GetBooksFresh(
	ctx context.Context,
	ids []uint32,
) ([]*BookRecord, error) {
	return h.getBooks(ctx, ids, false)
}

func (h *Hardcover) getBooks(
	ctx context.Context,
	ids []uint32,
	useMemo bool,
) ([]*BookRecord, error) {
	ctx, span := tracer.Start(ctx, "metadata.hardcover.get_books")
	defer span.End()

	found := make(map[uint32]*BookRecord, len(ids))
	var missing []uint32
	asked := make(map[uint32]bool, len(ids))
	order := make([]uint32, 0, len(ids))
	for _, id := range ids {
		if asked[id] {
			continue
		}
		asked[id] = true
		order = append(order, id)
		if useMemo {
			if rec, ok := h.books.get(strconv.FormatUint(uint64(id), 10)); ok {
				found[id] = rec
				continue
			}
		}
		missing = append(missing, id)
	}
	for chunk := range slices.Chunk(missing, hcBookBatch) {
		recs, err := h.fetchBooks(ctx, chunk)
		if err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		for _, rec := range recs {
			found[rec.HardcoverID] = rec
			h.books.put(
				strconv.FormatUint(uint64(rec.HardcoverID), 10), rec, hcDetailTTL,
			)
		}
	}
	out := make([]*BookRecord, 0, len(order))
	for _, id := range order {
		if rec, ok := found[id]; ok {
			out = append(out, rec)
		}
	}
	return out, nil
}

// hcSeriesQuery is the skeleton: the row and the numbered entries. Volumes'
// editions would be four levels deep, which Hardcover refuses, so they come
// from the batch book query afterwards.
const hcSeriesQuery = `
query ($id: Int!) {
  series(where: {id: {_eq: $id}}) {
    id name description is_completed primary_books_count books_count
    author { id name cached_image }
    book_series(where: {position: {_is_null: false}}, order_by: {position: asc}, limit: 500) {
      position book_id
    }
  }
}`

func (h *Hardcover) GetSeries(
	ctx context.Context,
	id uint32,
) (*SeriesRecord, error) {
	ctx, span := tracer.Start(ctx, "metadata.hardcover.get_series")
	defer span.End()

	var payload struct {
		Series []struct {
			ID                uint32  `json:"id"`
			Name              string  `json:"name"`
			Description       *string `json:"description"`
			IsCompleted       *bool   `json:"is_completed"`
			PrimaryBooksCount *uint32 `json:"primary_books_count"`
			BooksCount        *uint32 `json:"books_count"`
			Author            *struct {
				ID          uint32  `json:"id"`
				Name        string  `json:"name"`
				CachedImage hcImage `json:"cached_image"`
			} `json:"author"`
			BookSeries []struct {
				Position *float64 `json:"position"`
				BookID   uint32   `json:"book_id"`
			} `json:"book_series"`
		} `json:"series"`
	}
	if err := h.query(
		ctx, hcSeriesQuery, map[string]any{"id": id}, &payload,
	); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if len(payload.Series) == 0 {
		return nil, nil
	}
	row := payload.Series[0]
	rec := &SeriesRecord{
		HardcoverID: row.ID,
		Name:        strings.TrimSpace(row.Name),
		Description: derefStr(row.Description),
		Completed:   row.IsCompleted != nil && *row.IsCompleted,
	}
	if row.PrimaryBooksCount != nil {
		rec.PrimaryBooks = *row.PrimaryBooksCount
	}
	if row.BooksCount != nil {
		rec.Books = *row.BooksCount
	}
	if row.Author != nil {
		rec.AuthorHardcoverID = row.Author.ID
		rec.AuthorName = strings.TrimSpace(row.Author.Name)
		rec.AuthorImageURL = string(row.Author.CachedImage)
	}
	seenBook := map[uint32]bool{}
	for _, v := range row.BookSeries {
		if v.Position == nil || v.BookID == 0 || seenBook[v.BookID] {
			continue
		}
		seenBook[v.BookID] = true
		rec.Volumes = append(rec.Volumes, SeriesVolumeRef{
			Position: *v.Position, BookHardcoverID: v.BookID,
		})
	}
	return rec, nil
}
