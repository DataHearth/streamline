package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/datahearth/streamline/internal/otelx"
)

// Prowlarr queries a Prowlarr instance's native search API
// (GET /api/v1/search), which aggregates across every indexer Prowlarr
// manages in one call. Unlike Jackett, Prowlarr exposes no combined Torznab
// feed — its per-indexer Torznab endpoints (/{id}/api) only hit one tracker —
// so a dedicated JSON client is the only way to "query all indexers".
type Prowlarr struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewProwlarr(baseURL, apiKey string) *Prowlarr {
	return &Prowlarr{baseURL: baseURL, apiKey: apiKey, client: otelx.HTTPClient}
}

// prowlarrRelease is one entry of the /api/v1/search JSON array. Servarr
// serialises camelCase; only the fields streamline needs are decoded.
type prowlarrRelease struct {
	Title       string `json:"title"`
	InfoURL     string `json:"infoUrl"`
	DownloadURL string `json:"downloadUrl"`
	MagnetURL   string `json:"magnetUrl"`
	Size        int64  `json:"size"`
	Seeders     uint32 `json:"seeders"`
	Leechers    uint32 `json:"leechers"`
	Indexer     string `json:"indexer"`
	Protocol    string `json:"protocol"` // "torrent" | "usenet"
	PublishDate string `json:"publishDate"`

	// Provider ids the tracker published on the release, parsed by Prowlarr
	// out of the torznab attrs and re-emitted here. Absent decodes to 0, which
	// is "the tracker said nothing". ReleaseResource also carries imdbId and
	// tvMazeId; neither is decoded because nothing in the library has a
	// counterpart to compare them against.
	TMDBID uint32 `json:"tmdbId"`
	TVDBID uint32 `json:"tvdbId"`
}

// newznab category roots used to keep movie searches from returning TV (and
// vice versa); Prowlarr forwards these to each sub-indexer.
const (
	catMovies = "2000"
	catTV     = "5000"
)

func newznabCategory(kind MediaKind) string {
	switch kind {
	case KindMovie:
		return catMovies
	case KindTV:
		return catTV
	default:
		return ""
	}
}

// prowlarrSearchType picks the `type` param. It is not cosmetic: Prowlarr
// parses the {key:value} tokens out of the query string only under `tvsearch`
// or `movie` (NewznabRequest.QueryToParams), so a `search` type silently
// discards everything prowlarrQuery writes. The two must agree.
func prowlarrSearchType(kind MediaKind) string {
	switch kind {
	case KindMovie:
		return "movie"
	case KindTV:
		return "tvsearch"
	default:
		return "search"
	}
}

// prowlarrQuery renders the search term plus the {key:value} tokens Prowlarr
// parses back out of it. GET /api/v1/search takes only query/type/indexerIds/
// categories/limit/offset — there is no season or episode param — so the query
// string is the only channel there is. Prowlarr strips each matched token
// before handing the remainder to the trackers as the search term.
//
// Season and episode only. The id tokens are deliberately not sent: Prowlarr
// drops an indexer from the fan-out *entirely* when handed an id its caps
// don't declare (HttpIndexerBase.Fetch returns an empty result rather than
// falling back to a keyword search), so a tracker that simply doesn't index by
// tvdbid would contribute nothing instead of contributing what it has. The
// ids come back on the results instead, which filterProviderIDs uses and which
// costs no coverage. Season and episode are absent from that guard, so they
// narrow without ever excluding an indexer.
func prowlarrQuery(params SearchParams) string {
	q := strings.TrimSpace(params.Query)
	if params.Kind != KindTV {
		return q
	}
	var b strings.Builder
	b.WriteString(q)
	// Appended with no separator: Prowlarr removes the token text and trims,
	// so the term the trackers see is exactly the title either way — but a
	// season 0 token would name the specials, and every caller here treats a
	// zero season as "no season was named".
	//
	// The episode hangs off the season rather than standing alone. Prowlarr
	// renders the pair as one SxxEyy search string and yields nothing at all
	// for a season it was not given (TvSearchCriteria.GetEpisodeSearchString),
	// so a lone episode token narrows the tracker to "episode 3" of no
	// particular season.
	if params.Season > 0 {
		fmt.Fprintf(&b, "{season:%d}", params.Season)
		if params.Episode > 0 {
			fmt.Fprintf(&b, "{episode:%d}", params.Episode)
		}
	}
	return b.String()
}

