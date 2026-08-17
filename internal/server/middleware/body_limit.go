package middleware

import (
	"log/slog"
	"net/http"

	"github.com/datahearth/streamline/internal/auth"
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
// It is granted only to an admin caller. The generated strict handler decodes
// the body before roleGuard runs, so the raised cap is reachable before anyone
// has checked the role: without this gate every authenticated principal, down
// to request_only, could make the process buffer the JSON and its base64
// decode — tens of MiB per in-flight request — only to be told 403 afterwards.
// AddTorrent is admin-only, so a non-admin has no reason to send a body this
// size at all, and the default cap is the honest answer for them.
const torrentMaxBody = 24 << 20

const addTorrentPath = "/api/v1/torrents"

// BodyLimit bounds the request body at two points, because either one alone
// leaves a hole: a declared Content-Length over the cap is refused outright
// with 413 before any handler runs, and the body is wrapped in
// http.MaxBytesReader so a chunked or under-declared body is cut at the same
// cap mid-read. The second half surfaces to the caller as a decode error, which
// restapi and internal/server/web both map back to 413.
//
// It runs pre-routing, so the torrent carve-out has to resolve the path the
// same way the router will — see routePath. chi redirects a trailing slash, so
// an exact match is the whole of it. It also
// runs after the auth middleware, which is what puts the claims the carve-out
// reads in the context — and what keeps an anonymous caller on the 401 it gets
// today rather than telling it the ceiling.
func BodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(defaultMaxBody)
		if r.Method == http.MethodPost && routePath(r) == addTorrentPath &&
			isAdmin(r) {
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

// routePath resolves the path chi will route on, which is RawPath whenever the
// URL was escaped and Path otherwise (chi's Mux.ServeHTTP). Reading the decoded
// Path here instead would key this middleware off a different string than the
// router: /api/v1/%74orrents decodes to the carve-out path but routes to a 404,
// so the raised cap would apply to a request the router never sends there.
func routePath(r *http.Request) string {
	if r.URL.RawPath != "" {
		return r.URL.RawPath
	}
	return r.URL.Path
}

// isAdmin reports whether the request carries admin claims. A request with no
// claims at all is not an admin: the anonymous case is the auth middleware's to
// answer, and it already has by the time this runs.
func isAdmin(r *http.Request) bool {
	c := auth.ClaimsFromContext(r.Context())
	return c != nil && auth.RoleAtLeast(c.Role, "admin")
}
