package library

import (
	"strconv"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/musiccredit"
	"github.com/datahearth/streamline/internal/library/audiotags"
)

// VariousArtistsMBID is MusicBrainz's special "Various Artists" artist, the
// album artist of a multi-artist compilation.
const VariousArtistsMBID = "89ad4ac3-39f7-470e-963a-56509c546377"

// albumMedia is Picard's spelling of each medium the album row stores.
var albumMedia = map[string]string{
	"cd":       "CD",
	"vinyl":    "Vinyl",
	"digital":  "Digital Media",
	"cassette": "Cassette",
}

// albumReleaseType is Picard's RELEASETYPE for each album type: the primary
// type, and "album; …" for the two the album type takes from a secondary one.
var albumReleaseType = map[album.Type]string{
	album.TypeAlbum:       "album",
	album.TypeEp:          "ep",
	album.TypeSingle:      "single",
	album.TypeCompilation: "album; compilation",
	album.TypeLive:        "album; live",
	album.TypeOther:       "other",
}

// AlbumTrackTags is every key Streamline owns for one track's file, from the
// database alone. Every key is present: one the database has nothing for is
// empty, which deletes it, so a value an earlier tagger left (a release
// group's label, a wrong genre) does not survive next to ours. al must carry
// its tracks (for the totals) and tr its credits (for the featured artists).
func AlbumTrackTags(ar *ent.Artist, al *ent.Album, tr *ent.Track) audiotags.Tags {
	var featured []*ent.MusicCredit
	for _, c := range tr.Edges.Credits {
		if c.Kind == musiccredit.KindFeaturing {
			featured = append(featured, c)
		}
	}

	artists := make([]string, 0, 1+len(featured))
	artists = append(artists, ar.Name)
	artistIDs := nonEmpty(ar.Mbid)
	for _, c := range featured {
		artists = append(artists, c.Name)
		artistIDs = append(artistIDs, nonEmpty(c.Mbid)...)
	}
	artist := ar.Name
	artistSort := ar.SortName
	if len(featured) > 0 {
		names := make([]string, len(featured))
		for i, c := range featured {
			names[i] = c.Name
		}
		artist += " feat. " + joinNames(names)
		// A sort name is the album artist's alone; with guests on the track
		// there is none that is right.
		artistSort = ""
	}

	var trackTotal int
	var discTotal uint8
	for _, t := range al.Edges.Tracks {
		if t.Disc == tr.Disc {
			trackTotal++
		}
		discTotal = max(discTotal, t.Disc)
	}

	genres := al.Genres
	if len(genres) == 0 && ar.Genre != "" {
		genres = []string{ar.Genre}
	}

	var compilation []string
	if ar.Mbid == VariousArtistsMBID {
		compilation = []string{"1"}
	}

	return audiotags.Tags{
		audiotags.KeyTitle:            nonEmpty(tr.Title),
		audiotags.KeyArtist:           nonEmpty(artist),
		audiotags.KeyArtists:          artists,
		audiotags.KeyArtistSort:       nonEmpty(artistSort),
		audiotags.KeyAlbum:            nonEmpty(al.Title),
		audiotags.KeyAlbumArtist:      nonEmpty(ar.Name),
		audiotags.KeyAlbumArtistSort:  nonEmpty(ar.SortName),
		audiotags.KeyTrackNumber:      count(int(tr.Position)),
		audiotags.KeyTrackTotal:       count(trackTotal),
		audiotags.KeyDiscNumber:       count(int(tr.Disc)),
		audiotags.KeyDiscTotal:        count(int(discTotal)),
		audiotags.KeyDate:             nonEmpty(editionDate(al)),
		audiotags.KeyOriginalDate:     nonEmpty(originalDate(al)),
		audiotags.KeyGenre:            genres,
		audiotags.KeyLabel:            nonEmpty(al.Label),
		audiotags.KeyCatalogNumber:    nonEmpty(al.CatalogNumber),
		audiotags.KeyBarcode:          nonEmpty(al.Barcode),
		audiotags.KeyReleaseCountry:   nonEmpty(al.Country),
		audiotags.KeyMedia:            nonEmpty(medium(al.Media)),
		audiotags.KeyReleaseType:      nonEmpty(albumReleaseType[al.Type]),
		audiotags.KeyCompilation:      compilation,
		audiotags.KeyMBArtistID:       artistIDs,
		audiotags.KeyMBAlbumArtistID:  nonEmpty(ar.Mbid),
		audiotags.KeyMBAlbumID:        nonEmpty(al.ReleaseMbid),
		audiotags.KeyMBReleaseGroupID: nonEmpty(al.Mbid),
		audiotags.KeyMBRecordingID:    nonEmpty(tr.Mbid),
		audiotags.KeyMBReleaseTrackID: nonEmpty(tr.ReleaseTrackMbid),
	}
}

// originalDate is the release group's first release as MusicBrainz wrote it,
// or the year alone for an album stored before that was kept: the parsed
// release date cannot tell a year-only date from the first of January.
func originalDate(al *ent.Album) string {
	if al.OriginalDate != "" {
		return al.OriginalDate
	}
	if al.ReleaseDate != nil {
		return strconv.Itoa(al.ReleaseDate.Year())
	}
	return ""
}

// editionDate is the canonical release's own date, else the first release.
func editionDate(al *ent.Album) string {
	if al.EditionDate != "" {
		return al.EditionDate
	}
	return originalDate(al)
}

// medium is the album's medium when it has exactly one. The stored media are
// the union over every release of the group, so with several there is no
// telling which one these files came from.
func medium(csv string) string {
	if csv == "" || strings.Contains(csv, ",") {
		return ""
	}
	return albumMedia[csv]
}

// joinNames joins names as a credit reads: "A", "A & B", "A, B & C".
func joinNames(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " & " + names[len(names)-1]
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func count(n int) []string {
	if n <= 0 {
		return nil
	}
	return []string{strconv.Itoa(n)}
}
