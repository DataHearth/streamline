package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

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
}

// newznab category roots used to keep movie searches from returning TV (and
// vice versa); Prowlarr forwards these to each sub-indexer.
const (
	catMovies = "2000"
	catTV     = "5000"
)

func (p *Prowlarr) Search(
	ctx context.Context,
	params SearchParams,
) ([]SearchResult, error) {
	q := url.Values{
		"query": {params.Query},
		"type":  {"search"},
		"limit": {"100"},
		// indexerIds=-2 restricts the fan-out to torrent indexers only —
		// streamline can't grab usenet, so skip those trackers entirely.
		"indexerIds": {"-2"},
	}
	switch {
	case params.TVDBID > 0 || params.Season > 0:
		q.Set("categories", catTV)
	case params.TMDBID > 0:
		q.Set("categories", catMovies)
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
func (p *Prowlarr) Feed(context.Context) ([]SearchResult, error) {
	return nil, nil
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
		return err
	}
	req.Header.Set("X-Api-Key", p.apiKey)

	resp, err := p.client.Do(req)
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
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
	return nil
}
