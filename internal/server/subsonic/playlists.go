package subsonic

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/playlist"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/ent/user"
)

func isAdmin(u *ent.User) bool { return u.Role == user.RoleAdmin }

// playableTracks loads the tracks behind ids that still have a file, in the
// order of ids and keeping duplicates: what a playlist, queue or share plays.
func (h *Handler) playableTracks(
	ctx context.Context,
	ids []uint32,
) ([]*ent.Track, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	uniq := slices.Clone(ids)
	slices.Sort(uniq)
	uniq = slices.Compact(uniq)
	var rows []*ent.Track
	for chunk := range slices.Chunk(uniq, markChunk) {
		ts, err := songEdges(
			h.ent.Track.Query().Where(track.IDIn(chunk...), playable()),
		).
			All(ctx)
		if err != nil {
			return nil, err
		}
		rows = append(rows, ts...)
	}
	return inOrder(ids, rows, trackIDOf), nil
}

// trackIDsParam reads a repeated track-id parameter, refusing an id that
// names no track.
func (h *Handler) trackIDsParam(r *http.Request, name string) ([]uint32, error) {
	raw := r.Form[name]
	ids := make([]uint32, 0, len(raw))
	for _, s := range raw {
		kind, id, err := parseID(s)
		if err != nil || kind != kindTrack {
			return nil, errNotFoundErr
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return ids, nil
	}
	uniq := slices.Clone(ids)
	slices.Sort(uniq)
	uniq = slices.Compact(uniq)
	for chunk := range slices.Chunk(uniq, markChunk) {
		n, err := h.ent.Track.Query().Where(track.IDIn(chunk...)).Count(r.Context())
		if err != nil {
			return nil, err
		}
		if n != len(chunk) {
			return nil, errNotFoundErr
		}
	}
	return ids, nil
}

func trackIDsOf(ts []*ent.Track) []uint32 {
	out := make([]uint32, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}

// canSee is whether u may read p: its owner, anyone when public, an admin.
func canSee(u *ent.User, p *ent.Playlist) bool {
	return p.Public || p.Edges.Owner.ID == u.ID || isAdmin(u)
}

func (h *Handler) loadPlaylist(
	ctx context.Context,
	u *ent.User,
	id uint32,
) (*ent.Playlist, error) {
	p, err := h.ent.Playlist.Query().Where(playlist.IDEQ(id)).WithOwner().Only(ctx)
	if err != nil {
		return nil, lookupErr(err)
	}
	if !canSee(u, p) {
		// A private playlist of someone else does not exist for the caller.
		return nil, errNotFoundErr
	}
	return p, nil
}

func toPlaylist(p *ent.Playlist, entries []child) playlistInfo {
	var duration uint32
	for _, e := range entries {
		duration += e.Duration
	}
	out := playlistHeader(p)
	out.SongCount = len(entries)
	out.Duration = duration
	if len(entries) > 0 {
		out.CoverArt = entries[0].CoverArt
	}
	return out
}

func playlistHeader(p *ent.Playlist) playlistInfo {
	return playlistInfo{
		ID:      playlistID(p.ID),
		Name:    p.Name,
		Comment: p.Comment,
		Owner:   p.Edges.Owner.Email,
		Public:  p.Public,
		Created: timestamp(p.CreateTime),
		Changed: timestamp(p.UpdateTime),
	}
}

func (h *Handler) playlistWith(
	ctx context.Context,
	p *ent.Playlist,
) (*playlistWith, error) {
	ts, err := h.playableTracks(ctx, p.TrackIds)
	if err != nil {
		return nil, err
	}
	entries := songList(ts)
	return &playlistWith{playlistInfo: toPlaylist(p, entries), Entry: entries}, nil
}

// getPlaylists lists the caller's playlists and everyone's public ones. An
// admin may name another user and gets all of theirs.
func (h *Handler) getPlaylists(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ctx := r.Context()
	q := h.ent.Playlist.Query().WithOwner().Order(playlist.ByName(), playlist.ByID())
	if name := r.FormValue(
		"username",
	); name != "" &&
		!strings.EqualFold(name, u.Email) {
		if !isAdmin(u) {
			return errNotAuthorizedErr
		}
		q.Where(playlist.HasOwnerWith(user.EmailEQ(name)))
	} else {
		q.Where(
			playlist.Or(
				playlist.HasOwnerWith(user.IDEQ(u.ID)),
				playlist.Public(true),
			),
		)
	}
	ps, err := q.All(ctx)
	if err != nil {
		return err
	}
	out := &playlists{Playlist: make([]playlistInfo, 0, len(ps))}
	for _, p := range ps {
		full, err := h.playlistWith(ctx, p)
		if err != nil {
			return err
		}
		out.Playlist = append(out.Playlist, full.playlistInfo)
	}
	writeOK(w, r, &body{Playlists: out})
	return nil
}

func (h *Handler) getPlaylist(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	id, err := idParam(r, kindPlaylist)
	if err != nil {
		return err
	}
	p, err := h.loadPlaylist(r.Context(), u, id)
	if err != nil {
		return err
	}
	full, err := h.playlistWith(r.Context(), p)
	if err != nil {
		return err
	}
	return h.respond(w, r, u, &body{Playlist: full})
}

// ownPlaylist loads a playlist the caller may change: their own, or any
// when they are an admin.
func (h *Handler) ownPlaylist(
	ctx context.Context,
	u *ent.User,
	id uint32,
) (*ent.Playlist, error) {
	p, err := h.loadPlaylist(ctx, u, id)
	if err != nil {
		return nil, err
	}
	if p.Edges.Owner.ID != u.ID && !isAdmin(u) {
		return nil, errNotAuthorizedErr
	}
	return p, nil
}

// createPlaylist creates a playlist from name, or with playlistId replaces
// an existing one's songs.
func (h *Handler) createPlaylist(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ctx := r.Context()
	ids, err := h.trackIDsParam(r, "songId")
	if err != nil {
		return err
	}
	var p *ent.Playlist
	if r.FormValue("playlistId") != "" {
		id, err := idParamNamed(r, "playlistId", kindPlaylist)
		if err != nil {
			return err
		}
		if p, err = h.ownPlaylist(ctx, u, id); err != nil {
			return err
		}
		up := p.Update().SetTrackIds(ids)
		if name := strings.TrimSpace(r.FormValue("name")); name != "" {
			up.SetName(name)
		}
		if p, err = up.Save(ctx); err != nil {
			return err
		}
	} else {
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			return missingParam("name")
		}
		if p, err = h.ent.Playlist.Create().
			SetName(name).
			SetOwnerID(u.ID).
			SetTrackIds(ids).
			Save(ctx); err != nil {
			return err
		}
	}
	if p, err = h.loadPlaylist(ctx, u, p.ID); err != nil {
		return err
	}
	full, err := h.playlistWith(ctx, p)
	if err != nil {
		return err
	}
	return h.respond(w, r, u, &body{Playlist: full})
}

// updatePlaylist edits the header and the songs. songIndexToRemove counts
// entries as getPlaylist listed them, so the stored list is first reduced to
// those: an entry whose file is gone would otherwise shift every index.
func (h *Handler) updatePlaylist(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ctx := r.Context()
	id, err := idParamNamed(r, "playlistId", kindPlaylist)
	if err != nil {
		return err
	}
	p, err := h.ownPlaylist(ctx, u, id)
	if err != nil {
		return err
	}
	add, err := h.trackIDsParam(r, "songIdToAdd")
	if err != nil {
		return err
	}
	up := p.Update()
	if _, ok := r.Form["name"]; ok {
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			return &apiError{Code: errGeneric, Message: "name must not be empty"}
		}
		up.SetName(name)
	}
	if _, ok := r.Form["comment"]; ok {
		up.SetComment(r.FormValue("comment"))
	}
	if _, ok := r.Form["public"]; ok {
		up.SetPublic(boolParam(r, "public", p.Public))
	}
	if remove := r.Form["songIndexToRemove"]; len(remove) > 0 || len(add) > 0 {
		ts, err := h.playableTracks(ctx, p.TrackIds)
		if err != nil {
			return err
		}
		ids := trackIDsOf(ts)
		drop := map[int]bool{}
		for _, s := range remove {
			i, err := strconv.Atoi(s)
			if err != nil || i < 0 || i >= len(ids) {
				return &apiError{
					Code:    errGeneric,
					Message: "songIndexToRemove out of range",
				}
			}
			drop[i] = true
		}
		kept := make([]uint32, 0, len(ids)+len(add))
		for i, t := range ids {
			if !drop[i] {
				kept = append(kept, t)
			}
		}
		up.SetTrackIds(append(kept, add...))
	}
	if err := up.Exec(ctx); err != nil {
		return err
	}
	writeOK(w, r, nil)
	return nil
}

func (h *Handler) deletePlaylist(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	id, err := idParam(r, kindPlaylist)
	if err != nil {
		return err
	}
	if _, err := h.ownPlaylist(r.Context(), u, id); err != nil {
		return err
	}
	if err := h.ent.Playlist.DeleteOneID(id).Exec(r.Context()); err != nil {
		return lookupErr(err)
	}
	writeOK(w, r, nil)
	return nil
}
