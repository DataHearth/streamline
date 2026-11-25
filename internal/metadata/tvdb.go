package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"
	"golang.org/x/text/language"
)

// maxTokenResponse bounds TVDB's login reply, which carries one token.
const maxTokenResponse = 1 << 20

const tvdbBaseURL = "https://api4.thetvdb.com/v4"

type TVDB struct {
	apiKey  string
	BaseURL string
	client  *http.Client

	// language is the ISO 639-3 code (e.g. "eng") TVDB expects, derived from
	// the BCP47 metadata.language. Empty leaves records in their original
	// language.
	language string
	// country is metadata.tmdb_region as the ISO 3166-1 alpha-3 code TVDB
	// files content ratings under, lowercased ("fra"). Empty reads none.
	country string

	// One TVDB instance is shared by the TV service, the hygiene service and
	// the REST handlers, so every access to the cached token goes through mu.
	// mu is never held across a round trip: logins collapse through group
	// instead, so a stalled /login costs every waiter one timeout rather than
	// one each, and callers that already have a token are never blocked.
	mu    sync.Mutex
	group singleflight.Group
	token string // cached bearer token from /login
}

// NewTVDB builds a TVDB client from metadata.tvdb_api_key in the config
// singleton. The token is fetched lazily on first request. The hidden
// metadata.tvdb.base_url key overrides the TVDB API base URL when set (e2e
// seam).
func NewTVDB() *TVDB {
	m := config.Get().Metadata
	baseURL := config.HiddenString("metadata.tvdb.base_url")
	if baseURL == "" {
		baseURL = tvdbBaseURL
	}
	return &TVDB{
		apiKey:   config.SecretValue(m.TVDBAPIKey, m.TVDBAPIKeyFile),
		BaseURL:  baseURL,
		client:   otelx.HTTPClient,
		language: iso639_3(m.Language),
		country:  iso3166alpha3(m.TMDBRegion),
	}
}

func iso3166alpha3(alpha2 string) string {
	region, err := language.ParseRegion(alpha2)
	if err != nil {
		return ""
	}
	return strings.ToLower(region.ISO3())
}

// iso639_3 converts a BCP47 tag (metadata.language, e.g. "en") into the ISO
// 639-3 code TVDB uses for translations (e.g. "eng"). Empty in, empty out.
func iso639_3(bcp47 string) string {
	if bcp47 == "" {
		return ""
	}
	base, _ := language.Make(bcp47).Base()
	return base.ISO3()
}

// login returns the cached bearer token, fetching a fresh one when the cache
// is empty. Concurrent callers that find the cache empty collapse onto a
// single /login through singleflight — including the failing case, so a TVDB
// outage costs one round trip for the whole burst instead of one per caller.
func (t *TVDB) login(ctx context.Context) (string, error) {
	t.mu.Lock()
	cached := t.token
	t.mu.Unlock()
	if cached != "" {
		return cached, nil
	}
	token, err, _ := t.group.Do("login", func() (any, error) {
		return t.fetchToken(ctx)
	})
	if err != nil {
		return "", err
	}
	return token.(string), nil
}

func (t *TVDB) fetchToken(ctx context.Context) (string, error) {
	// A burst that queued behind an in-flight login is served by singleflight,
	// but a burst that queued behind the *previous* one arrives here with the
	// token already cached.
	t.mu.Lock()
	cached := t.token
	t.mu.Unlock()
	if cached != "" {
		return cached, nil
	}
	body, err := json.Marshal(map[string]string{"apikey": t.apiKey})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		t.BaseURL+"/login",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tvdb login: unexpected status %d", resp.StatusCode)
	}
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := otelx.DecodeJSON(resp.Body, maxTokenResponse, &out); err != nil {
		return "", err
	}
	t.mu.Lock()
	t.token = out.Data.Token
	t.mu.Unlock()
	return out.Data.Token, nil
}

// invalidate drops a token TVDB rejected. The stale check keeps a slow caller
// from wiping a token another goroutine already refreshed.
func (t *TVDB) invalidate(stale string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token == stale {
		t.token = ""
	}
}

