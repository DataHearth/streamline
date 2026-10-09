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
	// TMDBID and TVDBID are each scoped to their own provider's id space, so
	// neither is globally unique on its own; exactly one is set depending on
	// which provider supplied the entry.
	TMDBID     uint32
	TVDBID     uint32
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
	Rating           float32
	VoteCount        uint32
	Cast             []CastMember
	Tagline          string
	ReleaseDate      string // ISO yyyy-mm-dd, as TMDB returns it
	OriginalLanguage string // ISO 639-1
	// Aliases are TMDB's translated and alternative titles, excluding Title and
	// OriginalTitle. A film held under its localized title equals none of the
	// releases named in another language, so matchers compare against these as
	// well — the movie counterpart of TVResult.Aliases.
	Aliases []string
}

// PersonDetails is one person's biographical record, as either provider
// supplies it. Fields a provider does not carry stay empty rather than being
// synthesised — TVDB has no equivalent of KnownFor, and neither provider
// guarantees a biography, a death date or a birthplace.
type PersonDetails struct {
	Biography string
	// KnownFor is TMDB's known_for_department ("Acting", "Directing", …).
	// Always empty for TVDB.
	KnownFor     string
	Birthday     string // ISO yyyy-mm-dd, empty when unknown
	Deathday     string // ISO yyyy-mm-dd, empty when alive or unknown
	PlaceOfBirth string
	ProfileURL   string
	IMDbID       string
	InstagramID  string
	TwitterID    string
}

type Provider interface {
	SearchMovie(
		ctx context.Context,
		query string,
		year uint16,
	) ([]MovieResult, error)
	GetMovie(ctx context.Context, tmdbID uint32) (*MovieDetails, error)
	// GetPerson returns the biographical record behind a movie cast entry's
	// TMDBID.
	GetPerson(ctx context.Context, tmdbID uint32) (*PersonDetails, error)
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

// CoverArtURL returns the Cover Art Archive front-cover URL for a
// release-group. CAA redirects to the image; 404 means no art exists.
func CoverArtURL(releaseGroupMBID string) string {
	if releaseGroupMBID == "" {
		return ""
	}
	return "https://coverartarchive.org/release-group/" + releaseGroupMBID + "/front-500"
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
	// Aliases are TVDB's alternate names for the show. A folder named in romaji
	// ("Kimetsu no Yaiba", "Shingeki no Kyojin") only ever equals the entry
	// through one of these, so matchers compare against them as well as Title.
	Aliases []string
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
	Status     string // "continuing" | "ended"
	Type       SeriesType
	Creator    string
	Runtime    uint16
	Rating     float32
	Genres     []string
	Cast       []CastMember
	Seasons    []SeasonInfo
	Episodes   []EpisodeInfo
	FirstAired string // ISO yyyy-mm-dd, as TVDB returns it
}

// TVProvider fetches TV-series metadata. Implemented by *TVDB.
type TVProvider interface {
	SearchSeries(ctx context.Context, query string) ([]TVResult, error)
	GetSeries(ctx context.Context, tvdbID uint32) (*TVDetails, error)
	// GetSeriesCast returns top-billed actors for a series. Cheaper than
	// GetSeries: one extended-record fetch, no episode pagination.
	GetSeriesCast(ctx context.Context, tvdbID uint32) ([]CastMember, error)
	// GetPerson returns the biographical record behind a series cast entry's
	// TVDBID.
	GetPerson(ctx context.Context, tvdbID uint32) (*PersonDetails, error)
}

// ArtistResult is a single MusicBrainz artist search hit.
type ArtistResult struct {
	MBID           string
	Name           string
	SortName       string
	Disambiguation string
	Score          uint8
}

// AlbumType mirrors the Album schema enum values.
type AlbumType string

const (
	AlbumTypeAlbum       AlbumType = "album"
	AlbumTypeEP          AlbumType = "ep"
	AlbumTypeSingle      AlbumType = "single"
	AlbumTypeCompilation AlbumType = "compilation"
	AlbumTypeLive        AlbumType = "live"
	AlbumTypeOther       AlbumType = "other"
)

// ReleaseGroupInfo is one release-group in an artist's discography.
type ReleaseGroupInfo struct {
	MBID        string
	Title       string
	Type        AlbumType
	ReleaseDate *time.Time // nil when MusicBrainz has no first-release-date
}

// ReleaseGroupSearchResult is a MusicBrainz release-group search hit with its
// credited artist, used to match scanned album folders.
type ReleaseGroupSearchResult struct {
	ReleaseGroupInfo
	ArtistMBID string
	ArtistName string
	Score      uint8
}

// TrackInfo is one track of the canonical release picked for a release-group.
type TrackInfo struct {
	MBID     string // recording MBID
	Title    string
	Disc     uint8
	Position uint16
	Duration uint32 // seconds, 0 when unknown
}

// ReleaseGroupDetails carries the canonical release pick and its track list.
type ReleaseGroupDetails struct {
	ReleaseGroupInfo
	ArtistMBID  string
	ReleaseMBID string
	// Barcode is the canonical release's UPC/EAN, empty when MusicBrainz has none.
	Barcode string
	Tracks  []TrackInfo
}

// CoverHit is one cover-art search result.
type CoverHit struct {
	ArtistName string
	CoverURL   string
}

// CoverProvider finds album artwork outside MusicBrainz's own archive.
// Implemented by *Deezer.
type CoverProvider interface {
	// CoverByUPC returns the cover URL of the album carrying the barcode, ""
	// when the provider has none.
	CoverByUPC(ctx context.Context, barcode string) (string, error)
	// SearchCover returns the provider's first hit for the artist and album,
	// nil when there is none. Matching the hit's artist is the caller's job.
	SearchCover(ctx context.Context, artist, album string) (*CoverHit, error)
}

// ArtistDetails is the full artist record used to seed an artist and its albums.
type ArtistDetails struct {
	ArtistResult
	Overview      string
	ReleaseGroups []ReleaseGroupInfo
}

// MusicProvider fetches music metadata. Implemented by *MusicBrainz.
type MusicProvider interface {
	SearchArtists(ctx context.Context, query string) ([]ArtistResult, error)
	// GetArtist returns the artist plus its full release-group listing
	// (browse-paginated internally).
	GetArtist(ctx context.Context, mbid string) (*ArtistDetails, error)
	// GetReleaseGroup picks the canonical release (earliest official) and
	// returns its track list.
	GetReleaseGroup(ctx context.Context, mbid string) (*ReleaseGroupDetails, error)
	SearchReleaseGroups(
		ctx context.Context,
		artist, album string,
	) ([]ReleaseGroupSearchResult, error)
}
