package subsonic

import (
	"context"
	"net/http"
	"sort"
	"time"

	"entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/musicannotation"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/track"
)

func randomOrder(s *sql.Selector) { s.OrderBy("RANDOM()") }

// inOrder returns rows in the order of ids, dropping ids with no row.
func inOrder[T any](ids []uint32, rows []T, idOf func(T) uint32) []T {
	byID := make(map[uint32]T, len(rows))
	for _, row := range rows {
		byID[idOf(row)] = row
	}
	out := make([]T, 0, len(ids))
	for _, id := range ids {
		if row, ok := byID[id]; ok {
			out = append(out, row)
		}
	}
	return out
}

func albumIDOf(a *ent.Album) uint32   { return a.ID }
func trackIDOf(t *ent.Track) uint32   { return t.ID }
func artistIDOf(a *ent.Artist) uint32 { return a.ID }

// yearRange is the release-date window of fromYear..toYear, both inclusive.
// reversed reports toYear < fromYear, which the API defines as newest first.
func yearRange(from, to int) (lo, hi time.Time, reversed bool) {
	if to < from {
		from, to, reversed = to, from, true
	}
	lo = time.Date(from, time.January, 1, 0, 0, 0, 0, time.UTC)
	hi = time.Date(to+1, time.January, 1, 0, 0, 0, 0, time.UTC)
	return lo, hi, reversed
}

// albumList serves both getAlbumList and getAlbumList2.
func (h *Handler) albumList(r *http.Request, u *ent.User) ([]*ent.Album, error) {
	ctx := r.Context()
	if otherFolder(r) {
		return nil, nil
	}
	size := intParam(r, "size", defaultListSize, 1, maxListSize)
	offset := intParam(r, "offset", 0, 0, 1<<30)
	q := h.ent.Album.Query().WithArtist().WithTracks().Limit(size).Offset(offset)

	byMark := func(cond predicate.MusicAnnotation, order ...musicannotation.OrderOption) ([]*ent.Album, error) {
		return h.albumsByMark(ctx, u.ID, cond, size, offset, order...)
	}
	switch r.FormValue("type") {
	case "random":
		q.Order(randomOrder)
	case "newest":
		q.Order(album.ByCreateTime(sql.OrderDesc()), album.ByID(sql.OrderDesc()))
	case "alphabeticalByArtist":
		q.Order(
			album.ByArtistField(artist.FieldName),
			album.ByReleaseDate(),
			album.ByTitle(),
			album.ByID(),
		)
	case "byYear":
		if r.FormValue("fromYear") == "" {
			return nil, missingParam("fromYear")
		}
		if r.FormValue("toYear") == "" {
			return nil, missingParam("toYear")
		}
		lo, hi, reversed := yearRange(
			intParam(r, "fromYear", 0, 0, 9999),
			intParam(r, "toYear", 0, 0, 9999),
		)
		q.Where(album.ReleaseDateGTE(lo), album.ReleaseDateLT(hi))
		if reversed {
			q.Order(album.ByReleaseDate(sql.OrderDesc()), album.ByID())
		} else {
			q.Order(album.ByReleaseDate(), album.ByID())
		}
	case "byGenre":
		g := r.FormValue("genre")
		if g == "" {
			return nil, missingParam("genre")
		}
		q.Where(album.HasArtistWith(artist.GenreEqualFold(g))).
			Order(album.ByTitle(), album.ByID())
	case "starred":
		return byMark(
			musicannotation.StarredAtNotNil(),
			musicannotation.ByStarredAt(sql.OrderDesc()),
		)
	case "highest":
		return byMark(
			musicannotation.RatingGT(0),
			musicannotation.ByRating(sql.OrderDesc()),
			musicannotation.ByUpdateTime(sql.OrderDesc()),
		)
	case "frequent":
		return byMark(
			musicannotation.PlayCountGT(0),
			musicannotation.ByPlayCount(sql.OrderDesc()),
			musicannotation.ByPlayedAt(sql.OrderDesc()),
		)
	case "recent":
		return byMark(
			musicannotation.PlayedAtNotNil(),
			musicannotation.ByPlayedAt(sql.OrderDesc()),
		)
	default: // alphabeticalByName, and anything this server does not know
		q.Order(album.ByTitle(), album.ByID())
	}
	return q.All(ctx)
}

// albumsByMark pages the caller's album annotations matching cond, then
// loads those albums in that order.
func (h *Handler) albumsByMark(
	ctx context.Context,
	userID uint32,
	cond predicate.MusicAnnotation,
	size, offset int,
	order ...musicannotation.OrderOption,
) ([]*ent.Album, error) {
	rows, err := h.ent.MusicAnnotation.Query().
		Where(
			musicannotation.UserIDEQ(userID),
			musicannotation.AlbumIDNotNil(),
			cond,
		).
		Order(append(order, musicannotation.ByID())...).
		Limit(size).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]uint32, 0, len(rows))
	for _, a := range rows {
		ids = append(ids, *a.AlbumID)
	}
	als, err := h.ent.Album.Query().
		Where(album.IDIn(ids...)).
		WithArtist().
		WithTracks().
		All(ctx)
	if err != nil {
		return nil, err
	}
	return inOrder(ids, als, albumIDOf), nil
}

func (h *Handler) getAlbumList(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	als, err := h.albumList(r, u)
	if err != nil {
		return err
	}
	return h.respond(
		w,
		r,
		u,
		&body{AlbumList: &albumList{Album: albumChildren(als)}},
	)
}

func (h *Handler) getAlbumList2(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	als, err := h.albumList(r, u)
	if err != nil {
		return err
	}
	return h.respond(w, r, u, &body{AlbumList2: &albumList2{Album: albumsOf(als)}})
}

