package library

import (
	"slices"
	"strings"
	"unicode"

	"github.com/datahearth/streamline/ent"
)

// ArtistNames is every name a release may credit the artist under: its name,
// its MusicBrainz sort name read forwards, then its aliases. A sort name is
// written in Latin script whatever the name's own, so 星野源's "Hoshino, Gen"
// is the "Gen Hoshino" a release outside Japan carries.
func ArtistNames(a *ent.Artist) []string {
	names := []string{a.Name}
	sort := a.SortName
	// A name holding a comma of its own ("Earth, Wind & Fire") is its own
	// sort name, and is not a "Last, First".
	if last, first, ok := strings.Cut(sort, ", "); ok && sort != a.Name &&
		!strings.Contains(first, ",") {
		sort = first + " " + last
	}
	for _, n := range append([]string{sort}, a.Aliases...) {
		if n != "" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	return withCyrillicSpellings(names)
}

// TrackTitles is every title a file of the track may state: its own, then
// the ones its recording carries on the album's other-script releases.
func TrackTitles(t *ent.Track) []string {
	return append([]string{t.Title}, t.Aliases...)
}

// trackTitleIs is TitleMatchesStrict against any of the track's titles.
func trackTitleIs(t *ent.Track, title string) bool {
	return slices.ContainsFunc(TrackTitles(t), func(own string) bool {
		return TitleMatchesStrict(own, title)
	})
}

// TrackTitleAgrees is TitlePrefixMatches against any of the track's titles.
func TrackTitleAgrees(t *ent.Track, title string) bool {
	return slices.ContainsFunc(TrackTitles(t), func(own string) bool {
		return TitlePrefixMatches(own, title)
	})
}

// AlbumTitles is every title a release of the album may carry: its own, then
// the aliases MusicBrainz lists for it.
func AlbumTitles(a *ent.Album) []string {
	titles := []string{a.Title}
	for _, t := range a.Aliases {
		if !slices.Contains(titles, t) {
			titles = append(titles, t)
		}
	}
	return withCyrillicSpellings(titles)
}

// withCyrillicSpellings adds, after the names, the other Latin spellings of
// each one written in Cyrillic.
func withCyrillicSpellings(names []string) []string {
	for _, n := range slices.Clone(names) {
		for _, spelled := range cyrillicSpellings(n) {
			if !slices.Contains(names, spelled) {
				names = append(names, spelled)
			}
		}
	}
	return names
}

// LatinAlbumQuery is the artist name and album title to query indexers with
// beside the stored ones, when either is not written in Latin script and the
// library holds a Latin name for it: trackers outside the name's own country
// file "星野源" under "Gen Hoshino" and "Сказка" under "Skazka", and several
// drop the characters they cannot index and search on what is left. A part
// with no Latin name stays as it is.
func LatinAlbumQuery(artist *ent.Artist, a *ent.Album) (string, string, bool) {
	name := latinName(ArtistNames(artist))
	title := latinName(AlbumTitles(a))
	return name, title, name != artist.Name || title != a.Title
}

// latinName is names[0] when it is in Latin script, else the first name that
// is, one without diacritics before one with them: MusicBrainz lists the
// scholarly romanization ("Fàntèxī", "triviṣa-itihāsa") beside the name
// releases carry ("Fantasy", "Sandokushi"). With no Latin name, names[0].
func latinName(names []string) string {
	if latinScript(names[0]) {
		return names[0]
	}
	for _, plain := range []bool{true, false} {
		for _, n := range names {
			if latinScript(n) && (!plain || isASCII(n)) {
				return n
			}
		}
	}
	return names[0]
}

func isASCII(s string) bool {
	return strings.IndexFunc(
		s,
		func(r rune) bool { return r > unicode.MaxASCII },
	) < 0
}

func latinScript(s string) bool {
	latin := false
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		if !unicode.Is(unicode.Latin, r) {
			return false
		}
		latin = true
	}
	return latin
}

// AlbumTitleNamed is TitleNamesSameWork against any of the album's titles.
func AlbumTitleNamed(release string, a *ent.Album) bool {
	return slices.ContainsFunc(AlbumTitles(a), func(t string) bool {
		return TitleNamesSameWork(release, t)
	})
}

// ReleaseNamesAlbum is ReleaseNamesWork under any of the artist's names and
// any of the album's titles.
func ReleaseNamesAlbum(name string, artist *ent.Artist, a *ent.Album) bool {
	for _, c := range ArtistNames(artist) {
		for _, t := range AlbumTitles(a) {
			if ReleaseNamesWork(name, c, t) {
				return true
			}
		}
	}
	return false
}

// ReleaseNamedByArtist is ReleaseNamedByCreator under any of the artist's
// names.
func ReleaseNamedByArtist(name string, artist *ent.Artist) bool {
	return slices.ContainsFunc(ArtistNames(artist), func(c string) bool {
		return ReleaseNamedByCreator(name, c)
	})
}

// ReleaseOpensOnArtist is ReleaseOpensOnCreator under any of the artist's
// names.
func ReleaseOpensOnArtist(name string, artist *ent.Artist) bool {
	return slices.ContainsFunc(ArtistNames(artist), func(c string) bool {
		return ReleaseOpensOnCreator(name, c)
	})
}
