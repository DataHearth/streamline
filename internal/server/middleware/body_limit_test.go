package middleware

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing/iotest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/auth"
)

const (
	torrentPath = "/api/v1/torrents"
	// plainPath stands for any route without the torrent carve-out.
	plainPath = "/api/v1/movies"
)

// bodySink is the downstream handler under test: it records whether it ran,
// how much of the body it managed to read, and the error reading stopped on.
type bodySink struct {
	called  bool
	read    int64
	readErr error
}

func (s *bodySink) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.called = true
		b, err := io.ReadAll(r.Body)
		s.read = int64(len(b))
		s.readErr = err
	})
}

// serveBody runs a request through BodyLimit with a declared Content-Length,
// as an admin. Admin is the default because the torrent carve-out is only
// granted to one, and every other spec is indifferent to the role.
func serveBody(
	sink *bodySink,
	method, path string,
	size int,
) *httptest.ResponseRecorder {
	GinkgoHelper()
	return serveBodyAs(sink, method, path, size, &auth.Claims{Role: "admin"})
}

// serveBodyAs is serveBody with an explicit caller. A nil claims argument is
// the anonymous case, which reaches this middleware only on a route the auth
// middleware excludes.
func serveBodyAs(
	sink *bodySink,
	method, path string,
	size int,
	claims *auth.Claims,
) *httptest.ResponseRecorder {
	GinkgoHelper()
	req := httptest.NewRequest(
		method, path, bytes.NewReader(bytes.Repeat([]byte("a"), size)),
	)
	req.ContentLength = int64(size)
	if claims != nil {
		req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))
	}
	rec := httptest.NewRecorder()
	BodyLimit(sink.handler()).ServeHTTP(rec, req)
	return rec
}

