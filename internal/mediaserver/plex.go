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

type Plex struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewPlex(baseURL, token string) *Plex {
	return &Plex{
		baseURL: baseURL,
		token:   token,
		client:  otelx.HTTPClient,
	}
}

func (p *Plex) TestConnection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		p.baseURL+"/library/sections",
		nil,
	)
	if err != nil {
		return err
	}
	req.Header.Set("X-Plex-Token", p.token)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("plex: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (p *Plex) RefreshLibrary(
	ctx context.Context,
	libraryPath, sectionKey string,
) error {
	key := sectionKey
	if key == "" {
		k, err := p.findSection(ctx, libraryPath)
		if err != nil {
			return err
		}
		key = k
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet,
		fmt.Sprintf("%s/library/sections/%s/refresh", p.baseURL, key),
		nil,
	)
	if err != nil {
		return err
	}
	req.Header.Set("X-Plex-Token", p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("plex refresh: unexpected status %d", resp.StatusCode)
	}

	slog.InfoContext(ctx,
		"plex library refreshed",
		"section", key,
		"path", libraryPath,
	)
	return nil
}

func (p *Plex) findSection(ctx context.Context, libraryPath string) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		p.baseURL+"/library/sections",
		nil,
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Plex-Token", p.token)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("plex sections: unexpected status %d", resp.StatusCode)
	}

	var sections plexSectionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&sections); err != nil {
		return "", fmt.Errorf("plex sections decode: %w", err)
	}

	for _, dir := range sections.MediaContainer.Directory {
		for _, loc := range dir.Location {
			if loc.Path == libraryPath {
				return dir.Key, nil
			}
		}
	}

	return "", fmt.Errorf("plex: no section found for path %s", libraryPath)
}

// Section is a Plex library section: the unit RefreshLibrary targets when
// given a non-empty sectionKey. Locations are filesystem paths Plex itself
// sees (which may differ from Streamline's own mounts).
type Section struct {
	Key       string
	Name      string
	Locations []string
	Type      string
}

func (p *Plex) ListSections(ctx context.Context) ([]Section, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		p.baseURL+"/library/sections",
		nil,
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Plex-Token", p.token)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"plex sections: unexpected status %d",
			resp.StatusCode,
		)
	}

	var raw plexSectionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("plex sections decode: %w", err)
	}

	out := make([]Section, 0, len(raw.MediaContainer.Directory))
	for _, dir := range raw.MediaContainer.Directory {
		s := Section{
			Key:  dir.Key,
			Name: dir.Title,
			Type: dir.Type,
		}
		for _, l := range dir.Location {
			s.Locations = append(s.Locations, l.Path)
		}
		out = append(out, s)
	}
	return out, nil
}

type plexSectionsResponse struct {
	MediaContainer struct {
		Directory []plexDirectory `json:"Directory"`
	} `json:"MediaContainer"`
}

type plexDirectory struct {
	Key      string         `json:"key"`
	Title    string         `json:"title"`
	Type     string         `json:"type"`
	Location []plexLocation `json:"Location"`
}

type plexLocation struct {
	Path string `json:"path"`
}

type plexIdentityResponse struct {
	MediaContainer struct {
		MachineIdentifier string `json:"machineIdentifier"`
	} `json:"MediaContainer"`
}

type plexMetadataResponse struct {
	MediaContainer struct {
		Metadata []plexMetadataItem `json:"Metadata"`
	} `json:"MediaContainer"`
}

