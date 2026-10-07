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
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
)

const (
	hcEndpoint         = "https://api.hardcover.app/v1/graphql"
	hcBibliographyPage = 100

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

	resp, err := h.client.Do(req)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	defer resp.Body.Close()

	recordProviderStatus(ctx, resp.StatusCode)
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		h.authRejected.Store(true)
		slog.WarnContext(ctx, "hardcover token rejected",
			"http.status_code", resp.StatusCode)
		return otelx.RecordSpanError(span, ErrHardcoverUnauthorized)
	case resp.StatusCode != http.StatusOK:
		slog.WarnContext(ctx, "hardcover request non-200",
			"http.status_code", resp.StatusCode)
		return otelx.RecordSpanError(span,
			fmt.Errorf("hardcover: status %d", resp.StatusCode))
	}
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
