package indexer

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/datahearth/streamline/internal/otelx"
)

// Categorised torznab failures. Handlers map these to 422 with friendly
// messages; anything not matching is treated as a 500 internal error.
var (
	ErrUnreachable      = errors.New("indexer unreachable")
	ErrUnauthorized     = errors.New("indexer credentials rejected")
	ErrUnexpectedStatus = errors.New("indexer returned unexpected status")
	ErrBadResponse      = errors.New("indexer returned malformed response")
)

type Torznab struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewTorznab(baseURL, apiKey string) *Torznab {
	return &Torznab{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  otelx.HTTPClient,
	}
}

func (t *Torznab) Search(
	ctx context.Context,
	params SearchParams,
) ([]SearchResult, error) {
	q := url.Values{
		"apikey": {t.apiKey},
		"t":      {"search"},
		"q":      {params.Query},
	}
	if params.IMDBID != "" {
		q.Set("t", "movie")
		q.Set("imdbid", params.IMDBID)
	}
	if params.TMDBID > 0 {
		q.Set("t", "movie")
		q.Set("tmdbid", strconv.FormatUint(uint64(params.TMDBID), 10))
	}
	if params.TVDBID > 0 || params.Season > 0 {
		q.Set("t", "tvsearch")
	}
	if params.TVDBID > 0 {
		q.Set("tvdbid", strconv.FormatUint(uint64(params.TVDBID), 10))
	}
	if params.Season > 0 {
		q.Set("season", strconv.FormatUint(uint64(params.Season), 10))
	}
	if params.Episode > 0 {
		q.Set("ep", strconv.FormatUint(uint64(params.Episode), 10))
	}

	var rss torznabRSS
	if err := t.get(ctx, q, &rss); err != nil {
		return nil, fmt.Errorf("torznab search: %w", err)
	}

	results := parseItems(rss.Channel.Items)
	slog.DebugContext(
		ctx,
		"torznab search",
		"query",
		params.Query,
		"results",
		len(results),
	)
	return results, nil
}

// Feed performs a forward-feed query (`t=search` with no `q`), returning the
// indexer's recent items. Used by the rss-sync feed scanner.
func (t *Torznab) Feed(ctx context.Context) ([]SearchResult, error) {
	q := url.Values{
		"apikey": {t.apiKey},
		"t":      {"search"},
	}
	var rss torznabRSS
	if err := t.get(ctx, q, &rss); err != nil {
		return nil, fmt.Errorf("torznab feed: %w", err)
	}
	return parseItems(rss.Channel.Items), nil
}

func parseItems(items []torznabItem) []SearchResult {
	results := make([]SearchResult, 0, len(items))
	for _, item := range items {
		r := SearchResult{
			Title:       item.Title,
			InfoURL:     item.GUID,
			Download:    item.Enclosure.URL,
			Size:        item.Enclosure.Length,
			PublishDate: parsePubDate(item.PubDate),
		}
		for _, attr := range item.Attrs {
			switch attr.Name {
			case "seeders":
				if v, err := strconv.ParseUint(attr.Value, 10, 32); err == nil {
					r.Seeders = uint32(v)
				}
			case "peers":
				if v, err := strconv.ParseUint(attr.Value, 10, 32); err == nil {
					r.Leechers = uint32(v)
				}
			case "category":
				r.Category = attr.Value
			}
		}
		results = append(results, r)
	}
	return results
}

func (t *Torznab) TestConnection(ctx context.Context) error {
	q := url.Values{
		"apikey": {t.apiKey},
		"t":      {"caps"},
	}
	var caps torznabCaps
	return t.get(ctx, q, &caps)
}

func (t *Torznab) get(ctx context.Context, params url.Values, out any) error {
	u := t.baseURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: status %d", ErrUnauthorized, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("%w: status %d", ErrUnexpectedStatus, resp.StatusCode)
	}

	if err := xml.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
	return nil
}

// XML response types
type torznabRSS struct {
	XMLName xml.Name       `xml:"rss"`
	Channel torznabChannel `xml:"channel"`
}

// torznabCaps matches the `<caps>` document returned by `t=caps`. We only
// need the root element to validate the response shape; field contents are
// ignored by TestConnection.
type torznabCaps struct {
	XMLName xml.Name `xml:"caps"`
}

type torznabChannel struct {
	Items []torznabItem `xml:"item"`
}

type torznabItem struct {
	Title     string             `xml:"title"`
	GUID      string             `xml:"guid"`
	Link      string             `xml:"link"`
	PubDate   string             `xml:"pubDate"`
	Enclosure torznabEnclosure   `xml:"enclosure"`
	Attrs     []torznabAttribute `xml:"http://torznab.com/schemas/2015/feed attr"`
}

// parsePubDate decodes an RSS <pubDate>. Torznab feeds emit RFC1123Z, but some
// indexers drop the zone offset or use the bare RFC1123 form, so both are tried.
// An unparseable or absent value yields the zero time, which downstream callers
// treat as "no release date".
func parsePubDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

type torznabEnclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type torznabAttribute struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}