func (t *TVDB) get(ctx context.Context, path string, out any) error {
	started := time.Now()
	token, err := t.login(ctx)
	if err != nil {
		recordTVDBRequest(ctx, path, 0, started)
		return err
	}
	resp, err := t.do(ctx, path, token)
	if err != nil {
		recordTVDBRequest(ctx, path, 0, started)
		return err
	}
	// TVDB tokens expire after ~1 month; a 401 means this one aged out, so
	// drop it, log in again and replay the request once.
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		slog.InfoContext(
			ctx,
			"tvdb token rejected, logging in again",
			"tvdb.endpoint",
			path,
		)
		t.invalidate(token)
		if token, err = t.login(ctx); err != nil {
			recordTVDBRequest(ctx, path, http.StatusUnauthorized, started)
			return err
		}
		if resp, err = t.do(ctx, path, token); err != nil {
			recordTVDBRequest(ctx, path, 0, started)
			return err
		}
	}
	defer resp.Body.Close()
	recordTVDBRequest(ctx, path, resp.StatusCode, started)
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		slog.WarnContext(ctx, "tvdb rate limited",
			"tvdb.endpoint", path,
			"retry_after", retryAfter.Round(time.Second))
		return &RateLimitedError{RetryAfter: retryAfter}
	case http.StatusNotFound:
		// Debug, because a 404 is how TVDB says "no record", which several
		// callers expect and handle (a season with no translation in the
		// language). The error still reaches the caller, and a caller for
		// which a 404 is a real fault (a series TVDB deleted) logs it there.
		slog.DebugContext(ctx, "tvdb request returned 404",
			"tvdb.endpoint", path)
		return fmt.Errorf("tvdb: %s: %w", path, ErrNotFound)
	default:
		slog.WarnContext(
			ctx,
			"tvdb request returned non-200",
			"tvdb.endpoint",
			path,
			"http.status_code",
			resp.StatusCode,
		)
		return fmt.Errorf("tvdb: unexpected status %d", resp.StatusCode)
	}
	return otelx.DecodeJSON(resp.Body, maxProviderResponse, out)
}

func (t *TVDB) do(
	ctx context.Context,
	path, token string,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := t.client.Do(req)
	if err != nil {
		slog.WarnContext(
			ctx,
			"tvdb request transport error",
			"tvdb.endpoint",
			path,
			"error",
			err,
		)
		return nil, err
	}
	return resp, nil
}

// atou16/atou32 parse TVDB's string-encoded numerics; return 0 on any failure.
func atou16(s string) uint16 {
	n, _ := strconv.ParseUint(s, 10, 16)
	return uint16(n)
}

func atou32(s string) uint32 {
	n, _ := strconv.ParseUint(s, 10, 32)
	return uint32(n)
}

// SearchSeries queries TVDB, retrying with each half of the query when the
// full one comes back empty. TVDB's relevance scorer returns nothing for a
// show's own complete title while returning that show for substrings of it —
// "Zom 100 Bucket List of the Dead" finds nothing, "Zom 100 Bucket List" and
// "Bucket List of the Dead" both find it. No rule separates the substrings
// that work, so both halves are tried and the first non-empty answer wins.
func (t *TVDB) SearchSeries(ctx context.Context, query string) ([]TVResult, error) {
	ctx, span := tracer.Start(ctx, "metadata.tvdb.search_series",
		trace.WithAttributes(attribute.String("query", query)))
	defer span.End()

	out, err := t.searchSeries(ctx, query)
	if err != nil || len(out) > 0 {
		return out, err
	}
	for _, alt := range queryHalves(query) {
		out, err = t.searchSeries(ctx, alt)
		if err != nil {
			return nil, err
		}
		if len(out) > 0 {
			span.SetAttributes(attribute.String("query.fallback", alt))
			return out, nil
		}
	}
	return out, nil
}

// queryHalves splits query into its leading and trailing half, both rounded up
// so a 6-word query yields two 3-word ones. Returns nil for a query too short
// for the halves to carry a title.
func queryHalves(query string) []string {
	words := strings.Fields(query)
	if len(words) < 4 {
		return nil
	}
	h := (len(words) + 1) / 2
	return []string{
		strings.Join(words[:h], " "),
		strings.Join(words[len(words)-h:], " "),
	}
}

