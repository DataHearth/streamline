package metadata

import "regexp"

// Source names of the external_ids table: the values of ent's
// externalid.Source enum, which a new one is added to as well.
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
)

// mbURLPattern reads one site's id out of a MusicBrainz url relation. The
// value kept is what the site's own address is built from: a number, a
// Spotify base62 id, an AllMusic "mn…"/"mw…" id, or a path where the site
// has no id (Last.fm, RateYourMusic, Bandcamp).
type mbURLPattern struct {
	source string
	re     *regexp.Regexp
}

// The three entity kinds link different pages. An artist's Discogs page is
// /artist/, an album's the master release; Spotify, Deezer and Bandcamp link
// a release, never a release group, so an album reads those from its
// canonical release. The artist's Wikidata and Deezer ids are not here:
// they have their own columns (overview and photo) and readURLRel.
var (
	mbArtistURLs = []mbURLPattern{
		{SourceDiscogs, regexp.MustCompile(`^https?://(?:www\.)?discogs\.com/artist/(\d+)`)},
		{SourceAllMusic, regexp.MustCompile(`^https?://(?:www\.)?allmusic\.com/artist/(?:[^/?#]*-)?(mn\d+)`)},
		{SourceSpotify, regexp.MustCompile(`^https?://open\.spotify\.com/artist/([A-Za-z0-9]+)`)},
		{SourceBandcamp, regexp.MustCompile(`^https?://([a-z0-9-]+\.bandcamp\.com)/?$`)},
		{SourceLastFM, regexp.MustCompile(`^https?://(?:www\.)?last\.fm/music/([^/?#]+)/?$`)},
		{SourceRateYourMusic, regexp.MustCompile(`^https?://(?:www\.)?rateyourmusic\.com/artist/([^/?#]+)/?$`)},
	}
	mbReleaseGroupURLs = []mbURLPattern{
		{SourceWikidata, regexp.MustCompile(`^https?://(?:www\.)?wikidata\.org/wiki/(Q\d+)$`)},
		{SourceDiscogs, regexp.MustCompile(`^https?://(?:www\.)?discogs\.com/master/(\d+)`)},
		{SourceAllMusic, regexp.MustCompile(`^https?://(?:www\.)?allmusic\.com/album/(?:[^/?#]*-)?(mw\d+)`)},
		{SourceRateYourMusic, regexp.MustCompile(`^https?://(?:www\.)?rateyourmusic\.com/release/([^?#]+?)/?$`)},
	}
	mbReleaseURLs = []mbURLPattern{
		{SourceSpotify, regexp.MustCompile(`^https?://open\.spotify\.com/album/([A-Za-z0-9]+)`)},
		{SourceDeezer, regexp.MustCompile(`^https?://(?:www\.)?deezer\.com/(?:[a-z]{2}/)?album/(\d+)`)},
		{SourceBandcamp, regexp.MustCompile(`^https?://([a-z0-9-]+\.bandcamp\.com/album/[^/?#]+)`)},
	}
)

// mbURLIDs reads each site's id from an entity's url relations into out, the
// first match per site winning: MusicBrainz lists an entity's own page before
// a mirror or a regional duplicate far more often than after it. A source
// already in out is left alone, so a release group's links are not replaced
// by its release's.
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
