package middleware

import (
	"log/slog"
	"net/http"
)

// defaultMaxBody caps every request body the app accepts. Every JSON schema in
// api/openapi.yaml is a small object (creates, patches, a SearchResult being
// grabbed), so 1 MiB is compatibility headroom rather than a working size: what
// it buys is that json.Decode's single heap allocation is bounded at all.
const defaultMaxBody = 1 << 20

// torrentMaxBody is the carve-out for POST /api/v1/torrents, whose body carries
// a base64 .torrent file. The domain's own ceiling for a .torrent is 16 MiB
// (download.maxTorrentFileSize) and base64 inflates by 4/3, so ~21.4 MiB plus
// JSON framing has to fit or the fix would silently break a documented API
// capability.
//
// The decode happens before roleGuard in the generated strict handler, so on
// this one route any authenticated principal (not just admin) can make the
// process allocate up to this much. Bounded, and accepted.
const torrentMaxBody = 24 << 20

const addTorrentPath = "/api/v1/torrents"

// BodyLimit bounds the request body at two points, because either one alone
// leaves a hole: a declared Content-Length over the cap is refused outright
// with 413 before any handler runs, and the body is wrapped in
// http.MaxBytesReader so a chunked or under-declared body is cut at the same
// cap mid-read. The second half surfaces to the caller as a decode error, which
// restapi and internal/server/web both map back to 413.
//
// It runs pre-routing, so the torrent carve-out matches on the raw path. chi
// redirects a trailing slash, so an exact match is the whole of it.
func BodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(defaultMaxBody)
		if r.Method == http.MethodPost && r.URL.Path == addTorrentPath {
			limit = torrentMaxBody
		}

		if r.ContentLength > limit {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			if _, err := w.Write(
				[]byte(`{"message":"request body too large"}`),
			); err != nil {
				slog.ErrorContext(
					r.Context(), "body limit write failed", "error", err,
				)
			}
			return
		}

		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}
