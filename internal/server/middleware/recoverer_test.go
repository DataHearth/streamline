package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Recoverer middleware", Label("unit", "server"), func() {
	var capturedLogs bytes.Buffer

	BeforeEach(func() {
		capturedLogs.Reset()
		GinkgoWriter.TeeTo(&capturedLogs)
		DeferCleanup(GinkgoWriter.ClearTeeWriters)
	})

	It("converts a string panic into a 500 and logs the message", func() {
		h := Recoverer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("boom")
			}),
		)

		req := httptest.NewRequest("GET", "/widgets", nil)
		rec := httptest.NewRecorder()
		Expect(func() { h.ServeHTTP(rec, req) }).ToNot(Panic())
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))

		Expect(capturedLogs.String()).To(SatisfyAll(
			ContainSubstring("panic recovered in HTTP handler"),
			ContainSubstring("level=ERROR+4"),
			ContainSubstring("exception.message=boom"),
			ContainSubstring("http.request.method=GET"),
			ContainSubstring("url.path=/widgets"),
		))
	})

	It("converts an error panic into a 500 and logs the error message", func() {
		h := Recoverer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic(http.ErrBodyNotAllowed)
			}),
		)

		req := httptest.NewRequest("POST", "/x", nil)
		rec := httptest.NewRecorder()
		Expect(func() { h.ServeHTTP(rec, req) }).ToNot(Panic())
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))

		Expect(capturedLogs.String()).To(SatisfyAll(
			ContainSubstring("panic recovered in HTTP handler"),
			ContainSubstring(http.ErrBodyNotAllowed.Error()),
		))
	})

	It("re-panics on http.ErrAbortHandler sentinel without logging", func() {
		h := Recoverer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic(http.ErrAbortHandler)
			}),
		)

		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		Expect(func() { h.ServeHTTP(rec, req) }).
			To(PanicWith(http.ErrAbortHandler))

		Expect(capturedLogs.String()).ToNot(ContainSubstring("panic recovered"))
	})

	It("passes through when handler does not panic", func() {
		h := Recoverer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			}),
		)

		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		Expect(rec.Code).To(Equal(http.StatusTeapot))
		Expect(capturedLogs.String()).ToNot(ContainSubstring("panic recovered"))
	})
})
