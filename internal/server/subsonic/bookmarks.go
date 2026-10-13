package subsonic

import (
	"net/http"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/bookmark"
	"github.com/datahearth/streamline/ent/playqueue"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/appaccess"
)

func (h *Handler) getBookmarks(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ctx := r.Context()
	bs, err := h.ent.Bookmark.Query().
		Where(bookmark.UserIDEQ(u.ID), bookmark.HasTrackWith(playable())).
		Order(bookmark.ByUpdateTime(), bookmark.ByID()).
		All(ctx)
	if err != nil {
		return err
	}
	ids := make([]uint32, 0, len(bs))
	for _, b := range bs {
		ids = append(ids, b.TrackID)
	}
	ts, err := h.playableTracks(ctx, ids)
	if err != nil {
		return err
	}
	byID := make(map[uint32]*ent.Track, len(ts))
	for _, t := range ts {
		byID[t.ID] = t
	}
	out := &bookmarks{Bookmark: make([]bookmarkInfo, 0, len(bs))}
	for _, b := range bs {
		t := byID[b.TrackID]
		if t == nil {
			continue
		}
		out.Bookmark = append(out.Bookmark, bookmarkInfo{
			Position: b.Position,
			Username: u.Email,
			Comment:  b.Comment,
			Created:  timestamp(b.CreateTime),
			Changed:  timestamp(b.UpdateTime),
			Entry:    songOf(t),
		})
	}
	return h.respond(w, r, u, &body{Bookmarks: out})
}

func (h *Handler) createBookmark(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ctx := r.Context()
	id, err := idParam(r, kindTrack)
	if err != nil {
		return err
	}
	if r.FormValue("position") == "" {
		return missingParam("position")
	}
	pos, err := parseMillis(r.FormValue("position"))
	if err != nil || pos < 0 {
		return &apiError{
			Code:    errGeneric,
			Message: "position must be a non-negative number",
		}
	}
	if ok, err := h.ent.Track.Query().Where(track.IDEQ(id)).Exist(ctx); err != nil {
		return err
	} else if !ok {
		return errNotFoundErr
	}
	comment := strings.TrimSpace(r.FormValue("comment"))
	err = h.withTx(ctx, func(c *ent.Client) error {
		cur, err := c.Bookmark.Query().
			Where(bookmark.UserIDEQ(u.ID), bookmark.TrackIDEQ(id)).
			Only(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return err
		}
		if cur != nil {
			return cur.Update().SetPosition(pos).SetComment(comment).Exec(ctx)
		}
		return c.Bookmark.Create().
			SetUserID(u.ID).
			SetTrackID(id).
			SetPosition(pos).
			SetComment(comment).
			Exec(ctx)
	})
	if err != nil {
		return err
	}
	writeOK(w, r, nil)
	return nil
}

func (h *Handler) deleteBookmark(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	id, err := idParam(r, kindTrack)
	if err != nil {
		return err
	}
	if _, err := h.ent.Bookmark.Delete().
		Where(bookmark.UserIDEQ(u.ID), bookmark.TrackIDEQ(id)).
		Exec(r.Context()); err != nil {
		return err
	}
	writeOK(w, r, nil)
	return nil
}

// savedQueue is the caller's stored queue reduced to playable entries, with
// the current index moved to match. ok is false when nothing is saved.
type savedQueue struct {
	q       *ent.PlayQueue
	entries []child
	current int // -1 when the current entry is gone
}

func (h *Handler) loadQueue(r *http.Request, u *ent.User) (*savedQueue, error) {
	ctx := r.Context()
	q, err := h.ent.PlayQueue.Query().
		Where(playqueue.HasUserWith(user.IDEQ(u.ID))).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ts, err := h.playableTracks(ctx, q.TrackIds)
	if err != nil {
		return nil, err
	}
	// Map the stored index onto the reduced list: walk the stored ids and
	// count the ones that survived.
	live := make(map[uint32]bool, len(ts))
	for _, t := range ts {
		live[t.ID] = true
	}
	current, kept := -1, 0
	for i, id := range q.TrackIds {
		if !live[id] {
			continue
		}
		if i == q.Current {
			current = kept
		}
		kept++
	}
	return &savedQueue{q: q, entries: songList(ts), current: current}, nil
}