func (t *TVDB) searchSeries(ctx context.Context, query string) ([]TVResult, error) {
	span := trace.SpanFromContext(ctx)

	var resp struct {
		Data []struct {
			TVDBID       string            `json:"tvdb_id"`
			Name         string            `json:"name"`
			Year         string            `json:"year"`
			Network      string            `json:"network"`
			Overview     string            `json:"overview"`
			ImageURL     string            `json:"image_url"`
			Aliases      []string          `json:"aliases"`
			Translations map[string]string `json:"translations"` // lang -> name
			Overviews    map[string]string `json:"overviews"`    // lang -> overview
		} `json:"data"`
	}
	params := url.Values{"query": {query}, "type": {"series"}}
	if err := t.get(ctx, "/search?"+params.Encode(), &resp); err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("tvdb search: %w", err))
	}
	out := make([]TVResult, 0, len(resp.Data))
	for _, r := range resp.Data {
		title := r.Name
		if v := r.Translations[t.language]; t.language != "" && v != "" {
			title = v
		}
		overview := r.Overview
		if v := r.Overviews[t.language]; t.language != "" && v != "" {
			overview = v
		}
		// Every translated name is an alias for matching purposes: a folder is
		// as likely to carry the romaji or the original-language title as the
		// English one, and TVDB returns both lists.
		aliases := append([]string(nil), r.Aliases...)
		for _, v := range r.Translations {
			aliases = append(aliases, v)
		}
		year := atou16(r.Year)
		out = append(out, TVResult{
			TVDBID:        atou32(r.TVDBID),
			Title:         stripDisambiguationYear(title, year),
			OriginalTitle: stripDisambiguationYear(r.Name, year),
			Year:          year,
			Network:       r.Network,
			Overview:      overview,
			PosterPath:    r.ImageURL,
			Aliases:       aliases,
		})
	}
	return out, nil
}

// stripDisambiguationYear drops the " (YYYY)" TVDB appends to the name of a
// series that shares it with another ("Bodies (2023)"). Year already carries
// it, so `{title} ({year})` printed it twice, and Jellyfin found nothing
// searching the doubled folder name. A different year is part of the title
// itself and stays.
func stripDisambiguationYear(title string, year uint16) string {
	if year == 0 {
		return title
	}
	t, ok := strings.CutSuffix(title, fmt.Sprintf(" (%d)", year))
	if !ok || strings.TrimSpace(t) == "" {
		return title
	}
	return t
}

