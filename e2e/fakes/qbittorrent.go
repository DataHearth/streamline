package fakes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// QBittorrentCredentials are what a spec must configure the download client
// with; the fake accepts nothing else.
const (
	QBUsername = "e2e"
	QBPassword = "e2e-secret"
)

// QBTorrent is one torrent the fake reports, with the files it holds. Sizes
// are what the adoption preview filters on, so they must clear
// library.MinEpisodeSize for a file that is meant to count.
type QBTorrent struct {
	Hash  string
	Name  string
	Files []QBFile
}

// QBFile is one file inside a QBTorrent, named by its path within the torrent.
type QBFile struct {
	Path string
	Size int64
}

// QBittorrent serves the read side of qBittorrent's WebUI API — login, the
// category-filtered torrent listing, and per-torrent file lists. That is
// everything the manual-torrent adoption pass and the pending-proposal preview
// consume, and nothing that writes: a spec that needs a real daemon's add /
// selection behavior wants containers.StartQBittorrent instead.
type QBittorrent struct {
	URL string

	torrents []QBTorrent
}

// NewQBittorrent starts the fake reporting torrents, all of them seeding and
// in the managed category. Shuts down via DeferCleanup.
func NewQBittorrent(torrents ...QBTorrent) *QBittorrent {
	q := &QBittorrent{torrents: torrents}
	mux := http.NewServeMux()

	mux.HandleFunc(
		"/api/v2/auth/login",
		func(w http.ResponseWriter, r *http.Request) {
			if r.FormValue("username") != QBUsername ||
				r.FormValue("password") != QBPassword {
				// A real qBittorrent answers 200 "Fails." with no SID cookie.
				ExpectWrite(w, "Fails.")
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "fake-sid"})
			ExpectWrite(w, "Ok.")
		},
	)

	mux.HandleFunc(
		"/api/v2/app/version",
		func(w http.ResponseWriter, _ *http.Request) {
			ExpectWrite(w, "v5.2.1")
		},
	)

	mux.HandleFunc(
		"/api/v2/torrents/info",
		func(w http.ResponseWriter, _ *http.Request) {
			out := make([]map[string]any, 0, len(q.torrents))
			for _, t := range q.torrents {
				var size int64
				for _, f := range t.Files {
					size += f.Size
				}
				out = append(out, map[string]any{
					"hash": t.Hash, "name": t.Name,
					// stalledUP maps to StatusSeeding, which is one of the two
					// states adoption considers.
					"state": "stalledUP", "progress": 1.0,
					"size": size, "save_path": "/downloads",
					"dlspeed": 0, "eta": 8640000,
				})
			}
			writeQBJSON(w, out)
		},
	)

	mux.HandleFunc(
		"/api/v2/torrents/files",
		func(w http.ResponseWriter, r *http.Request) {
			hash := r.URL.Query().Get("hash")
			out := make([]map[string]any, 0)
			for _, t := range q.torrents {
				if t.Hash != hash {
					continue
				}
				for i, f := range t.Files {
					out = append(out, map[string]any{
						"index": i, "name": f.Path,
						"size": f.Size, "priority": 1,
					})
				}
			}
			writeQBJSON(w, out)
		},
	)

	srv := httptest.NewServer(mux)
	DeferCleanup(srv.Close)
	q.URL = srv.URL
	return q
}

// ExpectWrite writes a plain-text body, failing the spec if the write does.
func ExpectWrite(w http.ResponseWriter, body string) {
	GinkgoHelper()
	_, err := w.Write([]byte(body))
	Expect(err).NotTo(HaveOccurred())
}

func writeQBJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	Expect(json.NewEncoder(w).Encode(v)).To(Succeed())
}
