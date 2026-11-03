package subsonic

import (
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/track"
)

// nowPlayingTTL bounds how long an entry outlives its track when no final
// scrobble removes it (a player closed mid-song), and is the whole lifetime of
// an entry whose track has no known duration.
const nowPlayingTTL = 10 * time.Minute

// maxPlayersPerUser bounds one account's entries: a client naming itself
// differently on every call would otherwise grow the map without end.
const maxPlayersPerUser = 8

type playingKey struct {
	userID uint32
	player string
}

type playingEntry struct {
	username string
	trackID  uint32
	since    time.Time
	until    time.Time
}

// nowPlaying is what scrobble submission=false reports, in memory only: one
// entry per user and client, gone on restart.
type nowPlaying struct {
	mu      sync.Mutex
	entries map[playingKey]playingEntry
	now     func() time.Time
}

func newNowPlaying() *nowPlaying {
	return &nowPlaying{entries: map[playingKey]playingEntry{}, now: time.Now}
}

func (n *nowPlaying) set(u *ent.User, player string, trackID, durationSec uint32) {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := n.now()
	n.prune(now)
	k := playingKey{u.ID, player}
	if _, ok := n.entries[k]; !ok {
		n.evictOldest(u.ID, maxPlayersPerUser-1)
	}
	n.entries[k] = playingEntry{
		username: u.Email,
		trackID:  trackID,
		since:    now,
		until:    now.Add(time.Duration(durationSec)*time.Second + nowPlayingTTL),
	}
}

// prune drops expired entries. The caller holds mu.
func (n *nowPlaying) prune(now time.Time) {
	for k, e := range n.entries {
		if now.After(e.until) {
			delete(n.entries, k)
		}
	}
}

// evictOldest drops userID's oldest entries until at most keep remain. The
// caller holds mu.
func (n *nowPlaying) evictOldest(userID uint32, keep int) {
	var mine []playingKey
	for k := range n.entries {
		if k.userID == userID {
			mine = append(mine, k)
		}
	}
	sort.Slice(mine, func(i, j int) bool {
		return n.entries[mine[i]].since.Before(n.entries[mine[j]].since)
	})
	for len(mine) > keep {
		delete(n.entries, mine[0])
		mine = mine[1:]
	}
}

// done drops the entry a final scrobble of the same track closes.
func (n *nowPlaying) done(userID uint32, player string, trackID uint32) {
	n.mu.Lock()
	defer n.mu.Unlock()
	k := playingKey{userID, player}
	if e, ok := n.entries[k]; ok && e.trackID == trackID {
		delete(n.entries, k)
	}
}

type playingSnapshot struct {
	playingKey
	playingEntry
}

func (n *nowPlaying) list() []playingSnapshot {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.prune(n.now())
	out := make([]playingSnapshot, 0, len(n.entries))
	for k, e := range n.entries {
		out = append(out, playingSnapshot{k, e})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].since.After(out[j].since) })
	return out
}

func (h *Handler) getNowPlaying(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	out := &nowPlayingList{Entry: []nowPlayingEntry{}}
	now := h.playing.now()
	for i, e := range h.playing.list() {
		t, err := songEdges(h.ent.Track.Query().
			Where(track.IDEQ(e.trackID), track.HasMediaFilesWith(mediafile.MissingSinceIsNil())),
		).
			Only(r.Context())
		if ent.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		if firstFile(t) == nil {
			continue
		}
		al := t.Edges.Album
		out.Entry = append(out.Entry, nowPlayingEntry{
			child:      toSong(t, al, al.Edges.Artist, firstFile(t)),
			Username:   e.username,
			MinutesAgo: int(now.Sub(e.since).Minutes()),
			PlayerID:   i + 1,
			PlayerName: e.player,
		})
	}
	return h.respond(w, r, u, &body{NowPlaying: out})
}
