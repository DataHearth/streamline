package middleware

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

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

// uploadIdleTimeout is how far every Read of an upload body pushes the
// connection's deadlines: an upload may take as long as it keeps moving.
const uploadIdleTimeout = 2 * time.Minute

// uploadDrainMax is how much of a refused upload is read and thrown away. A
// handler that answers with the body unread makes the server close the
// connection under a client still sending, and a browser then reports a network
// error instead of the refusal. A track fits; a bigger upload learns the same
// refusal from its preview, before sending anything.
const uploadDrainMax = 256 << 20

// uploadRoute matches the four multipart upload routes, not their previews.
var uploadRoute = regexp.MustCompile(
	`^/api/v1/(music/(albums|tracks|artists)|books)/[0-9]+/upload$`,
)

type uploadLengthKey struct{}

// UploadLength is the declared Content-Length of an upload request, -1 when
// undeclared or when the request is not an upload.
func UploadLength(ctx context.Context) int64 {
	if n, ok := ctx.Value(uploadLengthKey{}).(int64); ok {
		return n
	}
	return -1
}

// idleBody pushes the read and write deadlines ahead on every Read, the
// read-side twin of the Subsonic stream's idleWriter. The write half matters
// as much: writeTimeout is armed when the headers are read, so without it a
// long upload could not be answered.
type idleBody struct {
	io.ReadCloser
	rc *http.ResponseController
	// The first error stays: a read after a timeout would push the deadline again,
	// and the drain would hold a stalled client for a second idle period.
	err error
}

func (b *idleBody) Read(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	deadline := time.Now().Add(uploadIdleTimeout)
	if err := b.rc.SetReadDeadline(deadline); err != nil {
		slog.DebugContext(
			context.Background(),
			"upload read deadline not set",
			"error",
			err,
		)
	}
	if err := b.rc.SetWriteDeadline(deadline); err != nil {
		slog.DebugContext(
			context.Background(),
			"upload write deadline not set",
			"error",
			err,
		)
	}
	n, err := b.ReadCloser.Read(p)
	b.err = err
	return n, err
}

func isMemberUpload(r *http.Request) bool {
	if r.Method != http.MethodPost || !uploadRoute.MatchString(routePath(r)) {
		return false
	}
	claims := auth.ClaimsFromContext(r.Context())
	return claims != nil && auth.RoleAtLeast(claims.Role, "member")
}

// BodyTooLargeJSON is the 413 body, shared with restapi so a caller sees the
// same answer whichever half of the limit tripped. The code matters because
// which half trips is a matter of framing, not of what the caller did wrong: a
// declared Content-Length is refused here, an undeclared or chunked one is cut
// mid-decode and answered downstream. A client branching on the reason must not
// have to care which.
const BodyTooLargeJSON = `{"message":"request body too large","code":"body_too_large"}`

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
		if isMemberUpload(r) {
			ctx := context.WithValue(r.Context(), uploadLengthKey{}, r.ContentLength)
			r = r.WithContext(ctx)
			if r.Body != nil {
				r.Body = &idleBody{
					ReadCloser: r.Body,
					rc:         http.NewResponseController(w),
				}
			}
			next.ServeHTTP(w, r)
			if r.Body != nil {
				if _, err := io.CopyN(
					io.Discard,
					r.Body,
					uploadDrainMax,
				); err != nil &&
					!errors.Is(err, io.EOF) {
					slog.DebugContext(
						r.Context(),
						"drain refused upload",
						"error",
						err,
					)
				}
			}
			return
		}

		limit := int64(defaultMaxBody)
		if r.Method == http.MethodPost && routePath(r) == addTorrentPath &&
			auth.IsAdmin(r.Context()) {
			limit = torrentMaxBody
		}

		if r.ContentLength > limit {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			if _, err := w.Write(
				[]byte(BodyTooLargeJSON),
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
