package subsonic

import (
	"net/http"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/track"
)

// searchQuery is the query parameter with the double quotes some clients
// wrap it in removed: Symfonium and DSub send `""` to mean "everything" when
// they sync the whole library through search3.
func searchQuery(r *http.Request) string {
	q := strings.TrimSpace(r.FormValue("query"))
	if len(q) >= 2 && strings.HasPrefix(q, `"`) && strings.HasSuffix(q, `"`) {
		q = q[1 : len(q)-1]
	}
	return strings.TrimSpace(q)
}

type searchHits struct {
	artists []*ent.Artist
	albums  []*ent.Album
	tracks  []*ent.Track
}

// maxSearchSize bounds search2/search3 counts. Clients sync the whole library
// by paging an empty query at their own count and stop at the first short
// page, so a ceiling below the counts they send ends the sync early.
const maxSearchSize = maxListSize

// searchAll is the shared body of search2 and search3.
func (h *Handler) searchAll(r *http.Request) (searchHits, error) {
	ctx := r.Context()
	query := searchQuery(r)
	var out searchHits
	if otherFolder(r) {
		return out, nil
	}

	if n := intParam(r, "artistCount", defaultPageSize, 0, maxSearchSize); n > 0 {
		ars, err := h.ent.Artist.Query().
			Where(artist.NameContainsFold(query)).
			WithAlbums().
			Order(artist.ByName(), artist.ByID()).
			Limit(n).
			Offset(intParam(r, "artistOffset", 0, 0, 1<<30)).
			All(ctx)
		if err != nil {
			return out, err
		}
		out.artists = ars
	}

	if n := intParam(r, "albumCount", defaultPageSize, 0, maxSearchSize); n > 0 {
		als, err := h.ent.Album.Query().
			Where(album.TitleContainsFold(query)).
			WithArtist().
			WithTracks().
			Order(album.ByTitle(), album.ByID()).
			Limit(n).
			Offset(intParam(r, "albumOffset", 0, 0, 1<<30)).
			All(ctx)
		if err != nil {
			return out, err
		}
		out.albums = als
	}

	if n := intParam(r, "songCount", defaultPageSize, 0, maxSearchSize); n > 0 {
		ts, err := songEdges(h.ent.Track.Query().Where(
			track.TitleContainsFold(query),
			playable(),
		)).
			Order(track.ByTitle(), track.ByID()).
			Limit(n).
			Offset(intParam(r, "songOffset", 0, 0, 1<<30)).
			All(ctx)
		if err != nil {
			return out, err
		}
		out.tracks = ts
	}
	return out, nil
}

func (h *Handler) search2(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	hits, err := h.searchAll(r)
	if err != nil {
		return err
	}
	return h.respond(w, r, u, &body{SearchResult2: &searchResult2{
		Artist: artistsOf(hits.artists),
		Album:  albumChildren(hits.albums),
		Song:   songList(hits.tracks),
	}})
}

func (h *Handler) search3(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	hits, err := h.searchAll(r)
	if err != nil {
		return err
	}
	return h.respond(w, r, u, &body{SearchResult3: &search3{
		Artist: artistsOf(hits.artists),
		Album:  albumsOf(hits.albums),
		Song:   songList(hits.tracks),
	}})
}

// search is the deprecated first search: songs only, matched on the artist,
// album and title parameters together, or on any of the three by "any".
func (h *Handler) search(w http.ResponseWriter, r *http.Request, u *ent.User) error {
	ctx := r.Context()
	preds := []predicate.Track{playable()}
	if v := strings.TrimSpace(r.FormValue("artist")); v != "" {
		preds = append(
			preds,
			track.HasAlbumWith(album.HasArtistWith(artist.NameContainsFold(v))),
		)
	}
	if v := strings.TrimSpace(r.FormValue("album")); v != "" {
		preds = append(preds, track.HasAlbumWith(album.TitleContainsFold(v)))
	}
	if v := strings.TrimSpace(r.FormValue("title")); v != "" {
		preds = append(preds, track.TitleContainsFold(v))
	}
	if v := strings.TrimSpace(r.FormValue("any")); v != "" {
		preds = append(preds, track.Or(
			track.TitleContainsFold(v),
			track.HasAlbumWith(album.TitleContainsFold(v)),
			track.HasAlbumWith(album.HasArtistWith(artist.NameContainsFold(v))),
		))
	}
	if since, ok := millisParam(r, "newerThan"); ok {
		preds = append(preds, track.HasMediaFilesWith(
			mediafile.MissingSinceIsNil(),
			mediafile.CreateTimeGT(since),
		))
	}
	total, err := h.ent.Track.Query().Where(preds...).Count(ctx)
	if err != nil {
		return err
	}
	offset := intParam(r, "offset", 0, 0, 1<<30)
	ts, err := songEdges(h.ent.Track.Query().Where(preds...)).
		Order(track.ByTitle(), track.ByID()).
		Limit(intParam(r, "count", defaultPageSize, 1, maxPageSize)).
		Offset(offset).
		All(ctx)
	if err != nil {
		return err
	}
	return h.respond(w, r, u, &body{SearchResult: &searchResult{
		Offset:    offset,
		TotalHits: total,
		Match:     songList(ts),
	}})
}
