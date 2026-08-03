package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
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

const tvdbBaseURL = "https://api4.thetvdb.com/v4"

type TVDB struct {
	apiKey  string
	BaseURL string
	client  *http.Client

	// language is the ISO 639-3 code (e.g. "eng") TVDB expects, derived from
	// the BCP47 metadata.language. Empty leaves records in their original
	// language.
	language string

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
	}
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
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
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
	token, err := t.login(ctx)
	if err != nil {
		return err
	}
	resp, err := t.do(ctx, path, token)
	if err != nil {
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
			return err
		}
		if resp, err = t.do(ctx, path, token); err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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
	return json.NewDecoder(resp.Body).Decode(out)
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

func (t *TVDB) SearchSeries(ctx context.Context, query string) ([]TVResult, error) {
	ctx, span := tracer.Start(ctx, "metadata.tvdb.search_series",
		trace.WithAttributes(attribute.String("query", query)))
	defer span.End()

	var resp struct {
		Data []struct {
			TVDBID       string            `json:"tvdb_id"`
			Name         string            `json:"name"`
			Year         string            `json:"year"`
			Network      string            `json:"network"`
			Overview     string            `json:"overview"`
			ImageURL     string            `json:"image_url"`
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
		out = append(out, TVResult{
			TVDBID:     atou32(r.TVDBID),
			Title:      title,
			Year:       atou16(r.Year),
			Network:    r.Network,
			Overview:   overview,
			PosterPath: r.ImageURL,
		})
	}
	return out, nil
}

func (t *TVDB) GetSeries(ctx context.Context, tvdbID uint32) (*TVDetails, error) {
	ctx, span := tracer.Start(ctx, "metadata.tvdb.get_series",
		trace.WithAttributes(attribute.Int("tvdb.id", int(tvdbID))))
	defer span.End()

	var ext struct {
		Data struct {
			ID             uint32 `json:"id"`
			Name           string `json:"name"`
			Year           string `json:"year"`
			Overview       string `json:"overview"`
			AverageRuntime uint16 `json:"averageRuntime"`
			Image          string `json:"image"`
			Status         struct {
				Name string `json:"name"`
			} `json:"status"`
			Genres []struct {
				Name string `json:"name"`
			} `json:"genres"`
			LatestNetwork struct {
				Name string `json:"name"`
			} `json:"latestNetwork"`
			Seasons []struct {
				Number uint16 `json:"number"`
				Name   string `json:"name"`
				Type   struct {
					Type string `json:"type"`
				} `json:"type"`
			} `json:"seasons"`
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
		TVResult: TVResult{
			TVDBID:        ext.Data.ID,
			Title:         ext.Data.Name,
			OriginalTitle: ext.Data.Name, // preserved before the language override below
			Year:          atou16(ext.Data.Year),
			Network:       ext.Data.LatestNetwork.Name,
			Overview:      ext.Data.Overview,
			PosterPath:    ext.Data.Image,
		},
		Status:  normalizeStatus(ext.Data.Status.Name),
		Type:    SeriesStandard, // refined below if a genre marks it anime
		Runtime: ext.Data.AverageRuntime,
		// TVDB v4 removed user ratings; `score` is an arbitrary popularity
		// metric (not a 0-10 rating), so Rating is left unset (0 = unknown).
	}
	for _, g := range ext.Data.Genres {
		d.Genres = append(d.Genres, g.Name)
		if strings.EqualFold(g.Name, "anime") {
			d.Type = SeriesAnime
		}
	}
	for _, s := range ext.Data.Seasons {
		if s.Type.Type == "official" || s.Type.Type == "" {
			d.Seasons = append(d.Seasons, SeasonInfo{Number: s.Number, Name: s.Name})
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
	}

	// Episodes (paginated). The /{lang} variant returns translated episode
	// titles/overviews; episodes lacking a translation come back with empty
	// fields (TVDB has no per-episode fallback).
	// ponytail: no default-language fallback fetch — a missing episode
	// translation shows blank until TVDB fills it in.
	langSeg := ""
	if t.language != "" {
		langSeg = "/" + t.language
	}
	page := 0
	for {
		var epResp struct {
			Data struct {
				Episodes []struct {
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
			return nil, otelx.RecordSpanError(
				span,
				fmt.Errorf("tvdb episodes: %w", err),
			)
		}
		for _, e := range epResp.Data.Episodes {
			d.Episodes = append(d.Episodes, EpisodeInfo{
				SeasonNumber:   e.SeasonNumber,
				Number:         e.Number,
				AbsoluteNumber: e.AbsoluteNumber,
				Title:          e.Name,
				Overview:       e.Overview,
				AirDate:        parseAirDate(e.Aired),
			})
		}
		if epResp.Links.Next == nil || *epResp.Links.Next == "" {
			break
		}
		page++
	}

	return d, nil
}

// GetSeriesCast returns up to maxCastMembers actors for a series, ordered by
// TVDB's `sort`. Non-actor crew (directors, writers, …) is skipped.
func (t *TVDB) GetSeriesCast(
	ctx context.Context,
	tvdbID uint32,
) ([]CastMember, error) {
	ctx, span := tracer.Start(ctx, "metadata.tvdb.get_series_cast",
		trace.WithAttributes(attribute.Int("tvdb.id", int(tvdbID))))
	defer span.End()

	var ext struct {
		Data struct {
			Characters []struct {
				Name       string `json:"name"`       // character name
				PersonName string `json:"personName"` // actor's real name
				PersonImg  string `json:"personImgURL"`
				PeopleType string `json:"peopleType"`
				PeopleID   uint32 `json:"peopleId"`
				Sort       int    `json:"sort"`
			} `json:"characters"`
		} `json:"data"`
	}
	if err := t.get(
		ctx,
		fmt.Sprintf("/series/%d/extended", tvdbID),
		&ext,
	); err != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("tvdb series cast: %w", err),
		)
	}

	chars := ext.Data.Characters
	sort.SliceStable(
		chars,
		func(i, j int) bool { return chars[i].Sort < chars[j].Sort },
	)

	cast := make([]CastMember, 0, maxCastMembers)
	for _, c := range chars {
		if !strings.EqualFold(c.PeopleType, "Actor") || c.PersonName == "" {
			continue
		}
		member := CastMember{
			Name:       c.PersonName,
			Character:  c.Name,
			ProfileURL: TVDBArtworkURL(c.PersonImg),
		}
		if c.PeopleID != 0 {
			member.PersonURL = fmt.Sprintf(
				"https://www.thetvdb.com/dereferrer/people/%d", c.PeopleID)
		}
		cast = append(cast, member)
		if len(cast) >= maxCastMembers {
			break
		}
	}
	return cast, nil
}

func normalizeStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "continuing", "returning series":
		return "continuing"
	case "ended", "canceled", "cancelled":
		return "ended"
	case "upcoming", "planned", "in production":
		return "upcoming"
	default:
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
