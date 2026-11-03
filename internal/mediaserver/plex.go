package mediaserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/internal/otelx"
)

// Response ceilings for Plex and Jellyfin answers. The small replies (identity,
// the section list, a PIN) are a few KiB; a library query answers one title
// and its matches.
const (
	maxSmallResponse   = 1 << 20
	maxLibraryResponse = 16 << 20
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
	req RefreshRequest,
) (RefreshScope, error) {
	if len(req.Folders) == 0 && req.SectionKey != "" {
		return ScopeLibrary, p.refreshSection(ctx, req.SectionKey, "")
	}
	sections, err := p.ListSections(ctx)
	if err != nil {
		if req.SectionKey == "" {
			return "", err
		}
		// The configured section needs no listing: the rescan the folders
		// would have narrowed still reaches it.
		slog.WarnContext(ctx,
			"plex: listing the sections failed, rescanning the configured one",
			"section", req.SectionKey, "error", err)
		return ScopeLibrary, p.refreshSection(ctx, req.SectionKey, "")
	}
	if len(req.Folders) > 0 {
		if targets, ok := plexTargets(sections, req); ok {
			return ScopeFolders, p.refreshFolders(ctx, targets)
		}
		slog.InfoContext(ctx,
			"plex: a folder lies outside every section, rescanning the library",
			"folders", req.Folders,
			"path", req.Root,
		)
	}
	return ScopeLibrary, p.refreshLibrary(ctx, sections, req)
}

// plexTarget is one partial scan: a folder and the section holding it.
type plexTarget struct {
	section string
	folder  string
}

// plexTargets assigns every folder to the section whose location holds it
// most narrowly, among the configured section when there is one, else among
// the sections of the request's type. ok is false as soon as one folder has
// none: Plex ignores a partial scan of a path outside the section, so the
// change would never be seen.
func plexTargets(sections []Section, req RefreshRequest) ([]plexTarget, bool) {
	var locs, keys []string
	for _, s := range sections {
		if req.SectionKey != "" && s.Key != req.SectionKey {
			continue
		}
		if req.SectionKey == "" && req.SectionType != "" &&
			s.Type != req.SectionType {
			continue
		}
		for _, l := range s.Locations {
			locs = append(locs, l)
			keys = append(keys, s.Key)
		}
	}
	out := make([]plexTarget, 0, len(req.Folders))
	for _, f := range req.Folders {
		i := longestContaining(f, locs)
		if i < 0 {
			return nil, false
		}
		out = append(out, plexTarget{section: keys[i], folder: f})
	}
	return out, true
}

// refreshFolders sends one partial scan per folder: Plex takes a single path
// per call.
func (p *Plex) refreshFolders(ctx context.Context, targets []plexTarget) error {
	var errs []error
	for _, t := range targets {
		if err := p.refreshSection(ctx, t.section, t.folder); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// refreshLibrary rescans whole sections: the configured one, else the one
// holding the library root, else every section (of the request's type).
func (p *Plex) refreshLibrary(
	ctx context.Context,
	sections []Section,
	req RefreshRequest,
) error {
	if req.SectionKey != "" {
		return p.refreshSection(ctx, req.SectionKey, "")
	}
	// Plex reports its *own* mount paths, which in any containerised install
	// differ from ours (/srv/streamline/movies here, /data/movies there)
	// unless path_mappings translates them, so the match can find nothing.
	// Refreshing every section is what Jellyfin already does and beats
	// importing a file nobody ever sees; configure library_section or a
	// mapping to scope it back down to one.
	if key := sectionForRoot(sections, req); key != "" {
		return p.refreshSection(ctx, key, "")
	}
	slog.WarnContext(
		ctx,
		"plex: no section matches the library path, refreshing all sections",
		"path",
		req.Root,
		"section_type",
		req.SectionType,
	)

	var errs []error
	matched := 0
	for _, s := range sections {
		if req.SectionType != "" && s.Type != req.SectionType {
			continue
		}
		matched++
		if err := p.refreshSection(ctx, s.Key, ""); err != nil {
			errs = append(errs, err)
		}
	}
	if matched == 0 && req.SectionType != "" {
		return fmt.Errorf("plex: no %s library section to refresh", req.SectionType)
	}
	return errors.Join(errs...)
}

// sectionForRoot returns the key of the section (of the request's type) whose
// location is the library root or holds it, or "" when none does.
func sectionForRoot(sections []Section, req RefreshRequest) string {
	var locs, keys []string
	for _, s := range sections {
		if req.SectionType != "" && s.Type != req.SectionType {
			continue
		}
		for _, l := range s.Locations {
			locs = append(locs, l)
			keys = append(keys, s.Key)
		}
	}
	if i := longestContaining(req.Root, locs); i >= 0 {
		return keys[i]
	}
	return ""
}

// refreshSection rescans one section, or only folder inside it when folder is
// set (Plex's partial scan).
func (p *Plex) refreshSection(ctx context.Context, key, folder string) error {
	endpoint := fmt.Sprintf(
		"%s/library/sections/%s/refresh",
		p.baseURL,
		url.PathEscape(key),
	)
	if folder != "" {
		endpoint += "?" + url.Values{"path": {folder}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
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
		"folder", folder,
	)
	return nil
}

// Section is a Plex library section: the unit RefreshLibrary rescans, whole or
// one folder of it. Locations are filesystem paths Plex itself sees (which
// differ from Streamline's own mounts unless path_mappings translates them).
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
	if err := otelx.DecodeJSON(resp.Body, maxSmallResponse, &raw); err != nil {
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
	RatingKey string `json:"ratingKey"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Year      uint16 `json:"year"`
	Guid      string `json:"guid"`
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
	if err := otelx.DecodeJSON(resp.Body, maxSmallResponse, &body); err != nil {
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
	if err := otelx.DecodeJSON(resp.Body, maxLibraryResponse, &body); err != nil {
		return "", fmt.Errorf("plex section decode: %w", err)
	}
	for _, m := range body.MediaContainer.Metadata {
		if m.Type == itemType && m.RatingKey != "" {
			return m.RatingKey, nil
		}
	}
	return "", nil
}