func (t *TVDB) GetSeries(ctx context.Context, tvdbID uint32) (*TVDetails, error) {
	ctx, span := tracer.Start(ctx, "metadata.tvdb.get_series",
		trace.WithAttributes(attribute.Int("tvdb.id", int(tvdbID))))
	defer span.End()

	var ext struct {
		Data struct {
			ID               uint32 `json:"id"`
			Name             string `json:"name"`
			Year             string `json:"year"`
			Overview         string `json:"overview"`
			AverageRuntime   uint16 `json:"averageRuntime"`
			OriginalCountry  string `json:"originalCountry"`
			OriginalLanguage string `json:"originalLanguage"`
			Image            string `json:"image"`
			FirstAired       string `json:"firstAired"`
			Status           struct {
				Name string `json:"name"`
			} `json:"status"`
			Genres []struct {
				Name string `json:"name"`
			} `json:"genres"`
			LatestNetwork struct {
				Name string `json:"name"`
			} `json:"latestNetwork"`
			Seasons []struct {
				ID     uint64 `json:"id"`
				Number uint16 `json:"number"`
				Name   string `json:"name"`
				// The languages the season's name is translated into, as
				// codes, not the names themselves. nil when TVDB left the
				// field out, which says nothing either way.
				NameTranslations []string `json:"nameTranslations"`
				Type             struct {
					Type string `json:"type"`
				} `json:"type"`
			} `json:"seasons"`
			Aliases []struct {
				Name string `json:"name"`
			} `json:"aliases"`
			RemoteIDs []struct {
				ID         string `json:"id"`
				SourceName string `json:"sourceName"`
			} `json:"remoteIds"`
			ContentRatings []struct {
				Name    string `json:"name"`
				Country string `json:"country"`
			} `json:"contentRatings"`
			Artworks []tvdbArtwork `json:"artworks"`
			// Populated only when we request ?meta=translations.
			Translations struct {
				NameTranslations []struct {
					Language string `json:"language"`
					Name     string `json:"name"`
				} `json:"nameTranslations"`
				OverviewTranslations []struct {
					Language string `json:"language"`
					Overview string `json:"overview"`
				} `json:"overviewTranslations"`
			} `json:"translations"`
		} `json:"data"`
	}
	extPath := fmt.Sprintf("/series/%d/extended", tvdbID)
	if t.language != "" {
		extPath += "?meta=translations"
	}
	if err := t.get(ctx, extPath, &ext); err != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("tvdb get series: %w", err),
		)
	}

	d := &TVDetails{
		TVDBID:        ext.Data.ID,
		Title:         ext.Data.Name,
		OriginalTitle: ext.Data.Name, // preserved before the language override below
		Year:          atou16(ext.Data.Year),
		Network:       ext.Data.LatestNetwork.Name,
		Overview:      ext.Data.Overview,
		PosterPath:    ext.Data.Image,
		LogoPaths:     pickTVDBLogos(ext.Data.Artworks, t.language),
		Status:        normalizeStatus(ext.Data.Status.Name),
		Type:          SeriesStandard, // refined below if a genre marks it anime
		Runtime:       ext.Data.AverageRuntime,
		FirstAired:    ext.Data.FirstAired,
		// TVDB v4 removed user ratings; `score` is an arbitrary popularity
		// metric (not a 0-10 rating), so Rating is left unset (0 = unknown).
	}
	for _, g := range ext.Data.Genres {
		d.Genres = append(d.Genres, g.Name)
	}
	for _, a := range ext.Data.Aliases {
		d.Aliases = append(d.Aliases, a.Name)
	}
	for _, tr := range ext.Data.Translations.NameTranslations {
		d.Aliases = append(d.Aliases, tr.Name)
	}
	for _, r := range ext.Data.RemoteIDs {
		if strings.EqualFold(r.SourceName, "imdb") {
			d.IMDbID = r.ID
			break
		}
	}
	for _, r := range ext.Data.ContentRatings {
		if t.country != "" && strings.EqualFold(r.Country, t.country) {
			d.Certification = r.Name
			break
		}
	}
	if inferAnime(d.Genres, ext.Data.OriginalLanguage, ext.Data.OriginalCountry) {
		d.Type = SeriesAnime
	}
	seasonRefs := make([]tvdbSeasonRef, 0, len(ext.Data.Seasons))
	for _, s := range ext.Data.Seasons {
		if s.Type.Type == "official" || s.Type.Type == "" {
			d.Seasons = append(d.Seasons, SeasonInfo{Number: s.Number, Name: s.Name})
			seasonRefs = append(seasonRefs, tvdbSeasonRef{
				id:        s.ID,
				nameLangs: s.NameTranslations,
			})
		}
	}

	// Override name/overview with the configured language when TVDB has a
	// translation; otherwise the original-language record stands.
	if t.language != "" {
		for _, tr := range ext.Data.Translations.NameTranslations {
			if strings.EqualFold(tr.Language, t.language) && tr.Name != "" {
				d.Title = tr.Name
				break
			}
		}
		for _, tr := range ext.Data.Translations.OverviewTranslations {
			if strings.EqualFold(tr.Language, t.language) && tr.Overview != "" {
				d.Overview = tr.Overview
				break
			}
		}
		t.translateSeasons(ctx, d.Seasons, seasonRefs)
	}
	d.Title = stripDisambiguationYear(d.Title, d.Year)
	d.OriginalTitle = stripDisambiguationYear(d.OriginalTitle, d.Year)

	eps, err := t.episodes(ctx, tvdbID, t.language)
	if err != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("tvdb episodes: %w", err),
		)
	}
	d.Episodes = eps
	t.fillUntranslatedEpisodes(ctx, tvdbID, d.Episodes)

	return d, nil
}

