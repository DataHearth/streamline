package mediaserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/internal/otelx"
)

type Jellyfin struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewJellyfin(baseURL, apiKey string) *Jellyfin {
	return &Jellyfin{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  otelx.HTTPClient,
	}
}

func (j *Jellyfin) TestConnection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		j.baseURL+"/System/Info",
		nil,
	)
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", j.apiKey)

	resp, err := j.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jellyfin: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (j *Jellyfin) RefreshLibrary(ctx context.Context, _, _ string) error {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		j.baseURL+"/Library/Refresh",
		nil,
	)
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", j.apiKey)

	resp, err := j.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jellyfin refresh: unexpected status %d", resp.StatusCode)
	}

	slog.InfoContext(ctx, "jellyfin library refresh triggered")
	return nil
}

type jellyfinItemsResponse struct {
	Items []jellyfinItem `json:"Items"`
}

type jellyfinItem struct {
	ID          string            `json:"Id"`
	Name        string            `json:"Name"`
	Type        string            `json:"Type"`
	Year        uint16            `json:"ProductionYear"`
	ProviderIds map[string]string `json:"ProviderIds"`
}

// providerIDEquals reports whether Jellyfin's ProviderIds map carries the given
// provider id. The key is matched case-insensitively (Jellyfin stores "Tmdb"/
// "Tvdb"). Jellyfin silently ignores an AnyProviderIdEquals filter it can't
// honor and returns the first library item, so the deep-link lookups verify the
// match here instead of trusting the server-side filter.
func providerIDEquals(ids map[string]string, provider string, want uint64) bool {
	target := strconv.FormatUint(want, 10)
	for k, v := range ids {
		if strings.EqualFold(k, provider) && v == target {
			return true
		}
	}
	return false
}

// MovieDeepLink returns a Jellyfin/Emby web URL pointing at the movie's
// details page. Tries TMDB-provider-id match first (exact), falls back to
// title + year. Both Jellyfin and Emby share this URL hash route. Returns
// ErrMovieNotFound when no item matches.
func (j *Jellyfin) MovieDeepLink(
	ctx context.Context, _ string, tmdbID uint32, title string, year uint16,
) (string, error) {
	id, err := j.findMovieID(ctx, tmdbID, title, year)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"%s/web/index.html#!/details?id=%s",
		strings.TrimRight(j.baseURL, "/"), id,
	), nil
}

func (j *Jellyfin) findMovieID(
	ctx context.Context, tmdbID uint32, title string, year uint16,
) (string, error) {
	if tmdbID > 0 {
		items, err := j.fetchItems(ctx, url.Values{
			"Recursive":           {"true"},
			"IncludeItemTypes":    {"Movie"},
			"Fields":              {"ProviderIds"},
			"AnyProviderIdEquals": {fmt.Sprintf("tmdb.%d", tmdbID)},
			"Limit":               {"5"},
		})
		if err == nil {
			for _, it := range items {
				if providerIDEquals(it.ProviderIds, "tmdb", uint64(tmdbID)) {
					return it.ID, nil
				}
			}
		}
	}
	q := url.Values{
		"Recursive":        {"true"},
		"IncludeItemTypes": {"Movie"},
		"SearchTerm":       {title},
		"Limit":            {"5"},
	}
	if year > 0 {
		q.Set("Years", strconv.FormatUint(uint64(year), 10))
	}
	if id := firstItemID(j.fetchItems(ctx, q)); id != "" {
		return id, nil
	}
	return "", ErrMovieNotFound
}

// TVShowDeepLink mirrors MovieDeepLink for series (Emby shares this API). The
// hintSection is Plex-only and ignored here.
func (j *Jellyfin) TVShowDeepLink(
	ctx context.Context, _ string, tvdbID uint32, title string, year uint16,
) (string, error) {
	id, err := j.findSeriesID(ctx, tvdbID, title, year)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"%s/web/index.html#!/details?id=%s",
		strings.TrimRight(j.baseURL, "/"), id,
	), nil
}

func (j *Jellyfin) findSeriesID(
	ctx context.Context, tvdbID uint32, title string, year uint16,
) (string, error) {
	if tvdbID > 0 {
		items, err := j.fetchItems(ctx, url.Values{
			"Recursive":           {"true"},
			"IncludeItemTypes":    {"Series"},
			"Fields":              {"ProviderIds"},
			"AnyProviderIdEquals": {fmt.Sprintf("tvdb.%d", tvdbID)},
			"Limit":               {"5"},
		})
		if err == nil {
			for _, it := range items {
				if providerIDEquals(it.ProviderIds, "tvdb", uint64(tvdbID)) {
					return it.ID, nil
				}
			}
		}
	}
	q := url.Values{
		"Recursive":        {"true"},
		"IncludeItemTypes": {"Series"},
		"SearchTerm":       {title},
		"Limit":            {"5"},
	}
	if year > 0 {
		q.Set("Years", strconv.FormatUint(uint64(year), 10))
	}
	if id := firstItemID(j.fetchItems(ctx, q)); id != "" {
		return id, nil
	}
	return "", ErrShowNotFound
}

// firstItemID returns the first non-empty item id, or "" on error/no match. The
// IncludeItemTypes filter already constrains the response to the wanted kind, so
// the title-search fallback just takes the top hit.
func firstItemID(items []jellyfinItem, err error) string {
	if err != nil {
		return ""
	}
	for _, it := range items {
		if it.ID != "" {
			return it.ID
		}
	}
	return ""
}

func (j *Jellyfin) fetchItems(
	ctx context.Context, q url.Values,
) ([]jellyfinItem, error) {
	endpoint := j.baseURL + "/Items?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Emby-Token", j.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := j.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jellyfin items: status %d", resp.StatusCode)
	}
	var body jellyfinItemsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("jellyfin items decode: %w", err)
	}
	return body.Items, nil
}
