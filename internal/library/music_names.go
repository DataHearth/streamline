package library

import (
	"slices"
	"strings"

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
	return names
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
	return titles
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

// ReleaseNamesOtherAlbum is ReleaseNamesOtherWork under every one of the
// artist's names and the album's titles: a release one pairing reads as the
// album, or cannot judge, is not another's.
func ReleaseNamesOtherAlbum(name string, artist *ent.Artist, a *ent.Album) bool {
	for _, c := range ArtistNames(artist) {
		for _, t := range AlbumTitles(a) {
			if !ReleaseNamesOtherWork(name, c, t) {
				return false
			}
		}
	}
	return true
}

// ReleaseNamedByArtist is ReleaseNamedByCreator under any of the artist's
// names.
func ReleaseNamedByArtist(name string, artist *ent.Artist) bool {
	return slices.ContainsFunc(ArtistNames(artist), func(c string) bool {
		return ReleaseNamedByCreator(name, c)
	})
}
