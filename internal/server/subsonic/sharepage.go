package subsonic

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/share"
)

// SharePage serves share links to anyone holding one, with no account: the
// page at /share/{token}, and the audio and covers it embeds. Each request
// re-reads the share, so a deleted or expired link stops working at once,
// and a visitor reaches only the tracks the share expands to.
func (h *Handler) SharePage() http.Handler {
	r := chi.NewRouter()
	r.Get("/{token}", h.servePage)
	r.Get("/{token}/stream/{track}", h.serveSharedTrack)
	r.Get("/{token}/cover/{album}", h.serveSharedCover)
	return r
}

// openShare loads a live share and the tracks it plays. A missing or expired
// share is a plain 404, the same for both, so a link reveals nothing.
func (h *Handler) openShare(
	w http.ResponseWriter,
	r *http.Request,
) (*ent.Share, []*ent.Track, bool) {
	ctx := r.Context()
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	s, err := h.ent.Share.Query().
		Where(share.TokenEQ(chi.URLParam(r, "token"))).
		WithOwner().
		Only(ctx)
	if err != nil || expired(s, time.Now()) {
		if err != nil && !ent.IsNotFound(err) {
			slog.ErrorContext(ctx, "load share", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return nil, nil, false
		}
		http.NotFound(w, r)
		return nil, nil, false
	}
	ts, err := h.expandShare(ctx, s)
	if err != nil {
		slog.ErrorContext(ctx, "expand share", "share.id", s.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, nil, false
	}
	return s, ts, true
}

func pathID(r *http.Request, name string) (uint32, bool) {
	n, err := strconv.ParseUint(chi.URLParam(r, name), 10, 32)
	return uint32(n), err == nil
}

func (h *Handler) serveSharedTrack(w http.ResponseWriter, r *http.Request) {
	_, ts, ok := h.openShare(w, r)
	if !ok {
		return
	}
	id, ok := pathID(r, "track")
	if !ok {
		http.NotFound(w, r)
		return
	}
	for _, t := range ts {
		if t.ID == id {
			if err := serveFile(w, r, firstFile(t), false); err != nil {
				http.NotFound(w, r)
			}
			return
		}
	}
	http.NotFound(w, r)
}

func (h *Handler) serveSharedCover(w http.ResponseWriter, r *http.Request) {
	_, ts, ok := h.openShare(w, r)
	if !ok {
		return
	}
	id, ok := pathID(r, "album")
	if !ok {
		http.NotFound(w, r)
		return
	}
	for _, t := range ts {
		if t.Edges.Album.ID == id {
			h.posters.Serve(w, r, "albums", id)
			return
		}
	}
	http.NotFound(w, r)
}

type pageTrack struct {
	ID       uint32
	Title    string
	Artist   string
	Album    string
	AlbumID  uint32
	Duration string
}

type pageData struct {
	Title       string
	Description string
	Owner       string
	Expires     string
	CoverAlbum  uint32
	Token       string
	Tracks      []pageTrack
}

func clock(sec uint32) string {
	return strconv.Itoa(int(sec/60)) + ":" + twoDigits(int(sec%60))
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// shareTitle names the page after what was shared: the single album or
// artist when the share is one of those, else the description.
func shareTitle(s *ent.Share, ts []*ent.Track) string {
	if s.Description != "" {
		return s.Description
	}
	if len(ts) == 0 {
		return "Shared music"
	}
	first := ts[0].Edges.Album
	for _, t := range ts {
		if t.Edges.Album.ID != first.ID {
			return "Shared music"
		}
	}
	return first.Title + " — " + first.Edges.Artist.Name
}

func (h *Handler) servePage(w http.ResponseWriter, r *http.Request) {
	s, ts, ok := h.openShare(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if err := s.Update().
		AddVisitCount(1).
		SetLastVisitedAt(time.Now()).
		Exec(context.WithoutCancel(ctx)); err != nil {
		slog.WarnContext(ctx, "count share visit", "share.id", s.ID, "error", err)
	}

	data := pageData{
		Title:       shareTitle(s, ts),
		Description: s.Description,
		Owner:       ownerName(s.Edges.Owner),
		Token:       s.Token,
		Tracks:      make([]pageTrack, 0, len(ts)),
	}
	if s.ExpiresAt != nil {
		data.Expires = s.ExpiresAt.UTC().Format("2 January 2006")
	}
	for _, t := range ts {
		song := songOf(t)
		data.Tracks = append(data.Tracks, pageTrack{
			ID:       t.ID,
			Title:    song.Title,
			Artist:   song.Artist,
			Album:    song.Album,
			AlbumID:  t.Edges.Album.ID,
			Duration: clock(song.Duration),
		})
	}
	if len(ts) > 0 {
		data.CoverAlbum = ts[0].Edges.Album.ID
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := pageTmpl.Execute(w, data); err != nil {
		slog.DebugContext(ctx, "write share page", "error", err)
	}
}

// ownerName is what the page shows of the sharer: their display name, never
// their login email.
func ownerName(u *ent.User) string {
	if u == nil || u.DisplayName == "" {
		return ""
	}
	return u.DisplayName
}

// The page carries no script: the site CSP allows none inline, and native
// audio controls are all a listener needs.
var pageTmpl = template.Must(template.New("share").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>{{.Title}}</title>
<style>
:root { color-scheme: light dark; --bg: #f6f6f4; --fg: #1c1c1a; --muted: #6b6b66; --card: #ffffff; --line: #e3e3de; }
@media (prefers-color-scheme: dark) { :root { --bg: #141413; --fg: #ecece8; --muted: #9b9b95; --card: #1f1f1d; --line: #2e2e2b; } }
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--fg); font: 15px/1.45 system-ui, -apple-system, "Segoe UI", sans-serif; }
main { max-width: 720px; margin: 0 auto; padding: 32px 16px 48px; }
header { display: flex; gap: 20px; align-items: center; margin-bottom: 28px; }
header img { width: 132px; height: 132px; border-radius: 8px; object-fit: cover; background: var(--line); flex: none; }
h1 { font-size: 1.5rem; margin: 0 0 6px; overflow-wrap: anywhere; }
.meta { color: var(--muted); font-size: .9rem; }
ol { list-style: none; margin: 0; padding: 0; }
li { background: var(--card); border: 1px solid var(--line); border-radius: 8px; padding: 12px 14px; margin-bottom: 10px; }
.row { display: flex; justify-content: space-between; gap: 12px; }
.title { font-weight: 600; overflow-wrap: anywhere; }
.sub { color: var(--muted); font-size: .85rem; }
.dur { color: var(--muted); font-variant-numeric: tabular-nums; font-size: .85rem; }
audio { width: 100%; margin-top: 8px; height: 36px; }
footer { margin-top: 32px; color: var(--muted); font-size: .8rem; text-align: center; }
@media (max-width: 480px) { header { flex-direction: column; align-items: flex-start; } }
</style>
</head>
<body>
<main>
<header>
{{if .CoverAlbum}}<img src="/share/{{.Token}}/cover/{{.CoverAlbum}}" alt="">{{end}}
<div>
<h1>{{.Title}}</h1>
<div class="meta">{{len .Tracks}} track{{if ne (len .Tracks) 1}}s{{end}}{{if .Owner}} · shared by {{.Owner}}{{end}}{{if .Expires}} · until {{.Expires}}{{end}}</div>
</div>
</header>
{{if .Tracks}}<ol>
{{range .Tracks}}<li>
<div class="row"><div><div class="title">{{.Title}}</div><div class="sub">{{.Artist}} — {{.Album}}</div></div><div class="dur">{{.Duration}}</div></div>
<audio controls preload="none" src="/share/{{$.Token}}/stream/{{.ID}}"></audio>
</li>
{{end}}</ol>{{else}}<p class="meta">Nothing in this share can be played right now.</p>{{end}}
<footer>Shared from Streamline</footer>
</main>
</body>
</html>
`))
