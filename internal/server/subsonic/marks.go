package subsonic

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/musicannotation"
	"github.com/datahearth/streamline/ent/track"
)

// marks are one listener's annotations, keyed by Subsonic id ("tr-12").
type marks map[string]*ent.MusicAnnotation

// markChunk bounds the IN lists of one annotation read.
const markChunk = 500

func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func optTimestamp(t *time.Time) string {
	if t == nil {
		return ""
	}
	return timestamp(*t)
}

// collector gathers every annotatable element of a response by pointer, so
// decorate can fill them in place after one read per kind.
type collector struct {
	children []*child
	albums   []*albumID3
	artists  []*artistID3
	dir      *directory
}

func (c *collector) kids(s []child) {
	for i := range s {
		c.children = append(c.children, &s[i])
	}
}

func (c *collector) albumSlice(s []albumID3) {
	for i := range s {
		c.albums = append(c.albums, &s[i])
	}
}

func (c *collector) artistSlice(s []artistID3) {
	for i := range s {
		c.artists = append(c.artists, &s[i])
	}
}

func (c *collector) songsOf(s *songs) {
	if s != nil {
		c.kids(s.Song)
	}
}

func collect(b *body) *collector {
	c := &collector{}
	if b.Artists != nil {
		for i := range b.Artists.Index {
			c.artistSlice(b.Artists.Index[i].Artist)
		}
	}
	if b.Indexes != nil {
		for i := range b.Indexes.Index {
			c.artistSlice(b.Indexes.Index[i].Artist)
		}
	}
	if b.Directory != nil {
		c.dir = b.Directory
		c.kids(b.Directory.Child)
	}
	if b.Artist != nil {
		c.artists = append(c.artists, &b.Artist.artistID3)
		c.albumSlice(b.Artist.Album)
	}
	if b.Album != nil {
		c.albums = append(c.albums, &b.Album.albumID3)
		c.kids(b.Album.Song)
	}
	if b.Song != nil {
		c.children = append(c.children, b.Song)
	}
	if b.ArtistInfo != nil {
		c.artistSlice(b.ArtistInfo.SimilarArtist)
	}
	if b.ArtistInfo2 != nil {
		c.artistSlice(b.ArtistInfo2.SimilarArtist)
	}
	c.songsOf(b.SimilarSongs)
	c.songsOf(b.SimilarSongs2)
	c.songsOf(b.TopSongs)
	c.songsOf(b.RandomSongs)
	c.songsOf(b.SongsByGenre)
	if b.AlbumList != nil {
		c.kids(b.AlbumList.Album)
	}
	if b.AlbumList2 != nil {
		c.albumSlice(b.AlbumList2.Album)
	}
	if b.NowPlaying != nil {
		for i := range b.NowPlaying.Entry {
			c.children = append(c.children, &b.NowPlaying.Entry[i].child)
		}
	}
	if b.Starred != nil {
		c.artistSlice(b.Starred.Artist)
		c.kids(b.Starred.Album)
		c.kids(b.Starred.Song)
	}
	if b.Starred2 != nil {
		c.artistSlice(b.Starred2.Artist)
		c.albumSlice(b.Starred2.Album)
		c.kids(b.Starred2.Song)
	}
	if b.SearchResult != nil {
		c.kids(b.SearchResult.Match)
	}
	if b.SearchResult2 != nil {
		c.artistSlice(b.SearchResult2.Artist)
		c.kids(b.SearchResult2.Album)
		c.kids(b.SearchResult2.Song)
	}
	if b.SearchResult3 != nil {
		c.artistSlice(b.SearchResult3.Artist)
		c.albumSlice(b.SearchResult3.Album)
		c.kids(b.SearchResult3.Song)
	}
	if b.Playlist != nil {
		c.kids(b.Playlist.Entry)
	}
	if b.Shares != nil {
		for i := range b.Shares.Share {
			c.kids(b.Shares.Share[i].Entry)
		}
	}
	if b.Bookmarks != nil {
		for i := range b.Bookmarks.Bookmark {
			c.children = append(c.children, &b.Bookmarks.Bookmark[i].Entry)
		}
	}
	if b.PlayQueue != nil {
		c.kids(b.PlayQueue.Entry)
	}
	if b.PlayQueueByIndex != nil {
		c.kids(b.PlayQueueByIndex.Entry)
	}
	return c
}

