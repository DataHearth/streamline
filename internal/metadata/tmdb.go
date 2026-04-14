package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const tmdbBaseURL = "https://api.themoviedb.org"

// maxCastMembers caps how many top-billed cast entries we surface per movie.
const maxCastMembers = 15

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/metadata")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/metadata")

	tmdbRequests metric.Int64Counter
	tmdbDuration metric.Float64Histogram
)

func init() {
	tmdbRequests = otelx.Must(meter.Int64Counter(
		"streamline.metadata.tmdb.requests",
		metric.WithDescription("TMDB API requests by endpoint and outcome"),
	))
	tmdbDuration = otelx.Must(meter.Float64Histogram(
		"streamline.metadata.tmdb.duration",
		metric.WithDescription("TMDB API request duration"),
		metric.WithUnit("s"),
	))

	ctx := context.Background()
	tmdbRequests.Add(ctx, 0)
	tmdbDuration.Record(ctx, 0)
}

type TMDB struct {
	apiKey   string
	language string
	BaseURL  string
	client   *http.Client
}

// NewTMDB builds a TMDB client using the api key and language from the
// config singleton (metadata.tmdb_api_key, metadata.language). An empty
// metadata.language leaves the language param off requests, letting TMDB
// fall back to its provider default.
func NewTMDB() *TMDB {
	m := config.Get().Metadata
	return &TMDB{
		apiKey:   config.SecretValue(m.TMDBAPIKey, m.TMDBAPIKeyFile),
		language: m.Language,
		BaseURL:  tmdbBaseURL,
		client:   otelx.HTTPClient,
	}
}

func (t *TMDB) SearchMovie(
	ctx context.Context,
	query string,
	year uint16,
) ([]MovieResult, error) {
	ctx, span := tracer.Start(ctx, "metadata.tmdb.search_movie",
		trace.WithAttributes(
			attribute.String("query", query),
			attribute.Int("year", int(year)),
		),
	)
	defer span.End()

	start := time.Now()
	outcome := "success"
	defer func() {
		tmdbDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
			attribute.String("endpoint", "search_movie"),
		))
		tmdbRequests.Add(ctx, 1, metric.WithAttributes(
			attribute.String("endpoint", "search_movie"),
			attribute.String("outcome", outcome),
		))
	}()

	params := url.Values{"query": {query}}
	if year > 0 {
		params.Set("year", strconv.FormatUint(uint64(year), 10))
	}
	params = t.withLang(params, t.language)

	var resp tmdbSearchResponse
	if err := t.get(ctx, "/3/search/movie", params, &resp); err != nil {
		outcome = "error"
		return nil, otelx.RecordSpanError(span, fmt.Errorf("tmdb search: %w", err))
	}

	results := make([]MovieResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		title := r.Title
		if title == "" {
			title = r.OriginalTitle
		}
		originalTitle := r.OriginalTitle
		if originalTitle == "" {
			originalTitle = title
		}
		results = append(results, MovieResult{
			TMDBID:        r.ID,
			Title:         title,
			OriginalTitle: originalTitle,
			Year:          extractYear(r.ReleaseDate),
			Overview:      r.Overview,
			PosterPath:    r.PosterPath,
		})
	}
	span.SetAttributes(attribute.Int("results.count", len(results)))

	slog.DebugContext(ctx, "tmdb search", "query", query, "results", len(results))
	return results, nil
}

