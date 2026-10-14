package library

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/musiccredit"
	"github.com/datahearth/streamline/internal/library/audiotags"
)

// MusicCreditsVersion is the layout an album's credits are stored under
// today: writer roles, and each track's own production credits and
// performers. An album stamped with an older one is fetched again in the
// background, and until then its credit tags are left as the file has them.
const MusicCreditsVersion uint8 = 1

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
//
// The exception is a value the database does not know yet, as opposed to one
// MusicBrainz has none for: the credits while they are not current
// (creditTags), and the release details of an album hydrated before they
// were fetched (NULL Album.genres marks it) — its genres, its dates as
// MusicBrainz writes them and the release track ids. Those keys are left out,
// so the file keeps what it carries until the backfill lands.
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

	tags := audiotags.Tags{
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
	if al.Genres == nil {
		for _, k := range []string{
			audiotags.KeyDate, audiotags.KeyOriginalDate,
			audiotags.KeyGenre, audiotags.KeyMBReleaseTrackID,
		} {
			delete(tags, k)
		}
	}
	maps.Copy(tags, creditTags(al, tr))
	return tags
}

// creditTagKey is the tag each production role is written under, grouped as
// Picard groups them; artwork has none.
var creditTagKey = map[musiccredit.Role]string{
	musiccredit.RoleProducer:  audiotags.KeyProducer,
	musiccredit.RoleRecording: audiotags.KeyEngineer,
	musiccredit.RoleMastering: audiotags.KeyEngineer,
	musiccredit.RoleMix:       audiotags.KeyMixer,
	musiccredit.RoleConductor: audiotags.KeyConductor,
	musiccredit.RoleRemixer:   audiotags.KeyRemixer,
	musiccredit.RoleArranger:  audiotags.KeyArranger,
}

// writerTagKey is the tag each writer role is written under; a writer stored
// before writers had roles is a plain writer.
var writerTagKey = map[musiccredit.Role]string{
	musiccredit.RoleComposer: audiotags.KeyComposer,
	musiccredit.RoleLyricist: audiotags.KeyLyricist,
	musiccredit.RoleWriter:   audiotags.KeyWriter,
	musiccredit.RoleArranger: audiotags.KeyArranger,
	"":                       audiotags.KeyWriter,
}

// creditTags is one track's credit keys: its writers, its recording's own
// production credits and performers, and the album's release-level
// credits — the album's rows no track carries, since the album folds the
// recordings' credits in with its own. Nil, so the file keeps what it has,
// while the album's credits are missing or stored under an older layout:
// deleting a performer list an earlier tagger wrote, in favour of nothing,
// would lose it.
func creditTags(al *ent.Album, tr *ent.Track) audiotags.Tags {
	if al.CreditsFetchedAt == nil || al.CreditsVersion < MusicCreditsVersion {
		return nil
	}
	tags := audiotags.Tags{}
	for _, k := range creditTagKey {
		tags[k] = nil
	}
	for _, k := range writerTagKey {
		tags[k] = nil
	}
	tags[audiotags.KeyPerformer] = nil
	add := func(key, value string) {
		if key != "" && !slices.Contains(tags[key], value) {
			tags[key] = append(tags[key], value)
		}
	}
	credit := func(c *ent.MusicCredit) string {
		id := c.Mbid
		if id == "" {
			id = strings.ToLower(c.Name)
		}
		return id + "|" + string(c.Role)
	}

	onTracks := map[string]bool{}
	for _, t := range al.Edges.Tracks {
		for _, c := range t.Edges.Credits {
			if c.Kind == musiccredit.KindCredit {
				onTracks[credit(c)] = true
			}
		}
	}
	for _, c := range tr.Edges.Credits {
		switch c.Kind {
		case musiccredit.KindWriter:
			add(writerTagKey[c.Role], c.Name)
		case musiccredit.KindCredit:
			add(creditTagKey[c.Role], c.Name)
		case musiccredit.KindPerformer:
			instruments := strings.Split(c.Instruments, ",")
			for _, in := range instruments {
				if in = strings.TrimSpace(in); in != "" {
					add(audiotags.KeyPerformer, c.Name+" ("+in+")")
				} else if len(instruments) == 1 {
					add(audiotags.KeyPerformer, c.Name)
				}
			}
		case musiccredit.KindFeaturing:
		}
	}
	for _, c := range al.Edges.Credits {
		if c.Kind == musiccredit.KindCredit && !onTracks[credit(c)] {
			add(creditTagKey[c.Role], c.Name)
		}
	}
	return tags
}

// originalDate is the release group's first release as MusicBrainz wrote it,
// or the year alone when MusicBrainz gave no date string: the parsed release
// date cannot tell a year-only date from the first of January.
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
