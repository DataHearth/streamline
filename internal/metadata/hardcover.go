package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
)

const (
	hcEndpoint         = "https://api.hardcover.app/v1/graphql"
	hcBibliographyPage = 100

	// Hardcover's free plan allows 5,000 requests a day. The reserve is what a
	// bulk scan leaves behind so adds, refreshes and request approvals keep
	// working after it has eaten the rest of the day. The limits are
	// Hardcover's, not the operator's, so there is no config key.
	hardcoverDailyBudget = 5000
	hardcoverScanReserve = 500

	hcDefaultRetryAfter = time.Minute

	hcFormatAudiobook = 2
	hcFormatEbook     = 4
)

var (
	ErrHardcoverKeyMissing = errors.New(
		"metadata: hardcover api key not configured",
	)
	ErrHardcoverUnauthorized = errors.New(
		"metadata: hardcover token rejected (expired?)",
	)
)

// Hardcover is the BookProvider implementation. GraphQL API, Bearer token,
// 60 req/min budget, max query depth 3 (bibliographies are fetched in two
// queries). Tokens expire every January 1st.
type Hardcover struct {
	client       *http.Client
	token        string
	limiter      *rate.Limiter
	authRejected atomic.Bool

	budgetMu  sync.Mutex
	budgetDay time.Time
	used      int
}

func NewHardcover() (*Hardcover, error) {
	m := config.Get().Metadata
	token := strings.TrimSpace(
		config.SecretValue(m.HardcoverAPIKey, m.HardcoverAPIKeyFile),
	)
	if token == "" {
		return nil, ErrHardcoverKeyMissing
	}
	c := *otelx.HTTPClient
	return &Hardcover{
		client:  &c,
		token:   token,
		limiter: rate.NewLimiter(rate.Every(time.Second), 1),
	}, nil
}

// AuthRejected reports whether Hardcover's latest answer was a 401.
func (h *Hardcover) AuthRejected() bool {
	return h.authRejected.Load()
}

var _ Budgeter = (*Hardcover)(nil)

func (h *Hardcover) ScanReserve() int {
	return hardcoverScanReserve
}

// Remaining is the number of requests left in the current UTC day.
func (h *Hardcover) Remaining() int {
	h.budgetMu.Lock()
	defer h.budgetMu.Unlock()
	h.rollDay(time.Now().UTC())
	return hardcoverDailyBudget - h.used
}

func (h *Hardcover) rollDay(now time.Time) {
	day := now.Truncate(24 * time.Hour)
	if !day.Equal(h.budgetDay) {
		h.budgetDay = day
		h.used = 0
	}
}

// take spends one request of today's budget. When none is left it returns how
// long until the budget resets at UTC midnight.
func (h *Hardcover) take() (time.Duration, bool) {
	h.budgetMu.Lock()
	defer h.budgetMu.Unlock()
	now := time.Now().UTC()
	h.rollDay(now)
	if h.used >= hardcoverDailyBudget {
		return h.budgetDay.Add(24 * time.Hour).Sub(now), false
	}
	h.used++
	return 0, true
}

func (h *Hardcover) refund() {
	h.budgetMu.Lock()
	defer h.budgetMu.Unlock()
	if h.used > 0 {
		h.used--
	}
}

func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0)
	}
	return hcDefaultRetryAfter
}

func (h *Hardcover) query(
	ctx context.Context,
	gql string,
	vars map[string]any,
	out any,
) error {
	ctx, span := tracer.Start(ctx, "metadata.hardcover.query")
	defer span.End()

	if err := h.limiter.Wait(ctx); err != nil {
		return otelx.RecordSpanError(span, err)
	}
	body, err := json.Marshal(map[string]any{"query": gql, "variables": vars})
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, hcEndpoint, bytes.NewReader(body),
	)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.token)

	if wait, ok := h.take(); !ok {
		hardcoverRequests.Add(ctx, 1, outcomeAttr("rate_limited"))
		slog.WarnContext(ctx, "hardcover daily budget exhausted",
			"retry_after", wait.Round(time.Second))
		return otelx.RecordSpanError(span, &RateLimitedError{RetryAfter: wait})
	}
	resp, err := h.client.Do(req)
	if err != nil {
		// Nothing reached Hardcover, so the day is not charged for it. A
		// timed-out request may have arrived; refunding it is the kinder error.
		h.refund()
		hardcoverRequests.Add(ctx, 1, outcomeAttr("error"))
		return otelx.RecordSpanError(span, err)
	}
	defer resp.Body.Close()

	recordProviderStatus(ctx, resp.StatusCode)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		hardcoverRequests.Add(ctx, 1, outcomeAttr("rate_limited"))
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		slog.WarnContext(ctx, "hardcover rate limited",
			"retry_after", retryAfter.Round(time.Second))
		return otelx.RecordSpanError(span, &RateLimitedError{RetryAfter: retryAfter})
	case resp.StatusCode == http.StatusUnauthorized:
		hardcoverRequests.Add(ctx, 1, outcomeAttr("auth"))
		h.authRejected.Store(true)
		slog.WarnContext(ctx, "hardcover token rejected",
			"http.status_code", resp.StatusCode)
		return otelx.RecordSpanError(span, ErrHardcoverUnauthorized)
	case resp.StatusCode != http.StatusOK:
		hardcoverRequests.Add(ctx, 1, outcomeAttr("error"))
		slog.WarnContext(ctx, "hardcover request non-200",
			"http.status_code", resp.StatusCode)
		return otelx.RecordSpanError(span,
			fmt.Errorf("hardcover: status %d", resp.StatusCode))
	}
	hardcoverRequests.Add(ctx, 1, outcomeAttr("ok"))
	h.authRejected.Store(false)

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := otelx.DecodeJSON(
		resp.Body,
		maxProviderResponse,
		&envelope,
	); err != nil {
		return otelx.RecordSpanError(span, err)
	}
	if len(envelope.Errors) > 0 {
		return otelx.RecordSpanError(span,
			fmt.Errorf("hardcover: %s", envelope.Errors[0].Message))
	}
	return otelx.RecordSpanError(span, json.Unmarshal(envelope.Data, out))
}

