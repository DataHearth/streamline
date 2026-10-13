package subsonic

import (
	"context"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/track"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	defaultListSize = 10
	maxListSize     = 500
	// defaultSimilar is getArtistInfo's count default and the similar and top
	// songs' one.
	defaultSimilar   = 20
	defaultSongCount = 50
)

var contentTypes = map[string]string{
	"flac": "audio/flac",
	"mp3":  "audio/mpeg",
	"m4a":  "audio/mp4",
	"ogg":  "audio/ogg",
	"opus": "audio/opus",
	"aac":  "audio/aac",
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

// playable keeps tracks with a file on disk: the only ones a client can play.
func playable() predicate.Track {
	return track.HasMediaFilesWith(mediafile.MissingSinceIsNil())
}

// songEdges loads what toSong reads.
func songEdges(q *ent.TrackQuery) *ent.TrackQuery {
	return q.WithAlbum(func(q *ent.AlbumQuery) { q.WithArtist() }).
		WithMediaFiles(liveFiles)
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
		ID:            artistID(ar.ID),
		Name:          ar.Name,
		AlbumCount:    len(ar.Edges.Albums),
		CoverArt:      artistID(ar.ID),
		MusicBrainzID: ar.Mbid,
		SortName:      ar.SortName,
		MediaType:     "artist",
	}
}

func toAlbum(al *ent.Album, ar *ent.Artist) albumID3 {
	var duration uint32
	for _, t := range al.Edges.Tracks {
		duration += t.Duration
	}
	return albumID3{
		ID:            albumID(al.ID),
		Name:          al.Title,
		Artist:        ar.Name,
		ArtistID:      artistID(ar.ID),
		CoverArt:      albumID(al.ID),
		SongCount:     len(al.Edges.Tracks),
		Duration:      duration,
		Year:          yearOf(al),
		Genre:         ar.Genre,
		Created:       timestamp(al.CreateTime),
		MusicBrainzID: al.Mbid,
		MediaType:     "album",
	}
}

// toAlbumChild is an album in the folder-style responses: a directory under
// its artist.
func toAlbumChild(al *ent.Album, ar *ent.Artist) child {
	a := toAlbum(al, ar)
	return child{
		ID:            a.ID,
		Parent:        a.ArtistID,
		IsDir:         true,
		Title:         a.Name,
		Album:         a.Name,
		Artist:        a.Artist,
		Year:          a.Year,
		Genre:         a.Genre,
		CoverArt:      a.CoverArt,
		Duration:      a.Duration,
		Created:       a.Created,
		AlbumID:       a.ID,
		ArtistID:      a.ArtistID,
		MediaType:     "album",
		MusicBrainzID: a.MusicBrainzID,
	}
}

// kbpsOf is the file's probed bitrate, else one estimated from its size
// over duration seconds; 0 when neither is known.
func kbpsOf(mf *ent.MediaFile, duration uint32) uint32 {
	if kbps := mf.Bitrate / 1000; kbps > 0 || duration == 0 || mf.Size <= 0 {
		return kbps
	}
	kbps := min(mf.Size*8/int64(duration)/1000, math.MaxUint32)
	return uint32(kbps) //nolint:gosec // clamped above
}

// durationOf is the file's probed length, else the track's MusicBrainz one
// (t may be nil); 0 when neither is known.
func durationOf(mf *ent.MediaFile, t *ent.Track) uint32 {
	if mf.DurationSeconds == 0 && t != nil {
		return t.Duration
	}
	return mf.DurationSeconds
}

func toSong(t *ent.Track, al *ent.Album, ar *ent.Artist, mf *ent.MediaFile) child {
	suffix := suffixOf(mf)
	duration := durationOf(mf, t)
	bitRate := kbpsOf(mf, duration)
	return child{
		ID:            trackID(t.ID),
		Parent:        albumID(al.ID),
		Title:         t.Title,
		Album:         al.Title,
		Artist:        ar.Name,
		Track:         t.Position,
		DiscNumber:    t.Disc,
		Year:          yearOf(al),
		Genre:         ar.Genre,
		CoverArt:      albumID(al.ID),
		Size:          mf.Size,
		ContentType:   contentTypeFor(suffix),
		Suffix:        suffix,
		Duration:      duration,
		BitRate:       bitRate,
		Created:       timestamp(mf.CreateTime),
		AlbumID:       albumID(al.ID),
		ArtistID:      artistID(ar.ID),
		Type:          "music",
		MediaType:     "song",
		MusicBrainzID: t.Mbid,
	}
}