func (h *Handler) getPlayQueue(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	sq, err := h.loadQueue(r, u)
	if err != nil || sq == nil {
		if err == nil {
			writeOK(w, r, nil)
		}
		return err
	}
	out := &playQueue{
		Position:  sq.q.Position,
		Username:  u.Email,
		Changed:   timestamp(sq.q.UpdateTime),
		ChangedBy: sq.q.ChangedBy,
		Entry:     sq.entries,
	}
	if sq.current >= 0 && sq.current < len(sq.entries) {
		out.Current = sq.entries[sq.current].ID
	} else {
		out.Position = 0
	}
	return h.respond(w, r, u, &body{PlayQueue: out})
}

func (h *Handler) getPlayQueueByIndex(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	sq, err := h.loadQueue(r, u)
	if err != nil || sq == nil {
		if err == nil {
			writeOK(w, r, nil)
		}
		return err
	}
	out := &playQueueByIndex{
		Position:  sq.q.Position,
		Username:  u.Email,
		Changed:   timestamp(sq.q.UpdateTime),
		ChangedBy: sq.q.ChangedBy,
		Entry:     sq.entries,
	}
	if sq.current >= 0 && sq.current < len(sq.entries) {
		cur := sq.current
		out.CurrentIndex = &cur
	} else {
		out.Position = 0
	}
	return h.respond(w, r, u, &body{PlayQueueByIndex: out})
}

// savePlayQueue stores the queue with current naming a track id; the first
// entry holding it becomes the current one.
func (h *Handler) savePlayQueue(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ids, err := h.trackIDsParam(r, "id")
	if err != nil {
		return err
	}
	current := 0
	if raw := r.FormValue("current"); raw != "" {
		kind, id, err := parseID(raw)
		if err != nil || kind != kindTrack {
			return errNotFoundErr
		}
		current = -1
		for i, t := range ids {
			if t == id {
				current = i
				break
			}
		}
		if current < 0 {
			return &apiError{
				Code:    errGeneric,
				Message: "current is not in the queue",
			}
		}
	}
	return h.storeQueue(w, r, u, ids, current)
}

func (h *Handler) savePlayQueueByIndex(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ids, err := h.trackIDsParam(r, "id")
	if err != nil {
		return err
	}
	current := 0
	if r.FormValue("currentIndex") != "" {
		current = intParam(r, "currentIndex", -1, -1, len(ids))
		if current < 0 || current >= len(ids) {
			return &apiError{
				Code:    errGeneric,
				Message: "currentIndex is out of range",
			}
		}
	}
	return h.storeQueue(w, r, u, ids, current)
}

// storeQueue overwrites the caller's queue; an empty one clears it.
func (h *Handler) storeQueue(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
	ids []uint32,
	current int,
) error {
	ctx := r.Context()
	var pos int64
	if raw := r.FormValue("position"); raw != "" {
		p, err := parseMillis(raw)
		if err != nil || p < 0 {
			return &apiError{
				Code:    errGeneric,
				Message: "position must be a non-negative number",
			}
		}
		pos = p
	}
	client := appaccess.SanitizeClient(r.FormValue("c"))
	err := h.withTx(ctx, func(c *ent.Client) error {
		if _, err := c.PlayQueue.Delete().
			Where(playqueue.HasUserWith(user.IDEQ(u.ID))).
			Exec(ctx); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		return c.PlayQueue.Create().
			SetUserID(u.ID).
			SetTrackIds(ids).
			SetCurrent(current).
			SetPosition(pos).
			SetChangedBy(client).
			Exec(ctx)
	})
	if err != nil {
		return err
	}
	writeOK(w, r, nil)
	return nil
}