// tvdbSeriesClearLogo is TVDB's artwork type for a series' transparent logo.
const tvdbSeriesClearLogo = 23

// tvdbArtwork is one image of a series. Language is an ISO 639-3 code, null for
// art with no text, and Score is null when TVDB has not ranked it.
type tvdbArtwork struct {
	Image    string  `json:"image"`
	Language string  `json:"language"`
	Type     int     `json:"type"`
	Score    float64 `json:"score"`
}

// pickTVDBLogos is rankLogos over a series' clear logos, for an ISO 639-3 code.
func pickTVDBLogos(arts []tvdbArtwork, lang string) []string {
	var imgs []logoImage
	for _, a := range arts {
		if a.Type == tvdbSeriesClearLogo {
			imgs = append(imgs, logoImage{
				ref:   a.Image,
				lang:  a.Language,
				score: a.Score,
			})
		}
	}
	return rankLogos(imgs, lang, "eng")
}

// episodes walks a series' default-order episode pages in one language. lang
// is a TVDB ISO 639-3 code; empty reads the base records, which TVDB keeps in
// the series' original language. A language variant answers an episode it
// holds no translation for with an empty name and overview — TVDB has no
// per-episode fallback of its own.
func (t *TVDB) episodes(
	ctx context.Context,
	tvdbID uint32,
	lang string,
) ([]EpisodeInfo, error) {
	langSeg := ""
	if lang != "" {
		langSeg = "/" + lang
	}
	var out []EpisodeInfo
	for page := 0; ; page++ {
		var epResp struct {
			Data struct {
				Episodes []struct {
					ID             uint32 `json:"id"`
					SeasonNumber   uint16 `json:"seasonNumber"`
					Number         uint16 `json:"number"`
					AbsoluteNumber uint16 `json:"absoluteNumber"`
					Name           string `json:"name"`
					Overview       string `json:"overview"`
					Aired          string `json:"aired"`
				} `json:"episodes"`
			} `json:"data"`
			Links struct {
				Next *string `json:"next"`
			} `json:"links"`
		}
		p := fmt.Sprintf(
			"/series/%d/episodes/default%s?page=%d",
			tvdbID,
			langSeg,
			page,
		)
		if err := t.get(ctx, p, &epResp); err != nil {
			return nil, err
		}
		for _, e := range epResp.Data.Episodes {
			out = append(out, EpisodeInfo{
				TVDBID:         e.ID,
				SeasonNumber:   e.SeasonNumber,
				Number:         e.Number,
				AbsoluteNumber: e.AbsoluteNumber,
				Title:          e.Name,
				Overview:       e.Overview,
				AirDate:        parseAirDate(e.Aired),
			})
		}
		if epResp.Links.Next == nil || *epResp.Links.Next == "" {
			return out, nil
		}
	}
}

// fillUntranslatedEpisodes gives an aired episode the configured language has
// no title for the title and overview of a fallback, in place: English, then
// the original-language base record. Translations trail the original airing by
// weeks, so on a French install a running English show's new season read as
// a column of blank titles — rendered "TBA" — over episodes already
// downloaded. A later refresh replaces the fallback once TVDB has the
// translation, since reconciliation overwrites any changed title.
//
// Each fallback costs one more pass over the episode pages and is spent only
// while an aired episode is still untitled: an announced future episode is
// often untitled in every language, and checking those would re-fetch every
// running show on every refresh. A failed fallback keeps the blanks rather
// than failing the refresh.
func (t *TVDB) fillUntranslatedEpisodes(
	ctx context.Context,
	tvdbID uint32,
	eps []EpisodeInfo,
) {
	if t.language == "" {
		return // already the base records
	}
	fallbacks := []string{"eng", ""}
	if t.language == "eng" {
		fallbacks = fallbacks[1:]
	}
	now := time.Now()
	for _, lang := range fallbacks {
		if !slices.ContainsFunc(eps, func(e EpisodeInfo) bool {
			return untitledAired(e, now)
		}) {
			return
		}
		alt, err := t.episodes(ctx, tvdbID, lang)
		if err != nil {
			slog.WarnContext(ctx, "tvdb episode title fallback failed",
				"tvdb.id", tvdbID, "fallback.language", lang, "error", err)
			continue
		}
		type key struct{ season, number uint16 }
		byKey := make(map[key]EpisodeInfo, len(alt))
		for _, a := range alt {
			byKey[key{a.SeasonNumber, a.Number}] = a
		}
		for i := range eps {
			if !untitledAired(eps[i], now) {
				continue
			}
			a := byKey[key{eps[i].SeasonNumber, eps[i].Number}]
			eps[i].Title = a.Title
			if eps[i].Overview == "" {
				eps[i].Overview = a.Overview
			}
		}
	}
}