func (t *TMDB) GetMovie(ctx context.Context, tmdbID uint32) (*MovieDetails, error) {
	ctx, span := tracer.Start(ctx, "metadata.tmdb.get_movie",
		trace.WithAttributes(attribute.Int64("tmdb.id", int64(tmdbID))),
	)
	defer span.End()

	start := time.Now()
	outcome := "success"
	defer func() {
		tmdbDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
			attribute.String("endpoint", "get_movie"),
		))
		tmdbRequests.Add(ctx, 1, metric.WithAttributes(
			attribute.String("endpoint", "get_movie"),
			attribute.String("outcome", outcome),
		))
	}()

	params := url.Values{"append_to_response": {"translations,credits"}}
	params = t.withLang(params, t.language)

	var resp tmdbMovieResponse
	if err := t.get(
		ctx,
		fmt.Sprintf("/3/movie/%d", tmdbID),
		params,
		&resp,
	); err != nil {
		outcome = "error"
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("tmdb get movie: %w", err),
		)
	}

	title, overview := resp.Title, resp.Overview
	if (title == "" || overview == "") && resp.OriginalLanguage != "" {
		if orig, ok := findTranslation(
			resp.Translations.Translations,
			resp.OriginalLanguage,
		); ok {
			if title == "" {
				title = orig.Data.Title
			}
			if overview == "" {
				overview = orig.Data.Overview
			}
		}
	}
	if title == "" {
		title = resp.OriginalTitle
	}

	genres := make([]string, 0, len(resp.Genres))
	for _, g := range resp.Genres {
		genres = append(genres, g.Name)
	}

	entries := resp.Credits.Cast
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Order < entries[j].Order
	})
	if len(entries) > maxCastMembers {
		entries = entries[:maxCastMembers]
	}
	cast := make([]CastMember, 0, len(entries))
	for _, c := range entries {
		cast = append(cast, CastMember{
			TMDBID:     c.ID,
			Name:       c.Name,
			Character:  c.Character,
			ProfileURL: PosterURL(c.ProfilePath, "w185"),
		})
	}

	originalTitle := resp.OriginalTitle
	if originalTitle == "" {
		originalTitle = title
	}

	return &MovieDetails{
		MovieResult: MovieResult{
			TMDBID:        resp.ID,
			Title:         title,
			OriginalTitle: originalTitle,
			Year:          extractYear(resp.ReleaseDate),
			Overview:      overview,
			PosterPath:    resp.PosterPath,
		},
		Genres:  genres,
		Runtime: uint16(resp.Runtime),
		Rating:  resp.VoteAverage,
		Cast:    cast,
	}, nil
}

// Recommendations returns TMDB's "recommended" movies for the given title.
// The response shares the search-result shape, so it maps through the same
// MovieResult projection.
func (t *TMDB) Recommendations(
	ctx context.Context,
	tmdbID uint32,
) ([]MovieResult, error) {
	ctx, span := tracer.Start(ctx, "metadata.tmdb.recommendations",
		trace.WithAttributes(attribute.Int64("tmdb.id", int64(tmdbID))),
	)
	defer span.End()

	start := time.Now()
	outcome := "success"
	defer func() {
		tmdbDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
			attribute.String("endpoint", "recommendations"),
		))
		tmdbRequests.Add(ctx, 1, metric.WithAttributes(
			attribute.String("endpoint", "recommendations"),
			attribute.String("outcome", outcome),
		))
	}()

	params := t.withLang(nil, t.language)

	var resp tmdbSearchResponse
	if err := t.get(
		ctx,
		fmt.Sprintf("/3/movie/%d/recommendations", tmdbID),
		params,
		&resp,
	); err != nil {
		outcome = "error"
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("tmdb recommendations: %w", err),
		)
	}

	results := make([]MovieResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		title := r.Title
		if title == "" {
			title = r.OriginalTitle
		}
		originalTitle := r.OriginalTitle
		if originalTitle == "" {
			originalTitle = title
		}
		results = append(results, MovieResult{
			TMDBID:        r.ID,
			Title:         title,
			OriginalTitle: originalTitle,
			Year:          extractYear(r.ReleaseDate),
			Overview:      r.Overview,
			PosterPath:    r.PosterPath,
		})
	}
	span.SetAttributes(attribute.Int("results.count", len(results)))

	slog.DebugContext(ctx, "tmdb recommendations",
		"tmdb.id", tmdbID, "results", len(results))
	return results, nil
}