// songOf renders a track loaded through songEdges.
func songOf(t *ent.Track) child {
	al := t.Edges.Album
	return toSong(t, al, al.Edges.Artist, firstFile(t))
}

// songList renders tracks loaded through songEdges, skipping any whose file
// went missing since the query.
func songList(ts []*ent.Track) []child {
	out := make([]child, 0, len(ts))
	for _, t := range ts {
		if firstFile(t) != nil {
			out = append(out, songOf(t))
		}
	}
	return out
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

func albumChildren(als []*ent.Album) []child {
	out := make([]child, 0, len(als))
	for _, al := range als {
		out = append(out, toAlbumChild(al, al.Edges.Artist))
	}
	return out
}

func (h *Handler) ping(w http.ResponseWriter, r *http.Request, _ *ent.User) error {
	writeOK(w, r, nil)
	return nil
}

func (h *Handler) getLicense(
	w http.ResponseWriter,
	r *http.Request,
	_ *ent.User,
) error {
	writeOK(w, r, &body{License: &license{Valid: true}})
	return nil
}

func (h *Handler) getMusicFolders(
	w http.ResponseWriter,
	r *http.Request,
	_ *ent.User,
) error {
	writeOK(w, r, &body{MusicFolders: &musicFolders{
		MusicFolder: []musicFolder{{ID: 1, Name: "Music"}},
	}})
	return nil
}

// otherFolder reports a musicFolderId naming a folder this server does not
// have: the library is the one folder, id 1.
func otherFolder(r *http.Request) bool {
	folder := r.FormValue("musicFolderId")
	return folder != "" && folder != "1"
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

func (h *Handler) sortedArtists(r *http.Request) ([]*ent.Artist, error) {
	artists, err := h.ent.Artist.Query().
		WithAlbums().
		Order(artist.ByID()).
		All(r.Context())
	if err != nil {
		return nil, err
	}
	sort.SliceStable(artists, func(i, j int) bool {
		return strings.ToLower(
			sortKey(artists[i]),
		) < strings.ToLower(
			sortKey(artists[j]),
		)
	})
	return artists, nil
}

func groupArtists(artists []*ent.Artist) []index {
	out := []index{}
	for _, ar := range artists {
		letter := indexLetter(sortKey(ar))
		if n := len(out); n == 0 || out[n-1].Name != letter {
			out = append(out, index{Name: letter, Artist: []artistID3{}})
		}
		last := &out[len(out)-1]
		last.Artist = append(last.Artist, toArtist(ar))
	}
	return out
}

func (h *Handler) getArtists(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	artists, err := h.sortedArtists(r)
	if err != nil {
		return err
	}
	if otherFolder(r) {
		artists = nil
	}
	return h.respond(w, r, u, &body{Artists: &indexes{Index: groupArtists(artists)}})
}

// getIndexes is the folder-based twin of getArtists: the library has the one
// music folder, so the same artist grouping answers it.
//
// ifModifiedSince is read but never short-circuits: no timestamp here moves
// when an artist is deleted (the newest remaining row can be older than the
// client's copy), so answering "unchanged" could leave a client holding a
// removed artist for good. The full index is always a correct answer.
func (h *Handler) getIndexes(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	artists, err := h.sortedArtists(r)
	if err != nil {
		return err
	}
	lastModified, err := h.libraryModified(r.Context(), artists)
	if err != nil {
		return err
	}

	out := &folderIndexes{LastModified: lastModified, Index: []index{}}
	if otherFolder(r) {
		artists = nil
	}
	out.Index = groupArtists(artists)
	return h.respond(w, r, u, &body{Indexes: out})
}

func (h *Handler) loadArtist(ctx context.Context, id uint32) (*ent.Artist, error) {
	ar, err := h.ent.Artist.Query().
		Where(artist.IDEQ(id)).
		WithAlbums(func(q *ent.AlbumQuery) {
			q.WithTracks().Order(album.ByReleaseDate(), album.ByTitle())
		}).
		Only(ctx)
	return ar, lookupErr(err)
}

func (h *Handler) loadAlbum(ctx context.Context, id uint32) (*ent.Album, error) {
	al, err := h.ent.Album.Query().
		Where(album.IDEQ(id)).
		WithArtist().
		WithTracks(func(q *ent.TrackQuery) {
			q.Order(track.ByDisc(), track.ByPosition()).WithMediaFiles(liveFiles)
		}).
		Only(ctx)
	return al, lookupErr(err)
}

func (h *Handler) getArtist(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	id, err := idParam(r, kindArtist)
	if err != nil {
		return err
	}
	ar, err := h.loadArtist(r.Context(), id)
	if err != nil {
		return err
	}
	out := &artistWith{
		artistID3: toArtist(ar),
		Album:     make([]albumID3, 0, len(ar.Edges.Albums)),
	}
	for _, al := range ar.Edges.Albums {
		out.Album = append(out.Album, toAlbum(al, ar))
	}
	return h.respond(w, r, u, &body{Artist: out})
}

func (h *Handler) getAlbum(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	id, err := idParam(r, kindAlbum)
	if err != nil {
		return err
	}
	al, err := h.loadAlbum(r.Context(), id)
	if err != nil {
		return err
	}
	ar := al.Edges.Artist
	out := &albumWith{albumID3: toAlbum(al, ar), Song: songsOf(al, ar)}
	return h.respond(w, r, u, &body{Album: out})
}

func (h *Handler) getSong(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	id, err := idParam(r, kindTrack)
	if err != nil {
		return err
	}
	t, err := songEdges(h.ent.Track.Query().Where(track.IDEQ(id), playable())).
		Only(r.Context())
	if err != nil {
		return lookupErr(err)
	}
	// The files load in a second query: one gone since the first leaves none.
	if firstFile(t) == nil {
		return errNotFoundErr
	}
	song := songOf(t)
	return h.respond(w, r, u, &body{Song: &song})
}

// getMusicDirectory walks the folder view: an artist holds its albums, an
// album its songs.
func (h *Handler) getMusicDirectory(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	kind, id, err := anyIDParam(r)
	if err != nil {
		return err
	}
	var dir *directory
	switch kind {
	case kindArtist:
		ar, err := h.loadArtist(r.Context(), id)
		if err != nil {
			return err
		}
		dir = &directory{ID: artistID(ar.ID), Name: ar.Name, Child: []child{}}
		for _, al := range ar.Edges.Albums {
			dir.Child = append(dir.Child, toAlbumChild(al, ar))
		}
	case kindAlbum:
		al, err := h.loadAlbum(r.Context(), id)
		if err != nil {
			return err
		}
		ar := al.Edges.Artist
		dir = &directory{
			ID:     albumID(al.ID),
			Parent: artistID(ar.ID),
			Name:   al.Title,
			Child:  songsOf(al, ar),
		}
	default:
		return errNotFoundErr
	}
	return h.respond(w, r, u, &body{Directory: dir})
}

// getGenres lists the artists' genres: MusicBrainz names one per artist, and
// its albums and songs inherit it.
func (h *Handler) getGenres(
	w http.ResponseWriter,
	r *http.Request,
	_ *ent.User,
) error {
	ctx := r.Context()
	als, err := h.ent.Album.Query().
		Where(album.HasArtistWith(artist.GenreNEQ(""))).
		WithArtist(func(q *ent.ArtistQuery) { q.Select(artist.FieldGenre) }).
		Select(album.FieldID).
		All(ctx)
	if err != nil {
		return err
	}
	albumCounts := map[string]int{}
	for _, al := range als {
		albumCounts[al.Edges.Artist.Genre]++
	}
	out := &genres{Genre: make([]genre, 0, len(albumCounts))}
	for name, n := range albumCounts {
		songs, err := h.ent.Track.Query().
			Where(playable(), track.HasAlbumWith(album.HasArtistWith(artist.GenreEQ(name)))).
			Count(ctx)
		if err != nil {
			return err
		}
		out.Genre = append(
			out.Genre,
			genre{Value: name, SongCount: songs, AlbumCount: n},
		)
	}
	sort.Slice(out.Genre, func(i, j int) bool {
		return strings.ToLower(
			out.Genre[i].Value,
		) < strings.ToLower(
			out.Genre[j].Value,
		)
	})
	writeOK(w, r, &body{Genres: out})
	return nil
}

// artistOf resolves an artist, album or song id to its artist, as
// getArtistInfo and the similar-songs calls accept any of the three.
func (h *Handler) artistOf(
	ctx context.Context,
	r *http.Request,
) (*ent.Artist, error) {
	kind, id, err := anyIDParam(r)
	if err != nil {
		return nil, err
	}
	q := h.ent.Artist.Query()
	switch kind {
	case kindArtist:
		q.Where(artist.IDEQ(id))
	case kindAlbum:
		q.Where(artist.HasAlbumsWith(album.IDEQ(id)))
	case kindTrack:
		q.Where(artist.HasAlbumsWith(album.HasTracksWith(track.IDEQ(id))))
	default:
		return nil, errNotFoundErr
	}
	ar, err := q.Only(ctx)
	return ar, lookupErr(err)
}

// similarArtists are other artists in the library sharing ar's genre, in a
// random order: the library knows no closer relation, and a shuffle keeps a
// client's "similar" row from always showing the same few.
func (h *Handler) similarArtists(
	ctx context.Context,
	ar *ent.Artist,
	n int,
) ([]*ent.Artist, error) {
	if ar.Genre == "" || n == 0 {
		return nil, nil
	}
	return h.ent.Artist.Query().
		Where(artist.GenreEQ(ar.Genre), artist.IDNEQ(ar.ID)).
		WithAlbums().
		Order(func(s *sql.Selector) { s.OrderBy("RANDOM()") }).
		Limit(n).
		All(ctx)
}

// biography is the stored Wikipedia overview, English first.
func biography(ar *ent.Artist) string {
	if ar.Overview != "" {
		return ar.Overview
	}
	return ar.OverviewFr
}

func (h *Handler) artistInfoParts(
	r *http.Request,
) (artistInfoBase, []artistID3, error) {
	ar, err := h.artistOf(r.Context(), r)
	if err != nil {
		return artistInfoBase{}, nil, err
	}
	similar, err := h.similarArtists(
		r.Context(), ar, intParam(r, "count", defaultSimilar, 0, maxPageSize),
	)
	if err != nil {
		return artistInfoBase{}, nil, err
	}
	out := make([]artistID3, 0, len(similar))
	for _, s := range similar {
		out = append(out, toArtist(s))
	}
	return artistInfoBase{Biography: biography(ar), MusicBrainzID: ar.Mbid}, out, nil
}

func (h *Handler) getArtistInfo(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	base, similar, err := h.artistInfoParts(r)
	if err != nil {
		return err
	}
	return h.respond(w, r, u, &body{
		ArtistInfo: &artistInfo{artistInfoBase: base, SimilarArtist: similar},
	})
}

func (h *Handler) getArtistInfo2(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	base, similar, err := h.artistInfoParts(r)
	if err != nil {
		return err
	}
	return h.respond(w, r, u, &body{
		ArtistInfo2: &artistInfo2{artistInfoBase: base, SimilarArtist: similar},
	})
}

// getAlbumInfo answers both getAlbumInfo and getAlbumInfo2: the two share a
// response. An album has no stored notes, only its MusicBrainz id.
func (h *Handler) getAlbumInfo(
	w http.ResponseWriter,
	r *http.Request,
	_ *ent.User,
) error {
	kind, id, err := anyIDParam(r)
	if err != nil {
		return err
	}
	q := h.ent.Album.Query()
	switch kind {
	case kindAlbum:
		q.Where(album.IDEQ(id))
	case kindTrack:
		q.Where(album.HasTracksWith(track.IDEQ(id)))
	default:
		return errNotFoundErr
	}
	al, err := q.Only(r.Context())
	if err != nil {
		return lookupErr(err)
	}
	writeOK(w, r, &body{AlbumInfo: &albumInfo{MusicBrainzID: al.Mbid}})
	return nil
}

// similarSongs is a random mix of the seed artist's songs and those of the
// artists sharing its genre: what a client's "instant mix" plays.
func (h *Handler) similarSongs(r *http.Request) ([]child, error) {
	ctx := r.Context()
	ar, err := h.artistOf(ctx, r)
	if err != nil {
		return nil, err
	}
	owner := track.HasAlbumWith(album.HasArtistWith(artist.IDEQ(ar.ID)))
	if ar.Genre != "" {
		owner = track.HasAlbumWith(album.HasArtistWith(
			artist.Or(artist.IDEQ(ar.ID), artist.GenreEQ(ar.Genre)),
		))
	}
	ts, err := songEdges(h.ent.Track.Query().Where(playable(), owner)).
		Order(func(s *sql.Selector) { s.OrderBy("RANDOM()") }).
		Limit(intParam(r, "count", defaultSongCount, 1, maxListSize)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return songList(ts), nil
}

func (h *Handler) getSimilarSongs(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	s, err := h.similarSongs(r)
	if err != nil {
		return err
	}
	return h.respond(w, r, u, &body{SimilarSongs: &songs{Song: s}})
}

func (h *Handler) getSimilarSongs2(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	s, err := h.similarSongs(r)
	if err != nil {
		return err
	}
	return h.respond(w, r, u, &body{SimilarSongs2: &songs{Song: s}})
}

// getTopSongs ranks an artist's songs by plays summed over every listener.
// A song nobody played is not a top song, so a fresh library answers none.
func (h *Handler) getTopSongs(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ctx := r.Context()
	name := r.FormValue("artist")
	if name == "" {
		return missingParam("artist")
	}
	limit := intParam(r, "count", defaultSongCount, 1, maxListSize)
	out := &songs{Song: []child{}}
	ar, err := h.ent.Artist.Query().Where(artist.NameEqualFold(name)).First(ctx)
	if ent.IsNotFound(err) {
		return h.respond(w, r, u, &body{TopSongs: out})
	}
	if err != nil {
		return err
	}
	ranked, err := h.topTracks(ctx, ar.ID, limit)
	if err != nil {
		return err
	}
	ts, err := songEdges(
		h.ent.Track.Query().Where(track.IDIn(ranked...), playable()),
	).
		All(ctx)
	if err != nil {
		return err
	}
	byID := make(map[uint32]*ent.Track, len(ts))
	for _, t := range ts {
		byID[t.ID] = t
	}
	for _, id := range ranked {
		if t := byID[id]; t != nil && firstFile(t) != nil {
			out.Song = append(out.Song, songOf(t))
		}
	}
	return h.respond(w, r, u, &body{TopSongs: out})
}

// libraryModified is the newest change to anything an index entry reflects:
// an artist row, or an album or track under it (album_count, a new release).
func (h *Handler) libraryModified(
	ctx context.Context,
	artists []*ent.Artist,
) (int64, error) {
	var newest int64
	for _, ar := range artists {
		newest = max(newest, ar.UpdateTime.UnixMilli())
	}
	al, err := h.ent.Album.Query().Order(ent.Desc(album.FieldUpdateTime)).First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return 0, err
	}
	if al != nil {
		newest = max(newest, al.UpdateTime.UnixMilli())
	}
	tr, err := h.ent.Track.Query().Order(ent.Desc(track.FieldUpdateTime)).First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return 0, err
	}
	if tr != nil {
		newest = max(newest, tr.UpdateTime.UnixMilli())
	}
	return newest, nil
}