func untitledAired(e EpisodeInfo, now time.Time) bool {
	return e.Title == "" && e.AirDate != nil && !e.AirDate.After(now)
}

type tvdbCharacter struct {
	Name       string `json:"name"`       // character name
	PersonName string `json:"personName"` // the person's real name
	PersonImg  string `json:"personImgURL"`
	PeopleType string `json:"peopleType"`
	PeopleID   uint32 `json:"peopleId"`
	Sort       int    `json:"sort"`
}

type tvdbCharacters struct {
	Data struct {
		Characters []tvdbCharacter `json:"characters"`
	} `json:"data"`
}

// tvdbDepartments groups TVDB's peopleType the way TMDB's department does.
var tvdbDepartments = map[string]string{
	"Director":           "Directing",
	"Writer":             "Writing",
	"Creator":            "Writing",
	"Producer":           "Production",
	"Executive Producer": "Production",
	"Showrunner":         "Production",
}

// GetSeriesCredits returns up to maxCastMembers actors for a series, ordered
// by TVDB's `sort`, then up to maxCrewMembers of its crew. Guest stars belong
// to episodes, not the series.
func (t *TVDB) GetSeriesCredits(
	ctx context.Context,
	tvdbID uint32,
) ([]CastMember, error) {
	ctx, span := tracer.Start(ctx, "metadata.tvdb.get_series_credits",
		trace.WithAttributes(attribute.Int("tvdb.id", int(tvdbID))))
	defer span.End()

	var ext tvdbCharacters
	if err := t.get(
		ctx,
		fmt.Sprintf("/series/%d/extended", tvdbID),
		&ext,
	); err != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("tvdb series credits: %w", err),
		)
	}
	return tvdbCredits(ext.Data.Characters, "Actor", "Guest Star"), nil
}

// GetEpisodeCredits returns an episode's guest stars, then its crew. Its
// actors are the series regulars TVDB repeats on every episode, which the
// series' own credits already hold.
func (t *TVDB) GetEpisodeCredits(
	ctx context.Context,
	episodeTVDBID uint32,
) ([]CastMember, error) {
	ctx, span := tracer.Start(ctx, "metadata.tvdb.get_episode_credits",
		trace.WithAttributes(attribute.Int("tvdb.episode.id", int(episodeTVDBID))))
	defer span.End()

	var ext tvdbCharacters
	if err := t.get(
		ctx,
		fmt.Sprintf("/episodes/%d/extended", episodeTVDBID),
		&ext,
	); err != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("tvdb episode credits: %w", err),
		)
	}
	return tvdbCredits(ext.Data.Characters, "Guest Star", "Actor"), nil
}

