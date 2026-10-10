package subsonic

import (
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/track"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	defaultListSize = 10
	maxListSize     = 500
)

const (
	kindArtist = "ar"
	kindAlbum  = "al"
	kindTrack  = "tr"
)

var contentTypes = map[string]string{
	"flac": "audio/flac",
	"mp3":  "audio/mpeg",
	"m4a":  "audio/mp4",
	"ogg":  "audio/ogg",
	"opus": "audio/opus",
}

func formatID(
	kind string,
	id uint32,
) string {
	return fmt.Sprintf("%s-%d", kind, id)
}

func artistID(id uint32) string { return formatID(kindArtist, id) }
func albumID(id uint32) string  { return formatID(kindAlbum, id) }
func trackID(id uint32) string  { return formatID(kindTrack, id) }

func parseID(s string) (string, uint32, error) {
	kind, tail, ok := strings.Cut(s, "-")
	if !ok || (kind != kindArtist && kind != kindAlbum && kind != kindTrack) {
		return "", 0, fmt.Errorf("malformed id %q", s)
	}
	n, err := strconv.ParseUint(tail, 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("malformed id %q: %w", s, err)
	}
	return kind, uint32(n), nil
}

// anyIDParam reads the id parameter whatever kind it names.
func anyIDParam(r *http.Request) (string, uint32, error) {
	raw := r.FormValue("id")
	if raw == "" {
		return "", 0, &apiError{
			Code:    errMissingParam,
			Message: "Required parameter is missing: id",
		}
	}
	kind, id, err := parseID(raw)
	if err != nil {
		return "", 0, errNotFoundErr
	}
	return kind, id, nil
}

func idParam(r *http.Request, wantKind string) (uint32, error) {
	kind, id, err := anyIDParam(r)
	if err != nil {
		return 0, err
	}
	if kind != wantKind {
		return 0, errNotFoundErr
	}
	return id, nil
}

var errNotFoundErr = &apiError{
	Code:    errNotFound,
	Message: "The requested data was not found",
}

func lookupErr(err error) error {
	if ent.IsNotFound(err) {
		return errNotFoundErr
	}
	return err
}

func intParam(r *http.Request, name string, def, lo, hi int) int {
	n, err := strconv.Atoi(r.FormValue(name))
	if err != nil {
		return def
	}
	return min(max(n, lo), hi)
}

func suffixOf(mf *ent.MediaFile) string {
	if s := strings.TrimPrefix(
		strings.ToLower(filepath.Ext(mf.Path)),
		".",
	); s != "" {
		return s
	}
	if mf.Format == "other" {
		return ""
	}
	return mf.Format
}

func contentTypeFor(suffix string) string {
	if ct, ok := contentTypes[suffix]; ok {
		return ct
	}
	return "application/octet-stream"
}

func liveFiles(q *ent.MediaFileQuery) {
	q.Where(mediafile.MissingSinceIsNil()).Order(mediafile.ByID())
}

func firstFile(t *ent.Track) *ent.MediaFile {
	if len(t.Edges.MediaFiles) == 0 {
		return nil
	}
	return t.Edges.MediaFiles[0]
}

func yearOf(al *ent.Album) uint16 {
	if al.ReleaseDate == nil {
		return 0
	}
	return uint16(min(max(al.ReleaseDate.Year(), 0), math.MaxUint16))
}

func toArtist(ar *ent.Artist) artistID3 {
	return artistID3{
		ID:         artistID(ar.ID),
		Name:       ar.Name,
		AlbumCount: len(ar.Edges.Albums),
		CoverArt:   artistID(ar.ID),
	}
}

func toAlbum(al *ent.Album, ar *ent.Artist) albumID3 {
	var duration uint32
	for _, t := range al.Edges.Tracks {
		duration += t.Duration
	}
	return albumID3{
		ID:        albumID(al.ID),
		Name:      al.Title,
		Artist:    ar.Name,
		ArtistID:  artistID(ar.ID),
		CoverArt:  albumID(al.ID),
		SongCount: len(al.Edges.Tracks),
		Duration:  duration,
		Year:      yearOf(al),
		Created:   al.CreateTime.UTC().Format(time.RFC3339),
	}
}

