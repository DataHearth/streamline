package middleware

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

// serveBody runs a request through BodyLimit with a declared Content-Length.
func serveBody(
	sink *bodySink,
	method, path string,
	size int,
) *httptest.ResponseRecorder {
	GinkgoHelper()
	req := httptest.NewRequest(
		method, path, bytes.NewReader(bytes.Repeat([]byte("a"), size)),
	)
	req.ContentLength = int64(size)
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
			Expect(
				rec.Body.String(),
			).To(Equal(`{"message":"request body too large"}`))
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
	})
})