func (p *Prowlarr) Search(
	ctx context.Context,
	params SearchParams,
) ([]SearchResult, error) {
	q := url.Values{
		"query": {prowlarrQuery(params)},
		"type":  {prowlarrSearchType(params.Kind)},
		"limit": {"100"},
		// indexerIds=-2 restricts the fan-out to torrent indexers only —
		// streamline can't grab usenet, so skip those trackers entirely.
		"indexerIds": {"-2"},
	}
	// Keyed off the search's own scope, never off whichever id happens to be
	// set: the id-less retry drops the ids by design, and deriving the
	// category from them sent no `categories` at all on that pass — a keyword
	// search over every indexer and every category. Prowlarr drops indexers
	// whose caps don't cover the root from the fan-out entirely
	// (ReleaseSearchService.Dispatch) and expands it into each tracker's own
	// children, so the root is the useful granularity.
	if cat := newznabCategory(params.Kind); cat != "" {
		q.Set("categories", cat)
	}

	var releases []prowlarrRelease
	if err := p.get(ctx, "/api/v1/search", q, &releases); err != nil {
		return nil, fmt.Errorf("prowlarr search: %w", err)
	}
	return mapProwlarrReleases(releases), nil
}

// Feed has no analogue in Prowlarr. Per the Servarr wiki, "an aggregate
// multi-indexer endpoint will not be added", and the search API requires a
// query — so there is no cross-indexer latest-releases feed to forward.
// rss-sync gets nothing for a Prowlarr entry, which is correct: RSS monitoring
// belongs on the individual trackers.
// Feed reports that Prowlarr has no forward-feed endpoint. It used to return
// an empty result, which is indistinguishable from "the feed was empty this
// tick" — so every 15-minute scan logged a fetch of 0 items for an indexer
// that was never going to produce any.
func (p *Prowlarr) Feed(context.Context) ([]SearchResult, error) {
	return nil, ErrFeedUnsupported
}

func (p *Prowlarr) TestConnection(ctx context.Context) error {
	return p.get(ctx, "/api/v1/health", nil, &json.RawMessage{})
}

func mapProwlarrReleases(releases []prowlarrRelease) []SearchResult {
	results := make([]SearchResult, 0, len(releases))
	for _, r := range releases {
		// streamline only drives torrent download clients; usenet results are
		// ungrabbable, so drop them rather than surface dead releases.
		if r.Protocol != "torrent" {
			continue
		}
		dl := r.DownloadURL
		if dl == "" {
			dl = r.MagnetURL
		}
		results = append(results, SearchResult{
			Title:       r.Title,
			InfoURL:     r.InfoURL,
			Download:    dl,
			Size:        r.Size,
			Seeders:     r.Seeders,
			Leechers:    r.Leechers,
			PublishDate: parsePubDate(r.PublishDate),
			// The meaningful indexer is the sub-tracker Prowlarr fanned out to,
			// not the Prowlarr entry; searchAll preserves a non-empty value.
			Indexer: r.Indexer,
			TMDBID:  r.TMDBID,
			TVDBID:  r.TVDBID,
		})
	}
	return results
}

func (p *Prowlarr) get(
	ctx context.Context,
	path string,
	params url.Values,
	out any,
) error {
	u := p.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return otelx.RedactTransportError(err)
	}
	req.Header.Set("X-Api-Key", p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, otelx.RedactTransportError(err))
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: status %d", ErrUnauthorized, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("%w: status %d", ErrUnexpectedStatus, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
	return nil
}
