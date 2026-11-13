package indexer

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/datahearth/streamline/internal/otelx"
)

// Categorised torznab failures. Handlers map these to 422 with friendly
// messages; anything not matching is treated as a 500 internal error.
var (
	ErrUnreachable      = errors.New("indexer unreachable")
	ErrUnauthorized     = errors.New("indexer credentials rejected")
	ErrUnexpectedStatus = errors.New("indexer returned unexpected status")
	ErrBadResponse      = errors.New("indexer returned malformed response")

	// ErrBadRequest means the indexer refused the query itself — an HTTP 400,
	// or a Torznab error document saying a parameter or function is not
	// supported — rather than failing to answer it. It always travels with
	// ErrUnexpectedStatus, so the callers that only read that one keep
	// working. searchAll retries such a query on a wider form: Jackett answers
	// an id its tracker cannot search by this way, where a title-only query
	// would have been answered.
	ErrBadRequest = errors.New("indexer rejected the query")

	// ErrFeedUnsupported means the protocol has no forward-feed endpoint at
	// all, which is a different thing from a feed that came back empty. The
	// RSS scanners skip such an indexer instead of scanning nothing and
	// reporting it as a result.
	ErrFeedUnsupported = errors.New("indexer does not support a feed")
)

const (
	// maxTorznabResponse bounds a single indexer response. Torznab has no
	// documented ceiling and the endpoint is a third party, so without this
	// the decoder reads for as long as the far side keeps writing. 8 MiB is
	// far past any real result page — Prowlarr's 100-item default is tens of
	// KB — so hitting it means something is wrong, not that a page was large.
	maxTorznabResponse = 8 << 20

	// maxTorznabError bounds what is read of a non-200 body looking for a
	// Torznab error document, which is a single empty element.
	maxTorznabError = 64 << 10

	// maxTorznabErrorDescription bounds the indexer's own wording carried
	// into the error, and so into the log and the span.
	maxTorznabErrorDescription = 200

	// torznabLimit is the page size asked for. Torznab defines no default:
	// Prowlarr sends 100 of its own accord, a bare Torznab endpoint sends
	// whatever it feels like, and "whatever it feels like" has been the whole
	// catalogue. Asking explicitly makes the two behave the same.
	torznabLimit = "100"

	// maxMergedResults bounds what a fan-out across every indexer × title
	// hands back. Applied after the seeder sort, so it keeps the best of the
	// merged set; nothing below 200 seeders' worth of ranking is a release
	// anyone scrolls to.
	maxMergedResults = 200
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

// torznabCategories is what a kind sends as cat=. Movie and TV send none,
// as before; only the kinds added later are scoped here.
func torznabCategories(kind MediaKind) string {
	switch kind {
	case KindMusic:
		return "3000"
	case KindEbook:
		return "7000,7020"
	case KindAudiobook:
		return "3030"
	default:
		return ""
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
		"limit":  {torznabLimit},
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
	// Only when no TMDB/TVDB id is sent: Jackett refuses a query carrying an
	// id its tracker cannot search by (an HTTP 400 with error 203, "Function
	// Not Available"), so pairing the two would lose every tracker that knows
	// one but not the other. searchAll asks again with this id alone when the
	// first query comes back empty or refused.
	if params.IMDbID != "" && params.TMDBID == 0 && params.TVDBID == 0 {
		switch params.Kind {
		case KindMovie:
			q.Set("t", "movie")
		case KindTV:
			q.Set("t", "tvsearch")
		}
		q.Set("imdbid", params.IMDbID)
	}
	if cat := torznabCategories(params.Kind); cat != "" {
		q.Set("cat", cat)
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
		"limit":  {torznabLimit},
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
			// XML decoding unescapes once; a tracker that escaped its title
			// itself still leaves "&amp;" behind.
			Title:       html.UnescapeString(item.Title),
			InfoURL:     item.GUID,
			Download:    item.Enclosure.URL,
			Size:        item.Enclosure.Length,
			PublishDate: parsePubDate(item.PubDate),
		}
		var hash, magnet string
		for _, attr := range item.Attrs {
			switch attr.Name {
			case "infohash":
				hash = attr.Value
			case "magneturl":
				magnet = attr.Value
			case "seeders":
				if v, err := strconv.ParseUint(attr.Value, 10, 32); err == nil {
					r.Seeders, r.HasSeeders = uint32(v), true
				}
			case "peers":
				if v, err := strconv.ParseUint(attr.Value, 10, 32); err == nil {
					r.Leechers = uint32(v)
				}
			case "category":
				// An item lists its root category and its subcategory in either
				// order; the subcategory is the one that tells an audiobook (3030)
				// from music under the same 3000 root.
				if r.Category == "" || strings.HasSuffix(r.Category, "000") {
					r.Category = attr.Value
				}
			// The tracker's own provider ids, when it publishes them. Both
			// spellings are accepted because both are in the wild; Prowlarr
			// reads the same pair (NewznabRssParser.GetIntAttribute). An
			// absent attr leaves the field zero, which filterProviderIDs
			// reads as "the tracker said nothing".
			case "tvdbid", "tvdb":
				if v, err := strconv.ParseUint(attr.Value, 10, 32); err == nil {
					r.TVDBID = uint32(v)
				}
			case "tmdbid", "tmdb":
				if v, err := strconv.ParseUint(attr.Value, 10, 32); err == nil {
					r.TMDBID = uint32(v)
				}
			case "imdbid", "imdb":
				r.IMDbID = imdbNumber(attr.Value)
			}
		}
		r.InfoHash = infoHash(hash, magnet, r.Download)
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
		return otelx.RedactTransportError(err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, otelx.RedactTransportError(err))
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: status %d", ErrUnauthorized, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		var doc torznabError
		if err := xml.NewDecoder(
			io.LimitReader(resp.Body, maxTorznabError),
		).Decode(&doc); err == nil && doc.Code != 0 {
			return doc.err(resp.StatusCode)
		}
		if resp.StatusCode == http.StatusBadRequest {
			return fmt.Errorf("%w: status %d: %w",
				ErrUnexpectedStatus, resp.StatusCode, ErrBadRequest)
		}
		return fmt.Errorf("%w: status %d", ErrUnexpectedStatus, resp.StatusCode)
	}

	// An indexer is a third party we do not control, and the decoder streams
	// whatever it is fed straight into Go structs. Without a ceiling a
	// misbehaving or hostile endpoint can hold the process at its own chosen
	// size — the cap turns that into a bounded read that fails cleanly.
	dec := xml.NewDecoder(io.LimitReader(resp.Body, maxTorznabResponse))
	start, err := rootElement(dec)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
	// Jackett answers a query its tracker's caps cannot handle at all (a
	// search mode the tracker lacks, an IMDb movie search it cannot do) with
	// a 200 carrying an error document, and an invalid API key the same way.
	if start.Name.Local == "error" {
		var doc torznabError
		if err := dec.DecodeElement(&doc, &start); err != nil {
			return fmt.Errorf("%w: %w", ErrBadResponse, err)
		}
		return doc.err(resp.StatusCode)
	}
	if err := dec.DecodeElement(out, &start); err != nil {
		return fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
	return nil
}

// rootElement reads up to the document's first element, skipping the prolog.
func rootElement(dec *xml.Decoder) (xml.StartElement, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return xml.StartElement{}, err
		}
		if start, ok := tok.(xml.StartElement); ok {
			return start, nil
		}
	}
}