// hcImage decodes an image field that is either a bare URL string or an
// object carrying one, depending on the row.
type hcImage string

func (i *hcImage) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*i = hcImage(s)
		return nil
	}
	var o struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	*i = hcImage(o.URL)
	return nil
}

func parseHCDate(s string) *time.Time {
	for _, layout := range []string{"2006-01-02", "2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

const hcSearchAuthorsQuery = `
query ($q: String!) {
  search(query: $q, query_type: "Author", per_page: 20, page: 1) { results }
}`

func (h *Hardcover) SearchAuthors(
	ctx context.Context,
	query string,
) ([]AuthorResult, error) {
	ctx, span := tracer.Start(ctx, "metadata.hardcover.search_authors")
	defer span.End()

	var payload struct {
		Search struct {
			Results struct {
				Hits []struct {
					Document struct {
						ID         string  `json:"id"`
						Name       string  `json:"name"`
						BooksCount uint32  `json:"books_count"`
						Image      hcImage `json:"image"`
					} `json:"document"`
				} `json:"hits"`
			} `json:"results"`
		} `json:"search"`
	}
	if err := h.query(
		ctx,
		hcSearchAuthorsQuery,
		map[string]any{"q": query},
		&payload,
	); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	results := make([]AuthorResult, 0, len(payload.Search.Results.Hits))
	for _, hit := range payload.Search.Results.Hits {
		id, err := strconv.ParseUint(hit.Document.ID, 10, 32)
		if err != nil {
			continue
		}
		results = append(results, AuthorResult{
			HardcoverID: uint32(id),
			Name:        hit.Document.Name,
			BooksCount:  hit.Document.BooksCount,
			ImageURL:    string(hit.Document.Image),
		})
	}
	return results, nil
}

const hcSearchBooksQuery = `
query ($q: String!) {
  search(query: $q, query_type: "Book", per_page: 20, page: 1) { results }
}`

func (h *Hardcover) SearchBooks(
	ctx context.Context,
	query string,
) ([]BookSearchResult, error) {
	ctx, span := tracer.Start(ctx, "metadata.hardcover.search_books")
	defer span.End()

	var payload struct {
		Search struct {
			Results struct {
				Hits []struct {
					Document struct {
						ID          string   `json:"id"`
						Title       string   `json:"title"`
						AuthorNames []string `json:"author_names"`
						ReleaseYear uint16   `json:"release_year"`
					} `json:"document"`
				} `json:"hits"`
			} `json:"results"`
		} `json:"search"`
	}
	if err := h.query(
		ctx,
		hcSearchBooksQuery,
		map[string]any{"q": query},
		&payload,
	); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	results := make([]BookSearchResult, 0, len(payload.Search.Results.Hits))
	for _, hit := range payload.Search.Results.Hits {
		id, err := strconv.ParseUint(hit.Document.ID, 10, 32)
		if err != nil {
			continue
		}
		r := BookSearchResult{
			HardcoverID: uint32(id),
			Title:       hit.Document.Title,
			Year:        hit.Document.ReleaseYear,
		}
		if len(hit.Document.AuthorNames) > 0 {
			r.Author = hit.Document.AuthorNames[0]
		}
		results = append(results, r)
	}
	return results, nil
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

const hcAuthorQuery = `
query ($id: Int!) {
  authors(where: {id: {_eq: $id}}) {
    id name bio cached_image books_count
  }
}`

// contribution is null for primary authorship; translators, narrators and
// illustrators carry their role there.
const hcBibliographyQuery = `
query ($id: Int!, $limit: Int!, $offset: Int!) {
  books(
    where: {contributions: {author_id: {_eq: $id}, contribution: {_is_null: true}}}
    order_by: {release_date: asc_nulls_last}
    limit: $limit
    offset: $offset
  ) {
    id title release_date cached_image
    book_series { position series { name } }
  }
}`

const hcBookQuery = `
query ($id: Int!) {
  books(where: {id: {_eq: $id}}) {
    id title description release_date cached_image
    contributions { author_id }
    editions { isbn_13 asin reading_format_id pages audio_seconds }
  }
}`

type hcBookRow struct {
	ID          uint32  `json:"id"`
	Title       string  `json:"title"`
	ReleaseDate string  `json:"release_date"`
	CachedImage hcImage `json:"cached_image"`
	BookSeries  []struct {
		Position *float64 `json:"position"`
		Series   struct {
			Name string `json:"name"`
		} `json:"series"`
	} `json:"book_series"`
}

func (r hcBookRow) toInfo() BookInfo {
	info := BookInfo{
		HardcoverID: r.ID,
		Title:       r.Title,
		ReleaseDate: parseHCDate(r.ReleaseDate),
		CoverURL:    string(r.CachedImage),
	}
	if len(r.BookSeries) > 0 {
		first := r.BookSeries[0]
		info.SeriesName = first.Series.Name
		if first.Position != nil {
			info.SeriesPosition = strconv.FormatFloat(*first.Position, 'f', -1, 64)
		}
	}
	return info
}

func (h *Hardcover) GetAuthor(
	ctx context.Context,
	hardcoverID uint32,
) (*AuthorDetails, error) {
	ctx, span := tracer.Start(ctx, "metadata.hardcover.get_author")
	defer span.End()

	var authorPayload struct {
		Authors []struct {
			ID          uint32  `json:"id"`
			Name        string  `json:"name"`
			Bio         string  `json:"bio"`
			BooksCount  uint32  `json:"books_count"`
			CachedImage hcImage `json:"cached_image"`
		} `json:"authors"`
	}
	if err := h.query(
		ctx,
		hcAuthorQuery,
		map[string]any{"id": hardcoverID},
		&authorPayload,
	); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if len(authorPayload.Authors) == 0 {
		return nil, otelx.RecordSpanError(span,
			fmt.Errorf("hardcover: author %d not found", hardcoverID))
	}
	row := authorPayload.Authors[0]
	details := &AuthorDetails{
		HardcoverID: row.ID,
		Name:        row.Name,
		BooksCount:  row.BooksCount,
		ImageURL:    string(row.CachedImage),
		Overview:    row.Bio,
	}

	for offset := 0; ; offset += hcBibliographyPage {
		var page struct {
			Books []hcBookRow `json:"books"`
		}
		vars := map[string]any{
			"id":     hardcoverID,
			"limit":  hcBibliographyPage,
			"offset": offset,
		}
		if err := h.query(ctx, hcBibliographyQuery, vars, &page); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		for _, b := range page.Books {
			details.Books = append(details.Books, b.toInfo())
		}
		if len(page.Books) < hcBibliographyPage {
			return details, nil
		}
	}
}

func hcFormatName(id int) string {
	switch id {
	case hcFormatAudiobook:
		return "audiobook"
	case hcFormatEbook:
		return "ebook"
	default:
		return "physical"
	}
}

func (h *Hardcover) GetBook(
	ctx context.Context,
	hardcoverID uint32,
) (*BookDetails, error) {
	ctx, span := tracer.Start(ctx, "metadata.hardcover.get_book")
	defer span.End()

	var payload struct {
		Books []struct {
			hcBookRow
			Description   string `json:"description"`
			Contributions []struct {
				AuthorID uint32 `json:"author_id"`
			} `json:"contributions"`
			Editions []struct {
				ISBN13          string `json:"isbn_13"`
				ASIN            string `json:"asin"`
				ReadingFormatID int    `json:"reading_format_id"`
				Pages           uint16 `json:"pages"`
				AudioSeconds    uint32 `json:"audio_seconds"`
			} `json:"editions"`
		} `json:"books"`
	}
	if err := h.query(
		ctx,
		hcBookQuery,
		map[string]any{"id": hardcoverID},
		&payload,
	); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if len(payload.Books) == 0 {
		return nil, otelx.RecordSpanError(span,
			fmt.Errorf("hardcover: book %d not found", hardcoverID))
	}
	row := payload.Books[0]
	details := &BookDetails{
		BookInfo: row.toInfo(),
		Overview: row.Description,
	}
	if len(row.Contributions) > 0 {
		details.AuthorHardcover = row.Contributions[0].AuthorID
	}
	for _, e := range row.Editions {
		details.Editions = append(details.Editions, BookEdition{
			ISBN13:       e.ISBN13,
			ASIN:         e.ASIN,
			Format:       hcFormatName(e.ReadingFormatID),
			Pages:        e.Pages,
			AudioSeconds: e.AudioSeconds,
		})
	}
	return details, nil
}
