package metadata

import (
	"context"
	"strings"
	"time"
)

type MovieResult struct {
	TMDBID        uint32
	Title         string
	OriginalTitle string
	Year          uint16
	Overview      string
	PosterPath    string
}

type CastMember struct {
	TMDBID     uint32
	Name       string
	Character  string
	ProfileURL string
	// PersonURL links to the person's page on the source provider. Empty for
	// TMDB cast (the URL is derived from TMDBID); set directly for TVDB cast.
	PersonURL string
}

type MovieDetails struct {
	MovieResult
	Genres  []string
	Runtime uint16
	// Rating is TMDB's vote average (0–10). Zero when TMDB has no votes.
	Rating float32
	Cast   []CastMember
}

type Provider interface {
	SearchMovie(
		ctx context.Context,
		query string,
		year uint16,
	) ([]MovieResult, error)
	GetMovie(ctx context.Context, tmdbID uint32) (*MovieDetails, error)
	// Recommendations returns TMDB's "recommended" movies for the given
	// title, capped and ordered as TMDB returns them.
	Recommendations(ctx context.Context, tmdbID uint32) ([]MovieResult, error)
	// FetchDigitalRelease returns the earliest digital-type (TMDB type 4)
	// release date for the given region, or (nil, nil) when none is published.
	FetchDigitalRelease(
		ctx context.Context,
		tmdbID uint32,
		region string,
	) (*time.Time, error)
}

func PosterURL(path, size string) string {
	if path == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/" + size + path
}

// TVDBArtworkURL returns an absolute URL for a TVDB image reference. TVDB image
// fields are usually already absolute; relative paths are prefixed with the
// TVDB artwork host. Empty in, empty out.
func TVDBArtworkURL(p string) string {
	if p == "" || strings.HasPrefix(p, "http") {
		return p
	}
	return "https://artworks.thetvdb.com" + p
}

// TVResult is a single TVDB search hit.
type TVResult struct {
	TVDBID        uint32
	Title         string
	OriginalTitle string // untranslated TVDB name; blank when no language override
	Year          uint16
	Network       string
	Overview      string
	PosterPath    string
}

// EpisodeInfo is one episode as TVDB reports it.
type EpisodeInfo struct {
	SeasonNumber   uint16
	Number         uint16
	AbsoluteNumber uint16
	Title          string
	Overview       string
	AirDate        *time.Time // nil when TVDB has no date (unaired/unknown)
}

// SeasonInfo is a season summary (episodes are carried flat on TVDetails).
type SeasonInfo struct {
	Number uint16
	Name   string
}

// SeriesType mirrors the schema enum values.
type SeriesType string

const (
	SeriesStandard SeriesType = "standard"
	SeriesAnime    SeriesType = "anime"
	SeriesDaily    SeriesType = "daily"
)

// TVDetails is the full TVDB record used to seed a show + its seasons/episodes.
type TVDetails struct {
	TVResult
	Status   string // "continuing" | "ended" | "upcoming"
	Type     SeriesType
	Creator  string
	Runtime  uint16
	Rating   float32
	Genres   []string
	Cast     []CastMember
	Seasons  []SeasonInfo
	Episodes []EpisodeInfo
}

// TVProvider fetches TV-series metadata. Implemented by *TVDB.
type TVProvider interface {
	SearchSeries(ctx context.Context, query string) ([]TVResult, error)
	GetSeries(ctx context.Context, tvdbID uint32) (*TVDetails, error)
	// GetSeriesCast returns top-billed actors for a series. Cheaper than
	// GetSeries: one extended-record fetch, no episode pagination.
	GetSeriesCast(ctx context.Context, tvdbID uint32) ([]CastMember, error)
}