var _ = Describe("BodyLimit middleware", Label("unit", "server"), func() {
	var sink *bodySink

	BeforeEach(func() {
		sink = &bodySink{}
	})

	Context("declared Content-Length", func() {
		It("refuses an over-cap body with 413 JSON, handler unreached", func() {
			rec := serveBody(sink, http.MethodPost, plainPath, defaultMaxBody+1)

			Expect(rec.Code).To(Equal(http.StatusRequestEntityTooLarge))
			Expect(
				rec.Header().Get("Content-Type"),
			).To(Equal("application/json"))
			Expect(rec.Body.String()).To(Equal(BodyTooLargeJSON))
			// Pinned literally, not via the constant alone: the SPA maps this
			// code to its own sentence (web/app/lib/api.ts BY_CODE), so
			// renaming it is an API change, not an internal one.
			Expect(rec.Body.String()).To(ContainSubstring(`"code":"body_too_large"`))
			Expect(sink.called).To(BeFalse())
		})

		It("passes a body exactly at the cap through intact", func() {
			rec := serveBody(sink, http.MethodPost, plainPath, defaultMaxBody)

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(sink.called).To(BeTrue())
			Expect(sink.readErr).ToNot(HaveOccurred())
			Expect(sink.read).To(Equal(int64(defaultMaxBody)))
		})
	})

	// The chunked shape: nothing to reject up front, so the cut has to happen
	// inside the read and surface as an error the decoder can recognise.
	It("cuts an undeclared body at the cap and reports MaxBytesError", func() {
		req := httptest.NewRequest(
			http.MethodPost,
			plainPath,
			bytes.NewReader(bytes.Repeat([]byte("a"), defaultMaxBody+4096)),
		)
		req.ContentLength = -1
		rec := httptest.NewRecorder()

		BodyLimit(sink.handler()).ServeHTTP(rec, req)

		Expect(sink.called).To(BeTrue())
		var tooLarge *http.MaxBytesError
		Expect(errors.As(sink.readErr, &tooLarge)).To(BeTrue())
		Expect(sink.read).To(BeNumerically("<=", int64(defaultMaxBody)))
	})

	It("leaves a bodyless GET untouched", func() {
		req := httptest.NewRequest(http.MethodGet, plainPath, nil)
		rec := httptest.NewRecorder()

		BodyLimit(sink.handler()).ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(sink.called).To(BeTrue())
		Expect(sink.readErr).ToNot(HaveOccurred())
		Expect(sink.read).To(BeZero())
	})

	Context("the POST /api/v1/torrents carve-out", func() {
		const betweenCaps = defaultMaxBody + 1

		It("accepts a base64 .torrent sized body", func() {
			rec := serveBody(sink, http.MethodPost, torrentPath, betweenCaps)

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(sink.called).To(BeTrue())
			Expect(sink.read).To(Equal(int64(betweenCaps)))
		})

		It("still refuses a body over the torrent cap", func() {
			rec := serveBody(
				sink, http.MethodPost, torrentPath, torrentMaxBody+1,
			)

			Expect(rec.Code).To(Equal(http.StatusRequestEntityTooLarge))
			Expect(sink.called).To(BeFalse())
		})

		It("does not extend the carve-out to another route", func() {
			rec := serveBody(sink, http.MethodPost, plainPath, betweenCaps)

			Expect(rec.Code).To(Equal(http.StatusRequestEntityTooLarge))
			Expect(sink.called).To(BeFalse())
		})

		It("does not extend the carve-out to another method", func() {
			rec := serveBody(sink, http.MethodPut, torrentPath, betweenCaps)

			Expect(rec.Code).To(Equal(http.StatusRequestEntityTooLarge))
			Expect(sink.called).To(BeFalse())
		})

		// AddTorrent is admin-only, but the generated strict handler decodes
		// the body before roleGuard runs. Were the carve-out granted on the
		// path alone, these callers could make the process buffer the JSON and
		// its base64 decode before anything checked the role.
		DescribeTable("does not extend the carve-out to a non-admin caller",
			func(claims *auth.Claims) {
				rec := serveBodyAs(
					sink, http.MethodPost, torrentPath, betweenCaps, claims,
				)

				Expect(rec.Code).To(Equal(http.StatusRequestEntityTooLarge))
				Expect(sink.called).To(BeFalse())
			},
			Entry("member", &auth.Claims{Role: "member"}),
			Entry("request_only", &auth.Claims{Role: "request_only"}),
			Entry("no claims", nil),
		)

		// chi routes on RawPath when the URL was escaped, so an escaped
		// spelling reaches a 404 rather than AddTorrent. Matching the decoded
		// path here would hand the raised cap to a request the router never
		// sends to the torrent handler at all.
		It("does not extend the carve-out to an escaped spelling", func() {
			rec := serveBody(
				sink, http.MethodPost, "/api/v1/%74orrents", betweenCaps,
			)

			Expect(rec.Code).To(Equal(http.StatusRequestEntityTooLarge))
			Expect(sink.called).To(BeFalse())
		})

		It("still holds a non-admin to the default cap, not to nothing", func() {
			rec := serveBodyAs(
				sink, http.MethodPost, torrentPath, defaultMaxBody,
				&auth.Claims{Role: "member"},
			)

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(sink.read).To(Equal(int64(defaultMaxBody)))
		})
	})

	Context("the upload carve-out", func() {
		const (
			uploadPath = "/api/v1/music/albums/7/upload"
			bigUpload  = 50 << 20
		)

		It("passes a member's upload over the default cap", func() {
			rec := serveBodyAs(
				sink, http.MethodPost, uploadPath, bigUpload,
				&auth.Claims{Role: "member"},
			)

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(sink.read).To(Equal(int64(bigUpload)))
		})

		DescribeTable("keeps the default cap for everyone else",
			func(claims *auth.Claims) {
				rec := serveBodyAs(
					sink, http.MethodPost, uploadPath, bigUpload, claims,
				)

				Expect(rec.Code).To(Equal(http.StatusRequestEntityTooLarge))
				Expect(sink.called).To(BeFalse())
			},
			Entry("request_only", &auth.Claims{Role: "request_only"}),
			Entry("anonymous", nil),
		)

		It("keeps the cap on the preview", func() {
			rec := serveBodyAs(
				sink, http.MethodPost, uploadPath+"/preview", bigUpload,
				&auth.Claims{Role: "member"},
			)

			Expect(rec.Code).To(Equal(http.StatusRequestEntityTooLarge))
		})

		It("hands the declared length to the handler", func() {
			var got int64
			req := httptest.NewRequest(
				http.MethodPost, uploadPath, bytes.NewReader([]byte("abc")),
			)
			req = req.WithContext(
				auth.ContextWithClaims(req.Context(), &auth.Claims{Role: "member"}),
			)
			BodyLimit(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = UploadLength(r.Context())
			})).ServeHTTP(httptest.NewRecorder(), req)

			Expect(got).To(Equal(int64(3)))
			Expect(UploadLength(context.Background())).To(Equal(int64(-1)))
		})

		It("keeps reading when the connection takes no deadlines", func() {
			rw := &deadlineRecorder{
				ResponseRecorder: httptest.NewRecorder(),
				err:              http.ErrNotSupported,
			}
			req := httptest.NewRequest(
				http.MethodPost, uploadPath, bytes.NewReader([]byte("abc")),
			)
			req = req.WithContext(
				auth.ContextWithClaims(req.Context(), &auth.Claims{Role: "member"}),
			)
			var read []byte
			BodyLimit(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				var err error
				read, err = io.ReadAll(r.Body)
				Expect(err).ToNot(HaveOccurred())
			})).ServeHTTP(rw, req)

			Expect(string(read)).To(Equal("abc"))
		})

		It("pushes both deadlines ahead on every Read", func() {
			rw := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
			req := httptest.NewRequest(
				http.MethodPost, uploadPath, bytes.NewReader([]byte("abcdef")),
			)
			req = req.WithContext(
				auth.ContextWithClaims(req.Context(), &auth.Claims{Role: "member"}),
			)
			var reads, writes int
			BodyLimit(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				buf := make([]byte, 2)
				for range 3 {
					_, err := r.Body.Read(buf)
					Expect(err).ToNot(HaveOccurred())
				}
				reads, writes = rw.reads, rw.writes
			})).ServeHTTP(rw, req)

			Expect(reads).To(Equal(3))
			Expect(writes).To(Equal(3))
			Expect(
				rw.lastRead,
			).To(BeTemporally("~", time.Now().Add(2*time.Minute), 5*time.Second))
		})

		It("does not read again after a failed read", func() {
			rw := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
			req := httptest.NewRequest(
				http.MethodPost,
				uploadPath,
				iotest.TimeoutReader(
					iotest.OneByteReader(bytes.NewReader([]byte("abcdef"))),
				),
			)
			req = req.WithContext(
				auth.ContextWithClaims(req.Context(), &auth.Claims{Role: "member"}),
			)
			var during int
			BodyLimit(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				_, err := io.ReadAll(r.Body)
				Expect(err).To(MatchError(iotest.ErrTimeout))
				during = rw.reads
			})).ServeHTTP(rw, req)

			Expect(rw.reads).To(Equal(during))
		})

		It("reads what a refusing handler left unread", func() {
			rw := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
			body := bytes.NewReader([]byte("abcdef"))
			req := httptest.NewRequest(http.MethodPost, uploadPath, body)
			req = req.WithContext(
				auth.ContextWithClaims(req.Context(), &auth.Claims{Role: "member"}),
			)
			BodyLimit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusConflict)
			})).ServeHTTP(rw, req)

			Expect(rw.Code).To(Equal(http.StatusConflict))
			Expect(body.Len()).To(BeZero())
		})
	})
})

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	reads, writes int
	lastRead      time.Time
	err           error
}

func (d *deadlineRecorder) SetReadDeadline(t time.Time) error {
	d.reads++
	d.lastRead = t
	return d.err
}

func (d *deadlineRecorder) SetWriteDeadline(time.Time) error {
	d.writes++
	return d.err
}