func (h *Handler) decorate(ctx context.Context, u *ent.User, b *body) error {
	c := collect(b)
	ids := map[string][]uint32{}
	add := func(raw string) {
		if kind, id, err := parseID(raw); err == nil {
			ids[kind] = append(ids[kind], id)
		}
	}
	for _, ch := range c.children {
		add(ch.ID)
	}
	for _, al := range c.albums {
		add(al.ID)
	}
	for _, ar := range c.artists {
		add(ar.ID)
	}
	if c.dir != nil {
		add(c.dir.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	m, err := h.loadMarks(ctx, u.ID, ids)
	if err != nil {
		return err
	}
	if len(m) == 0 {
		return nil
	}
	for _, ch := range c.children {
		if a := m[ch.ID]; a != nil {
			ch.Starred = optTimestamp(a.StarredAt)
			ch.UserRating = a.Rating
			ch.PlayCount = a.PlayCount
			ch.Played = optTimestamp(a.PlayedAt)
		}
	}
	for _, al := range c.albums {
		if a := m[al.ID]; a != nil {
			al.Starred = optTimestamp(a.StarredAt)
			al.UserRating = a.Rating
			al.PlayCount = a.PlayCount
			al.Played = optTimestamp(a.PlayedAt)
		}
	}
	for _, ar := range c.artists {
		if a := m[ar.ID]; a != nil {
			ar.Starred = optTimestamp(a.StarredAt)
			ar.UserRating = a.Rating
		}
	}
	if c.dir != nil {
		if a := m[c.dir.ID]; a != nil {
			c.dir.Starred = optTimestamp(a.StarredAt)
			c.dir.UserRating = a.Rating
			c.dir.PlayCount = a.PlayCount
			c.dir.Played = optTimestamp(a.PlayedAt)
		}
	}
	return nil
}

func (h *Handler) loadMarks(
	ctx context.Context,
	userID uint32,
	ids map[string][]uint32,
) (marks, error) {
	out := marks{}
	for kind, list := range ids {
		slices.Sort(list)
		list = slices.Compact(list)
		for chunk := range slices.Chunk(list, markChunk) {
			q := h.ent.MusicAnnotation.Query().
				Where(musicannotation.UserIDEQ(userID))
			switch kind {
			case kindTrack:
				q.Where(musicannotation.TrackIDIn(chunk...))
			case kindAlbum:
				q.Where(musicannotation.AlbumIDIn(chunk...))
			case kindArtist:
				q.Where(musicannotation.ArtistIDIn(chunk...))
			default:
				continue
			}
			rows, err := q.All(ctx)
			if err != nil {
				return nil, fmt.Errorf("load annotations: %w", err)
			}
			for _, a := range rows {
				out[markKey(a)] = a
			}
		}
	}
	return out, nil
}

func markKey(a *ent.MusicAnnotation) string {
	switch {
	case a.TrackID != nil:
		return trackID(*a.TrackID)
	case a.AlbumID != nil:
		return albumID(*a.AlbumID)
	case a.ArtistID != nil:
		return artistID(*a.ArtistID)
	}
	return ""
}

// markState is the writable part of an annotation.
type markState struct {
	starredAt *time.Time
	rating    uint8
	playCount uint32
	playedAt  *time.Time
}

// setMark reads the caller's annotation on one target, lets fn change it and
// writes it back, creating the row on first use.
func setMark(
	ctx context.Context,
	client *ent.Client,
	userID uint32,
	kind string,
	id uint32,
	fn func(*markState),
) error {
	q := client.MusicAnnotation.Query().Where(musicannotation.UserIDEQ(userID))
	switch kind {
	case kindTrack:
		q.Where(musicannotation.TrackIDEQ(id))
	case kindAlbum:
		q.Where(musicannotation.AlbumIDEQ(id))
	case kindArtist:
		q.Where(musicannotation.ArtistIDEQ(id))
	default:
		return errNotFoundErr
	}
	cur, err := q.Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("read annotation: %w", err)
	}
	var st markState
	if cur != nil {
		st = markState{cur.StarredAt, cur.Rating, cur.PlayCount, cur.PlayedAt}
	}
	fn(&st)
	if cur != nil {
		up := cur.Update().
			SetRating(st.rating).
			SetPlayCount(st.playCount).
			SetNillableStarredAt(st.starredAt).
			SetNillablePlayedAt(st.playedAt)
		if st.starredAt == nil {
			up.ClearStarredAt()
		}
		if err := up.Exec(ctx); err != nil {
			return fmt.Errorf("update annotation: %w", err)
		}
		return nil
	}
	create := client.MusicAnnotation.Create().
		SetUserID(userID).
		SetRating(st.rating).
		SetPlayCount(st.playCount).
		SetNillableStarredAt(st.starredAt).
		SetNillablePlayedAt(st.playedAt)
	switch kind {
	case kindTrack:
		create.SetTrackID(id)
	case kindAlbum:
		create.SetAlbumID(id)
	case kindArtist:
		create.SetArtistID(id)
	}
	if err := create.Exec(ctx); err != nil {
		if ent.IsConstraintError(err) {
			// The target vanished between the existence check and here.
			return errNotFoundErr
		}
		return fmt.Errorf("create annotation: %w", err)
	}
	return nil
}

// withTx runs fn in one transaction. The pool has a single connection, so
// fn must use the client it is handed, never h.ent.
func (h *Handler) withTx(ctx context.Context, fn func(*ent.Client) error) error {
	tx, err := h.ent.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := fn(tx.Client()); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// starTargets reads star/unstar's three id parameters into (kind, id) pairs.
// id may name any kind; albumId and artistId are the ID3 forms.
func starTargets(r *http.Request) ([]target, error) {
	var out []target
	for _, p := range []struct {
		name string
		kind string
	}{{"id", ""}, {"albumId", kindAlbum}, {"artistId", kindArtist}} {
		for _, raw := range r.Form[p.name] {
			kind, id, err := parseID(raw)
			if err != nil || (p.kind != "" && kind != p.kind) {
				return nil, errNotFoundErr
			}
			if kind != kindTrack && kind != kindAlbum && kind != kindArtist {
				return nil, errNotFoundErr
			}
			out = append(out, target{kind, id})
		}
	}
	if len(out) == 0 {
		return nil, missingParam("id")
	}
	return out, nil
}

type target struct {
	kind string
	id   uint32
}

func (h *Handler) star(w http.ResponseWriter, r *http.Request, u *ent.User) error {
	return h.applyStar(w, r, u, true)
}

func (h *Handler) unstar(w http.ResponseWriter, r *http.Request, u *ent.User) error {
	return h.applyStar(w, r, u, false)
}

func (h *Handler) applyStar(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
	on bool,
) error {
	targets, err := starTargets(r)
	if err != nil {
		return err
	}
	now := time.Now()
	err = h.withTx(r.Context(), func(c *ent.Client) error {
		for _, t := range targets {
			if err := setMark(
				r.Context(),
				c,
				u.ID,
				t.kind,
				t.id,
				func(s *markState) {
					switch {
					case !on:
						s.starredAt = nil
					case s.starredAt == nil:
						s.starredAt = &now
					}
				},
			); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	writeOK(w, r, nil)
	return nil
}

func (h *Handler) setRating(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	kind, id, err := anyIDParam(r)
	if err != nil {
		return err
	}
	if r.FormValue("rating") == "" {
		return missingParam("rating")
	}
	rating := intParam(r, "rating", -1, -1, 6)
	if rating < 0 || rating > 5 {
		return &apiError{Code: errGeneric, Message: "rating must be between 0 and 5"}
	}
	err = h.withTx(r.Context(), func(c *ent.Client) error {
		return setMark(r.Context(), c, u.ID, kind, id, func(s *markState) {
			s.rating = uint8(rating) //nolint:gosec // bounded above
		})
	})
	if err != nil {
		return err
	}
	writeOK(w, r, nil)
	return nil
}

// scrobble records plays (submission=true, the default) or reports what is
// playing now (submission=false). A play counts on the track, its album and
// its artist, so the album lists' frequent and recent read their own rows.
func (h *Handler) scrobble(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ctx := r.Context()
	raw := r.Form["id"]
	if len(raw) == 0 {
		return missingParam("id")
	}
	times := r.Form["time"]
	submission := r.FormValue("submission") != "false"

	type play struct {
		t  *ent.Track
		at time.Time
	}
	plays := make([]play, 0, len(raw))
	for i, s := range raw {
		kind, id, err := parseID(s)
		if err != nil || kind != kindTrack {
			return errNotFoundErr
		}
		t, err := h.ent.Track.Query().
			Where(track.IDEQ(id)).
			WithAlbum(func(q *ent.AlbumQuery) { q.Select(album.FieldID).WithArtist() }).
			Only(ctx)
		if err != nil {
			return lookupErr(err)
		}
		at := time.Now()
		if i < len(times) {
			if ms, err := parseMillis(times[i]); err == nil && ms > 0 {
				at = time.UnixMilli(ms)
			}
		}
		plays = append(plays, play{t, at})
	}

	if !submission {
		last := plays[len(plays)-1]
		h.playing.set(u, r.FormValue("c"), last.t.ID, last.t.Duration)
		writeOK(w, r, nil)
		return nil
	}

	err := h.withTx(ctx, func(c *ent.Client) error {
		for _, p := range plays {
			bump := func(s *markState) {
				s.playCount++
				if s.playedAt == nil || p.at.After(*s.playedAt) {
					at := p.at
					s.playedAt = &at
				}
			}
			al := p.t.Edges.Album
			for _, tg := range []target{
				{kindTrack, p.t.ID},
				{kindAlbum, al.ID},
				{kindArtist, al.Edges.Artist.ID},
			} {
				if err := setMark(ctx, c, u.ID, tg.kind, tg.id, bump); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, p := range plays {
		h.playing.done(u.ID, r.FormValue("c"), p.t.ID)
	}
	writeOK(w, r, nil)
	return nil
}