// tvdbCredits splits TVDB's characters into cast — the castType entries,
// with their character — and crew, every type but castType and skipType,
// each capped and in TVDB's `sort` order.
func tvdbCredits(chars []tvdbCharacter, castType, skipType string) []CastMember {
	sort.SliceStable(
		chars,
		func(i, j int) bool { return chars[i].Sort < chars[j].Sort },
	)
	var cast, crew []CastMember
	for _, c := range chars {
		if c.PersonName == "" || strings.EqualFold(c.PeopleType, skipType) {
			continue
		}
		member := CastMember{
			TVDBID:     c.PeopleID,
			Name:       c.PersonName,
			ProfileURL: TVDBArtworkURL(c.PersonImg),
		}
		if c.PeopleID != 0 {
			member.PersonURL = fmt.Sprintf(
				"https://www.thetvdb.com/dereferrer/people/%d", c.PeopleID)
		}
		switch {
		case strings.EqualFold(c.PeopleType, castType):
			if len(cast) < maxCastMembers {
				member.Character = c.Name
				cast = append(cast, member)
			}
		case len(crew) < maxCrewMembers:
			member.Job = c.PeopleType
			member.Department = tvdbDepartments[c.PeopleType]
			crew = append(crew, member)
		}
	}
	return append(cast, crew...)
}

// GetPerson returns the biographical record for a TVDB people id.
//
// TVDB carries no known-for department, so PersonDetails.KnownFor is left empty
// rather than guessed from the person's credits. Social handles are only ever
// present as remoteIds entries on this same record — most people carry an IMDB
// id and nothing else — and no extra request is made hunting for them.
func (t *TVDB) GetPerson(
	ctx context.Context,
	tvdbID uint32,
) (*PersonDetails, error) {
	ctx, span := tracer.Start(ctx, "metadata.tvdb.get_person",
		trace.WithAttributes(attribute.Int("tvdb.id", int(tvdbID))))
	defer span.End()

	var ext struct {
		Data struct {
			ID          uint32          `json:"id"`
			Name        string          `json:"name"`
			Image       string          `json:"image"`
			Birth       string          `json:"birth"`
			Death       string          `json:"death"`
			BirthPlace  string          `json:"birthPlace"`
			Biographies []tvdbBiography `json:"biographies"`
			RemoteIDs   []struct {
				ID         string `json:"id"`
				SourceName string `json:"sourceName"`
			} `json:"remoteIds"`
		} `json:"data"`
	}
	if err := t.get(
		ctx,
		fmt.Sprintf("/people/%d/extended", tvdbID),
		&ext,
	); err != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("tvdb get person: %w", err),
		)
	}

	d := &PersonDetails{
		Biography:    pickBiography(ext.Data.Biographies, t.language),
		Birthday:     ext.Data.Birth,
		Deathday:     ext.Data.Death,
		PlaceOfBirth: ext.Data.BirthPlace,
		ProfileURL:   TVDBArtworkURL(ext.Data.Image),
	}
	for _, r := range ext.Data.RemoteIDs {
		switch strings.ToLower(r.SourceName) {
		case "imdb":
			d.IMDbID = r.ID
		case "instagram":
			d.InstagramID = r.ID
		case "twitter", "x":
			d.TwitterID = r.ID
		}
	}
	return d, nil
}

// pickBiography chooses one entry out of TVDB's per-language biographies array:
// the configured language, then English, then whatever TVDB listed first.
//
// TVDB marks no entry as the default, and the array is frequently English-only,
// so without the English leg a French install shows an empty biography for most
// of the catalogue. The first-entry leg covers the people TVDB holds a bio for
// in neither.
func pickBiography(bios []tvdbBiography, lang string) string {
	var english string
	for _, b := range bios {
		if b.Biography == "" {
			continue
		}
		if lang != "" && strings.EqualFold(b.Language, lang) {
			return b.Biography
		}
		if english == "" && strings.EqualFold(b.Language, "eng") {
			english = b.Biography
		}
	}
	if english != "" {
		return english
	}
	for _, b := range bios {
		if b.Biography != "" {
			return b.Biography
		}
	}
	return ""
}

type tvdbBiography struct {
	Biography string `json:"biography"`
	Language  string `json:"language"`
}

// tvdbSeasonRef is what translateSeasons needs of a season from the extended
// series record: its id, and the languages TVDB lists for its name.
type tvdbSeasonRef struct {
	id        uint64
	nameLangs []string
}