// torznabError is the error document of the Newznab/Torznab API:
// `<error code="203" description="Function Not Available: …"/>`.
type torznabError struct {
	XMLName     xml.Name `xml:"error"`
	Code        int      `xml:"code,attr"`
	Description string   `xml:"description,attr"`
}

// err files the document under the sentinel its code means. The spec's 1xx
// codes are about the account, 2xx about the request (missing or incorrect
// parameter, no such function, function not available); everything else —
// 900 above all, which Jackett answers for any exception its tracker raised —
// is a failure to answer, not a refusal of the query, and retrying it wider
// would only send a failing tracker more requests.
//
// The description is the indexer's own text. It is kept for the 1xx and 2xx
// codes, whose wording is the spec's or Jackett's fixed sentences; a 900
// carries an exception's whole text, which can hold the tracker's URL and so
// its passkey, and is dropped.
func (e torznabError) err(status int) error {
	switch {
	case e.Code >= 100 && e.Code < 200:
		return fmt.Errorf("%w: status %d: torznab error %d: %s",
			ErrUnauthorized, status, e.Code, e.description())
	case e.Code >= 200 && e.Code < 300:
		return fmt.Errorf("%w: status %d: %w: torznab error %d: %s",
			ErrUnexpectedStatus, status, ErrBadRequest, e.Code, e.description())
	default:
		return fmt.Errorf("%w: status %d: torznab error %d",
			ErrUnexpectedStatus, status, e.Code)
	}
}

func (e torznabError) description() string {
	d := strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, strings.TrimSpace(e.Description))
	if len(d) <= maxTorznabErrorDescription {
		return d
	}
	cut := maxTorznabErrorDescription
	for cut > 0 && !utf8.RuneStart(d[cut]) {
		cut--
	}
	return d[:cut] + "…"
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