type plexMetadataItem struct {
	RatingKey string         `json:"ratingKey"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Year      uint16         `json:"year"`
	Guid      string         `json:"guid"`
	Guids     []plexGuidItem `json:"Guid"`
}

type plexGuidItem struct {
	ID string `json:"id"`
}

func (p *Plex) identity(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, p.baseURL+"/identity", nil,
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Plex-Token", p.token)
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("plex identity: unexpected status %d", resp.StatusCode)
	}
	var body plexIdentityResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("plex identity decode: %w", err)
	}
	if body.MediaContainer.MachineIdentifier == "" {
		return "", fmt.Errorf("plex identity: empty machineIdentifier")
	}
	return body.MediaContainer.MachineIdentifier, nil
}

// MovieDeepLink returns a Plex Web URL pointing at the movie's details page.
// Enumerates all movie-type sections on the server (Plex commonly splits
// movies across "Movies", "4K", "Documentaries", etc.) and queries each
// in turn — `hintSection`, if set and present in the list, is checked
// first to minimise round-trips on the happy path. Within each section
// it tries TMDB-GUID match first (exact), then title+year (fuzzy).
// Returns ErrMovieNotFound when no section yields a match.
func (p *Plex) MovieDeepLink(
	ctx context.Context,
	hintSection string,
	tmdbID uint32,
	title string,
	year uint16,
) (string, error) {
	machineID, err := p.identity(ctx)
	if err != nil {
		return "", fmt.Errorf("plex identity: %w", err)
	}
	sections, err := p.movieSectionKeys(ctx, hintSection)
	if err != nil {
		return "", fmt.Errorf("plex sections: %w", err)
	}
	if len(sections) == 0 {
		return "", ErrMovieNotFound
	}
	for _, sectionKey := range sections {
		if k := p.findMovieInSection(
			ctx,
			sectionKey,
			tmdbID,
			title,
			year,
		); k != "" {
			return fmt.Sprintf(
				"%s/web/index.html#!/server/%s/details?key=/library/metadata/%s",
				strings.TrimRight(p.baseURL, "/"), machineID, k,
			), nil
		}
	}
	return "", ErrMovieNotFound
}

// movieSectionKeys returns the keys of all movie-type sections, with hint
// floated to the front when it matches a movie section.
func (p *Plex) movieSectionKeys(
	ctx context.Context, hint string,
) ([]string, error) {
	all, err := p.ListSections(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(all))
	var hinted bool
	for _, s := range all {
		if s.Type != "movie" {
			continue
		}
		if hint != "" && s.Key == hint {
			hinted = true
			continue
		}
		keys = append(keys, s.Key)
	}
	if hinted {
		keys = append([]string{hint}, keys...)
	}
	return keys, nil
}

// findMovieInSection probes one section, swallowing per-section errors so the
// outer loop can fall through to the next section instead of bailing on a
// transient hiccup.
func (p *Plex) findMovieInSection(
	ctx context.Context,
	sectionKey string,
	tmdbID uint32,
	title string,
	year uint16,
) string {
	if tmdbID > 0 {
		q := url.Values{
			"type": {"1"},
			"guid": {fmt.Sprintf("tmdb://%d", tmdbID)},
		}
		if k, err := p.querySection(ctx, sectionKey, q, "movie"); err == nil &&
			k != "" {
			return k
		}
	}
	q := url.Values{"type": {"1"}, "title": {title}}
	if year > 0 {
		q.Set("year", strconv.FormatUint(uint64(year), 10))
	}
	if k, err := p.querySection(ctx, sectionKey, q, "movie"); err == nil &&
		k != "" {
		return k
	}
	return ""
}

// TVShowDeepLink mirrors MovieDeepLink for TV-type sections: matches on the
// TVDB guid first, then title+year, and builds the same details URL.
func (p *Plex) TVShowDeepLink(
	ctx context.Context,
	hintSection string,
	tvdbID uint32,
	title string,
	year uint16,
) (string, error) {
	machineID, err := p.identity(ctx)
	if err != nil {
		return "", fmt.Errorf("plex identity: %w", err)
	}
	sections, err := p.tvSectionKeys(ctx, hintSection)
	if err != nil {
		return "", fmt.Errorf("plex sections: %w", err)
	}
	if len(sections) == 0 {
		return "", ErrShowNotFound
	}
	for _, sectionKey := range sections {
		if k := p.findShowInSection(ctx, sectionKey, tvdbID, title, year); k != "" {
			return fmt.Sprintf(
				"%s/web/index.html#!/server/%s/details?key=/library/metadata/%s",
				strings.TrimRight(p.baseURL, "/"), machineID, k,
			), nil
		}
	}
	return "", ErrShowNotFound
}

// tvSectionKeys returns the keys of all show-type sections, with hint floated
// to the front when it matches a TV section.
func (p *Plex) tvSectionKeys(
	ctx context.Context, hint string,
) ([]string, error) {
	all, err := p.ListSections(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(all))
	var hinted bool
	for _, s := range all {
		if s.Type != "show" {
			continue
		}
		if hint != "" && s.Key == hint {
			hinted = true
			continue
		}
		keys = append(keys, s.Key)
	}
	if hinted {
		keys = append([]string{hint}, keys...)
	}
	return keys, nil
}

// findShowInSection probes one show section by TVDB guid then title+year
// (Plex search type 2 = show), swallowing per-section errors.
func (p *Plex) findShowInSection(
	ctx context.Context,
	sectionKey string,
	tvdbID uint32,
	title string,
	year uint16,
) string {
	if tvdbID > 0 {
		q := url.Values{
			"type": {"2"},
			"guid": {fmt.Sprintf("tvdb://%d", tvdbID)},
		}
		if k, err := p.querySection(ctx, sectionKey, q, "show"); err == nil &&
			k != "" {
			return k
		}
	}
	q := url.Values{"type": {"2"}, "title": {title}}
	if year > 0 {
		q.Set("year", strconv.FormatUint(uint64(year), 10))
	}
	if k, err := p.querySection(ctx, sectionKey, q, "show"); err == nil &&
		k != "" {
		return k
	}
	return ""
}

func (p *Plex) querySection(
	ctx context.Context, sectionKey string, q url.Values, itemType string,
) (string, error) {
	endpoint := fmt.Sprintf("%s/library/sections/%s/all?%s",
		p.baseURL, sectionKey, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Plex-Token", p.token)
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("plex section lookup: status %d", resp.StatusCode)
	}
	var body plexMetadataResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("plex section decode: %w", err)
	}
	for _, m := range body.MediaContainer.Metadata {
		if m.Type == itemType && m.RatingKey != "" {
			return m.RatingKey, nil
		}
	}
	return "", nil
}