func toSong(t *ent.Track, al *ent.Album, ar *ent.Artist, mf *ent.MediaFile) child {
	suffix := suffixOf(mf)
	duration := mf.DurationSeconds
	if duration == 0 {
		duration = t.Duration
	}
	bitRate := mf.Bitrate / 1000
	if bitRate == 0 && duration > 0 && mf.Size > 0 {
		kbps := min(mf.Size*8/int64(duration)/1000, math.MaxUint32)
		bitRate = uint32(kbps) //nolint:gosec // clamped above
	}
	return child{
		ID:          trackID(t.ID),
		Parent:      albumID(al.ID),
		Title:       t.Title,
		Album:       al.Title,
		Artist:      ar.Name,
		Track:       t.Position,
		DiscNumber:  t.Disc,
		Year:        yearOf(al),
		CoverArt:    albumID(al.ID),
		Size:        mf.Size,
		ContentType: contentTypeFor(suffix),
		Suffix:      suffix,
		Duration:    duration,
		BitRate:     bitRate,
		AlbumID:     albumID(al.ID),
		ArtistID:    artistID(ar.ID),
		Type:        "music",
	}
}

func songsOf(al *ent.Album, ar *ent.Artist) []child {
	songs := make([]child, 0, len(al.Edges.Tracks))
	for _, t := range al.Edges.Tracks {
		if mf := firstFile(t); mf != nil {
			songs = append(songs, toSong(t, al, ar, mf))
		}
	}
	return songs
}

func albumsOf(als []*ent.Album) []albumID3 {
	out := make([]albumID3, 0, len(als))
	for _, al := range als {
		out = append(out, toAlbum(al, al.Edges.Artist))
	}
	return out
}

func (h *Handler) ping(w http.ResponseWriter, r *http.Request) error {
	writeOK(w, r, nil)
	return nil
}

func (h *Handler) getLicense(w http.ResponseWriter, r *http.Request) error {
	writeOK(w, r, &body{License: &license{Valid: true}})
	return nil
}

func (h *Handler) getMusicFolders(w http.ResponseWriter, r *http.Request) error {
	writeOK(w, r, &body{MusicFolders: &musicFolders{
		MusicFolder: []musicFolder{{ID: 1, Name: "Music"}},
	}})
	return nil
}

func sortKey(ar *ent.Artist) string {
	if ar.SortName != "" {
		return ar.SortName
	}
	return ar.Name
}

func indexLetter(key string) string {
	if r, _ := utf8.DecodeRuneInString(key); unicode.IsLetter(r) {
		return string(unicode.ToUpper(r))
	}
	return "#"
}

func (h *Handler) getArtists(w http.ResponseWriter, r *http.Request) error {
	artists, err := h.ent.Artist.Query().
		WithAlbums().
		Order(artist.ByID()).
		All(r.Context())
	if err != nil {
		return err
	}
	sort.SliceStable(artists, func(i, j int) bool {
		return strings.ToLower(
			sortKey(artists[i]),
		) < strings.ToLower(
			sortKey(artists[j]),
		)
	})

	out := &indexes{Index: []index{}}
	for _, ar := range artists {
		letter := indexLetter(sortKey(ar))
		if n := len(out.Index); n == 0 || out.Index[n-1].Name != letter {
			out.Index = append(out.Index, index{Name: letter, Artist: []artistID3{}})
		}
		last := &out.Index[len(out.Index)-1]
		last.Artist = append(last.Artist, toArtist(ar))
	}
	writeOK(w, r, &body{Artists: out})
	return nil
}

func (h *Handler) getArtist(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, kindArtist)
	if err != nil {
		return err
	}
	ar, err := h.ent.Artist.Query().
		Where(artist.IDEQ(id)).
		WithAlbums(func(q *ent.AlbumQuery) {
			q.WithTracks().Order(album.ByReleaseDate(), album.ByTitle())
		}).
		Only(r.Context())
	if err != nil {
		return lookupErr(err)
	}
	out := &artistWith{
		artistID3: toArtist(ar),
		Album:     make([]albumID3, 0, len(ar.Edges.Albums)),
	}
	for _, al := range ar.Edges.Albums {
		out.Album = append(out.Album, toAlbum(al, ar))
	}
	writeOK(w, r, &body{Artist: out})
	return nil
}

