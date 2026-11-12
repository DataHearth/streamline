package metadata

import "regexp"

// Source names, the values of ent's externalid.Source enum.
const (
	SourceIMDb          = "imdb"
	SourceTMDB          = "tmdb"
	SourceWikidata      = "wikidata"
	SourceDiscogs       = "discogs"
	SourceAllMusic      = "allmusic"
	SourceSpotify       = "spotify"
	SourceDeezer        = "deezer"
	SourceBandcamp      = "bandcamp"
	SourceLastFM        = "lastfm"
	SourceRateYourMusic = "rateyourmusic"
	SourceGoodreads     = "goodreads"
	SourceOpenLibrary   = "openlibrary"
	SourceGoogleBooks   = "google_books"
	SourceAudible       = "audible"
)

// The providers' own ids as the API's external_ids map names them: wire
// only, never stored.
const (
	SourceTVDB        = "tvdb"
	SourceMusicBrainz = "musicbrainz"
	SourceHardcover   = "hardcover"
)

// mbURLPattern reads one site's id out of a MusicBrainz url relation.
type mbURLPattern struct {
	source string
	re     *regexp.Regexp
}

// Each entity kind links its own pages: an album reads its streaming links
// from its release. An artist's Wikidata and Deezer ids are columns.
var (
	mbArtistURLs = []mbURLPattern{
		{
			SourceDiscogs,
			regexp.MustCompile(`^https?://(?:www\.)?discogs\.com/artist/(\d+)`),
		},
		{
			SourceAllMusic,
			regexp.MustCompile(
				`^https?://(?:www\.)?allmusic\.com/artist/(?:[^/?#]*-)?(mn\d+)`,
			),
		},
		{
			SourceSpotify,
			regexp.MustCompile(`^https?://open\.spotify\.com/artist/([A-Za-z0-9]+)`),
		},
		{
			SourceBandcamp,
			regexp.MustCompile(`^https?://([a-z0-9-]+\.bandcamp\.com)/?$`),
		},
		{
			SourceLastFM,
			regexp.MustCompile(`^https?://(?:www\.)?last\.fm/music/([^/?#]+)/?$`),
		},
		{
			SourceRateYourMusic,
			regexp.MustCompile(
				`^https?://(?:www\.)?rateyourmusic\.com/artist/([^/?#]+)/?$`,
			),
		},
	}
	mbReleaseGroupURLs = []mbURLPattern{
		{
			SourceWikidata,
			regexp.MustCompile(`^https?://(?:www\.)?wikidata\.org/wiki/(Q\d+)$`),
		},
		{
			SourceDiscogs,
			regexp.MustCompile(`^https?://(?:www\.)?discogs\.com/master/(\d+)`),
		},
		{
			SourceAllMusic,
			regexp.MustCompile(
				`^https?://(?:www\.)?allmusic\.com/album/(?:[^/?#]*-)?(mw\d+)`,
			),
		},
		{
			SourceRateYourMusic,
			regexp.MustCompile(
				`^https?://(?:www\.)?rateyourmusic\.com/release/([^?#]+?)/?$`,
			),
		},
	}
	mbReleaseURLs = []mbURLPattern{
		{
			SourceSpotify,
			regexp.MustCompile(`^https?://open\.spotify\.com/album/([A-Za-z0-9]+)`),
		},
		{
			SourceDeezer,
			regexp.MustCompile(
				`^https?://(?:www\.)?deezer\.com/(?:[a-z]{2}/)?album/(\d+)`,
			),
		},
		{
			SourceBandcamp,
			regexp.MustCompile(
				`^https?://([a-z0-9-]+\.bandcamp\.com/album/[^/?#]+)`,
			),
		},
	}
)

// mbURLIDs adds each site's first id to out, keeping a source already set.
func mbURLIDs(out map[string]string, rels []mbRel, patterns []mbURLPattern) {
	for _, rel := range rels {
		if rel.URL == nil || rel.Ended {
			continue
		}
		for _, p := range patterns {
			if _, ok := out[p.source]; ok {
				continue
			}
			if m := p.re.FindStringSubmatch(rel.URL.Resource); m != nil {
				out[p.source] = m[1]
				break
			}
		}
	}
}
