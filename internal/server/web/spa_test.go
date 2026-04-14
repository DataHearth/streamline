package web

import (
	"net/http"
	"net/http/httptest"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = g.Describe("SPA shell handler", g.Label("unit"), func() {
	var h *Handler

	g.BeforeEach(func() {
		h = &Handler{}
	})

	g.DescribeTable("serves the embedded shell on /app and /app/*",
		func(path string) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()

			h.SPAShell(rr, req)

			Expect(rr.Code).To(Equal(http.StatusOK))
			Expect(rr.Header().Get("Content-Type")).To(HavePrefix("text/html"))
			Expect(rr.Header().Get("Cache-Control")).To(Equal("no-store"))
			Expect(strings.Contains(rr.Body.String(), `id="app"`)).To(BeTrue())
		},
		g.Entry("/app", "/app"),
		g.Entry("/app/movies", "/app/movies"),
		g.Entry("/app/movies/42", "/app/movies/42"),
		g.Entry("/app/settings/general", "/app/settings/general"),
	)
})