func (h *Handler) getAlbum(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, kindAlbum)
	if err != nil {
		return err
	}
	al, err := h.ent.Album.Query().
		Where(album.IDEQ(id)).
		WithArtist().
		WithTracks(func(q *ent.TrackQuery) {
			q.Order(track.ByDisc(), track.ByPosition()).WithMediaFiles(liveFiles)
		}).
		Only(r.Context())
	if err != nil {
		return lookupErr(err)
	}
	ar := al.Edges.Artist
	out := &albumWith{albumID3: toAlbum(al, ar), Song: songsOf(al, ar)}
	writeOK(w, r, &body{Album: out})
	return nil
}

func (h *Handler) getSong(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, kindTrack)
	if err != nil {
		return err
	}
	t, err := h.ent.Track.Query().
		Where(track.IDEQ(id), track.HasMediaFilesWith(mediafile.MissingSinceIsNil())).
		WithAlbum(func(q *ent.AlbumQuery) { q.WithArtist() }).
		WithMediaFiles(liveFiles).
		Only(r.Context())
	if err != nil {
		return lookupErr(err)
	}
	al := t.Edges.Album
	song := toSong(t, al, al.Edges.Artist, firstFile(t))
	writeOK(w, r, &body{Song: &song})
	return nil
}

func (h *Handler) getAlbumList2(w http.ResponseWriter, r *http.Request) error {
	q := h.ent.Album.Query().
		WithArtist().
		WithTracks().
		Limit(intParam(r, "size", defaultListSize, 1, maxListSize)).
		Offset(intParam(r, "offset", 0, 0, 1<<30))

	switch r.FormValue("type") {
	case "newest":
		q.Order(album.ByCreateTime(sql.OrderDesc()), album.ByID(sql.OrderDesc()))
	case "random":
		q.Order(func(s *sql.Selector) { s.OrderBy("RANDOM()") })
	default:
		q.Order(album.ByTitle(), album.ByID())
	}

	als, err := q.All(r.Context())
	if err != nil {
		return err
	}
	writeOK(w, r, &body{AlbumList2: &albumList2{Album: albumsOf(als)}})
	return nil
}

func (h *Handler) search3(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	query := r.FormValue("query")
	out := &search3{Artist: []artistID3{}, Album: []albumID3{}, Song: []child{}}

	if n := intParam(r, "artistCount", defaultPageSize, 0, maxPageSize); n > 0 {
		ars, err := h.ent.Artist.Query().
			Where(artist.NameContainsFold(query)).
			WithAlbums().
			Order(artist.ByName(), artist.ByID()).
			Limit(n).
			Offset(intParam(r, "artistOffset", 0, 0, 1<<30)).
			All(ctx)
		if err != nil {
			return err
		}
		for _, ar := range ars {
			out.Artist = append(out.Artist, toArtist(ar))
		}
	}

	if n := intParam(r, "albumCount", defaultPageSize, 0, maxPageSize); n > 0 {
		als, err := h.ent.Album.Query().
			Where(album.TitleContainsFold(query)).
			WithArtist().
			WithTracks().
			Order(album.ByTitle(), album.ByID()).
			Limit(n).
			Offset(intParam(r, "albumOffset", 0, 0, 1<<30)).
			All(ctx)
		if err != nil {
			return err
		}
		out.Album = albumsOf(als)
	}

	if n := intParam(r, "songCount", defaultPageSize, 0, maxPageSize); n > 0 {
		ts, err := h.ent.Track.Query().
			Where(
				track.TitleContainsFold(query),
				track.HasMediaFilesWith(mediafile.MissingSinceIsNil()),
			).
			WithAlbum(func(q *ent.AlbumQuery) { q.WithArtist() }).
			WithMediaFiles(liveFiles).
			Order(track.ByTitle(), track.ByID()).
			Limit(n).
			Offset(intParam(r, "songOffset", 0, 0, 1<<30)).
			All(ctx)
		if err != nil {
			return err
		}
		for _, t := range ts {
			al := t.Edges.Album
			out.Song = append(out.Song, toSong(t, al, al.Edges.Artist, firstFile(t)))
		}
	}

	writeOK(w, r, &body{SearchResult: out})
	return nil
}