// FetchDigitalRelease returns the earliest digital-type (TMDB type 4)
// release date for the given region, or (nil, nil) when none is published.
func (t *TMDB) FetchDigitalRelease(
	ctx context.Context,
	tmdbID uint32,
	region string,
) (*time.Time, error) {
	ctx, span := tracer.Start(ctx, "metadata.tmdb.fetch_digital_release",
		trace.WithAttributes(
			attribute.Int64("tmdb.id", int64(tmdbID)),
			attribute.String("region", region),
		),
	)
	defer span.End()

	start := time.Now()
	outcome := "success"
	defer func() {
		tmdbDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
			attribute.String("endpoint", "release_dates"),
		))
		tmdbRequests.Add(ctx, 1, metric.WithAttributes(
			attribute.String("endpoint", "release_dates"),
			attribute.String("outcome", outcome),
		))
	}()

	var resp tmdbReleaseDatesResponse
	if err := t.get(
		ctx,
		fmt.Sprintf("/3/movie/%d/release_dates", tmdbID),
		nil,
		&resp,
	); err != nil {
		outcome = "error"
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("tmdb release_dates: %w", err),
		)
	}

	region = strings.ToUpper(region)
	for _, r := range resp.Results {
		if strings.ToUpper(r.ISO31661) != region {
			continue
		}
		var earliest *time.Time
		for _, d := range r.ReleaseDates {
			if d.Type != 4 || d.ReleaseDate == "" {
				continue
			}
			parsed, err := time.Parse(time.RFC3339, d.ReleaseDate)
			if err != nil {
				continue
			}
			if earliest == nil || parsed.Before(*earliest) {
				p := parsed
				earliest = &p
			}
		}
		return earliest, nil
	}
	return nil, nil
}

func findTranslation(ts []tmdbTranslation, lang string) (tmdbTranslation, bool) {
	for _, t := range ts {
		if t.ISO639_1 == lang {
			return t, true
		}
	}
	return tmdbTranslation{}, false
}

func (t *TMDB) get(
	ctx context.Context,
	path string,
	params url.Values,
	out any,
) error {
	u := t.BaseURL + path
	if params != nil {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+t.apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		slog.WarnContext(
			ctx,
			"tmdb request transport error",
			"tmdb.endpoint",
			path,
			"error",
			err,
		)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.WarnContext(
			ctx,
			"tmdb request non-200",
			"tmdb.endpoint",
			path,
			"http.status_code",
			resp.StatusCode,
		)
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func (t *TMDB) withLang(p url.Values, lang string) url.Values {
	if p == nil {
		p = url.Values{}
	}
	if lang == "" {
		return p
	}
	p.Set("language", lang)
	return p
}

func extractYear(releaseDate string) uint16 {
	if parts := strings.SplitN(releaseDate, "-", 2); len(parts) > 0 {
		y, _ := strconv.ParseUint(parts[0], 10, 16)
		return uint16(y)
	}
	return 0
}

type tmdbSearchResponse struct {
	Results []tmdbSearchResult `json:"results"`
}

type tmdbSearchResult struct {
	ID            uint32 `json:"id"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title"`
	ReleaseDate   string `json:"release_date"`
	Overview      string `json:"overview"`
	PosterPath    string `json:"poster_path"`
}

type tmdbMovieResponse struct {
	ID               uint32             `json:"id"`
	Title            string             `json:"title"`
	OriginalTitle    string             `json:"original_title"`
	OriginalLanguage string             `json:"original_language"`
	ReleaseDate      string             `json:"release_date"`
	Overview         string             `json:"overview"`
	PosterPath       string             `json:"poster_path"`
	Genres           []tmdbGenre        `json:"genres"`
	Runtime          int                `json:"runtime"`
	VoteAverage      float32            `json:"vote_average"`
	Translations     tmdbTranslationBag `json:"translations"`
	Credits          tmdbCredits        `json:"credits"`
}

type tmdbCredits struct {
	Cast []tmdbCastEntry `json:"cast"`
}

type tmdbCastEntry struct {
	ID          uint32 `json:"id"`
	Name        string `json:"name"`
	Character   string `json:"character"`
	ProfilePath string `json:"profile_path"`
	Order       int    `json:"order"`
}

type tmdbGenre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type tmdbTranslationBag struct {
	Translations []tmdbTranslation `json:"translations"`
}

type tmdbTranslation struct {
	ISO639_1 string              `json:"iso_639_1"`
	Data     tmdbTranslationData `json:"data"`
}

type tmdbTranslationData struct {
	Title    string `json:"title"`
	Overview string `json:"overview"`
}

type tmdbReleaseDatesResponse struct {
	Results []tmdbRegionReleaseDates `json:"results"`
}

type tmdbRegionReleaseDates struct {
	ISO31661     string                 `json:"iso_3166_1"`
	ReleaseDates []tmdbReleaseDateEntry `json:"release_dates"`
}

type tmdbReleaseDateEntry struct {
	Type        int    `json:"type"`
	ReleaseDate string `json:"release_date"`
}