// translateSeasons replaces each season's name with the configured language's,
// in place. seasons and refs are index-aligned.
//
// TVDB offers no bulk form: the extended series record carries only the
// original-language season name, and its ?meta=translations covers the
// series, not its seasons, so a translated name costs one request per season.
// Left untranslated, an anime's arc names stay Japanese on a French install
// while everything around them is translated.
//
// The extended record does list, per season, the languages its name is
// translated into (nameTranslations), so only a season listing the configured
// language is asked for. Most seasons have no name in any language and list
// none: asking for every one cost a request and a 404 per season of every
// show on every refresh. A season whose record leaves the field out is asked
// for anyway, since its absence says nothing.
//
// A miss is still ordinary: TVDB can answer 404 for a language it lists, and a
// record it does hold can carry a null name (an overview-only translation).
// Both keep the original name, which is why the error is dropped rather than
// surfaced — one season without a translation must not fail a metadata
// refresh. A rate limit ends the pass: every later request would be refused
// too.
func (t *TVDB) translateSeasons(
	ctx context.Context,
	seasons []SeasonInfo,
	refs []tvdbSeasonRef,
) {
	for i, ref := range refs {
		if i >= len(seasons) || ref.id == 0 {
			continue
		}
		if ref.nameLangs != nil && !listsLanguage(ref.nameLangs, t.language) {
			continue
		}
		var tr struct {
			Data struct {
				Name string `json:"name"`
			} `json:"data"`
		}
		path := fmt.Sprintf("/seasons/%d/translations/%s", ref.id, t.language)
		if err := t.get(ctx, path, &tr); err != nil {
			// Debug, because the ordinary miss above is not worth a line per
			// season. But it must leave *some* trace: a TVDB outage or an
			// expired token silently reverts every season of every show to
			// its original-language name through this exact path, and with
			// nothing recorded at all the two are indistinguishable.
			slog.DebugContext(ctx, "tvdb season translation unavailable",
				"tvdb.season_id", ref.id, "error", err)
			if errors.Is(err, ErrRateLimited) {
				return
			}
			continue
		}
		if tr.Data.Name != "" {
			seasons[i].Name = tr.Data.Name
		}
	}
}

// listsLanguage reports whether a TVDB nameTranslations list names lang. TVDB
// sends it either one code per element or as one comma-joined string
// (["eng,deu,spa,fra"]), so each element is split.
func listsLanguage(langs []string, lang string) bool {
	for _, l := range langs {
		for code := range strings.SplitSeq(l, ",") {
			if strings.EqualFold(strings.TrimSpace(code), lang) {
				return true
			}
		}
	}
	return false
}

func normalizeStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "continuing", "returning series":
		return "continuing"
	case "ended", "canceled", "cancelled":
		return "ended"
	default:
		// "Upcoming", "Planned" and "In Production" land here with everything
		// else TVDB might send. A series that has not premiered is still a
		// series that is coming, which is what "continuing" has to mean with
		// only two values — and the library only ever holds shows someone
		// chose to follow, so a third value would buy nothing: the facet it
		// fed would read 0 forever while staying clickable, which is
		// indistinguishable from a filter that does not work.
		return "continuing"
	}
}

func parseAirDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}

// animeOriginLanguages/Countries are the TVDB origin codes that, combined with
// an animation genre, mark a show as anime.
var (
	animeOriginLanguages = map[string]bool{"jpn": true, "ja": true, "jp": true}
	animeOriginCountries = map[string]bool{"jpn": true, "jp": true}
)

// inferAnime decides SeriesAnime from a show's TVDB metadata. The literal
// "anime" genre alone missed most of the catalogue — TVDB tags Drifters
// "Comedy" and Nanana's Buried Treasure "Animation" — and the type drives
// absolute-number episode matching, so a miss silently mis-matches files.
// Animation plus a Japanese origin is the signal TVDB actually carries.
func inferAnime(genres []string, originalLanguage, originalCountry string) bool {
	animated := false
	for _, g := range genres {
		switch strings.ToLower(g) {
		case "anime":
			return true
		case "animation", "animated":
			animated = true
		}
	}
	return animated &&
		(animeOriginLanguages[strings.ToLower(originalLanguage)] ||
			animeOriginCountries[strings.ToLower(originalCountry)])
}