func (h *Handler) getRandomSongs(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	out := &songs{Song: []child{}}
	if otherFolder(r) {
		return h.respond(w, r, u, &body{RandomSongs: out})
	}
	q := h.ent.Track.Query().Where(playable())
	if g := r.FormValue("genre"); g != "" {
		q.Where(track.HasAlbumWith(album.HasArtistWith(artist.GenreEqualFold(g))))
	}
	if r.FormValue("fromYear") != "" {
		lo, _, _ := yearRange(intParam(r, "fromYear", 0, 0, 9999), 9999)
		q.Where(track.HasAlbumWith(album.ReleaseDateGTE(lo)))
	}
	if r.FormValue("toYear") != "" {
		_, hi, _ := yearRange(0, intParam(r, "toYear", 9999, 0, 9999))
		q.Where(track.HasAlbumWith(album.ReleaseDateLT(hi)))
	}
	ts, err := songEdges(q).
		Order(randomOrder).
		Limit(intParam(r, "size", defaultListSize, 1, maxListSize)).
		All(r.Context())
	if err != nil {
		return err
	}
	out.Song = songList(ts)
	return h.respond(w, r, u, &body{RandomSongs: out})
}

func (h *Handler) getSongsByGenre(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	g := r.FormValue("genre")
	if g == "" {
		return missingParam("genre")
	}
	out := &songs{Song: []child{}}
	if otherFolder(r) {
		return h.respond(w, r, u, &body{SongsByGenre: out})
	}
	ts, err := songEdges(h.ent.Track.Query().Where(
		playable(),
		track.HasAlbumWith(album.HasArtistWith(artist.GenreEqualFold(g))),
	)).
		Order(track.ByID()).
		Limit(intParam(r, "count", defaultListSize, 1, maxListSize)).
		Offset(intParam(r, "offset", 0, 0, 1<<30)).
		All(r.Context())
	if err != nil {
		return err
	}
	out.Song = songList(ts)
	return h.respond(w, r, u, &body{SongsByGenre: out})
}

// starredItems is everything the caller starred, newest star first.
func (h *Handler) starredItems(
	ctx context.Context,
	u *ent.User,
) ([]*ent.Artist, []*ent.Album, []*ent.Track, error) {
	rows, err := h.ent.MusicAnnotation.Query().
		Where(musicannotation.UserIDEQ(u.ID), musicannotation.StarredAtNotNil()).
		Order(musicannotation.ByStarredAt(sql.OrderDesc()), musicannotation.ByID()).
		All(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	var arIDs, alIDs, trIDs []uint32
	for _, a := range rows {
		switch {
		case a.TrackID != nil:
			trIDs = append(trIDs, *a.TrackID)
		case a.AlbumID != nil:
			alIDs = append(alIDs, *a.AlbumID)
		case a.ArtistID != nil:
			arIDs = append(arIDs, *a.ArtistID)
		}
	}
	ars, err := h.ent.Artist.Query().
		Where(artist.IDIn(arIDs...)).
		WithAlbums().
		All(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	als, err := h.ent.Album.Query().Where(album.IDIn(alIDs...)).
		WithArtist().
		WithTracks().
		All(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	ts, err := songEdges(
		h.ent.Track.Query().Where(track.IDIn(trIDs...), playable()),
	).
		All(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	return inOrder(arIDs, ars, artistIDOf),
		inOrder(alIDs, als, albumIDOf),
		inOrder(trIDs, ts, trackIDOf),
		nil
}

func artistsOf(ars []*ent.Artist) []artistID3 {
	out := make([]artistID3, 0, len(ars))
	for _, ar := range ars {
		out = append(out, toArtist(ar))
	}
	return out
}

func (h *Handler) getStarred(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ars, als, ts, err := h.starredItems(r.Context(), u)
	if err != nil {
		return err
	}
	return h.respond(w, r, u, &body{Starred: &starred{
		Artist: artistsOf(ars),
		Album:  albumChildren(als),
		Song:   songList(ts),
	}})
}

func (h *Handler) getStarred2(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ars, als, ts, err := h.starredItems(r.Context(), u)
	if err != nil {
		return err
	}
	return h.respond(w, r, u, &body{Starred2: &starred2{
		Artist: artistsOf(ars),
		Album:  albumsOf(als),
		Song:   songList(ts),
	}})
}

// topTracks ranks an artist's tracks by plays summed across listeners.
func (h *Handler) topTracks(
	ctx context.Context,
	artistID uint32,
	limit int,
) ([]uint32, error) {
	var rows []struct {
		TrackID uint32 `json:"track_id"`
		Plays   int    `json:"plays"`
	}
	err := h.ent.MusicAnnotation.Query().
		Where(
			musicannotation.TrackIDNotNil(),
			musicannotation.PlayCountGT(0),
			musicannotation.HasTrackWith(
				track.HasAlbumWith(album.HasArtistWith(artist.IDEQ(artistID))),
			),
		).
		GroupBy(musicannotation.FieldTrackID).
		Aggregate(func(s *sql.Selector) string {
			return sql.As(sql.Sum(s.C(musicannotation.FieldPlayCount)), "plays")
		}).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Plays != rows[j].Plays {
			return rows[i].Plays > rows[j].Plays
		}
		return rows[i].TrackID < rows[j].TrackID
	})
	ids := make([]uint32, 0, min(len(rows), limit))
	for _, row := range rows {
		if len(ids) == limit {
			break
		}
		ids = append(ids, row.TrackID)
	}
	return ids, nil
}
