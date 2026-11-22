package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	webassets "github.com/datahearth/streamline/web"
)

var _ = g.Describe("static assets", g.Label("unit"), func() {
	var files http.Handler

	g.BeforeEach(func() {
		sub, err := fs.Sub(webassets.Assets, "static")
		Expect(err).ToNot(HaveOccurred())
		files = http.StripPrefix(
			"/static/",
			staticFiles(http.FileServer(http.FS(sub))),
		)
	})

	get := func(target, ifNoneMatch string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		rr := httptest.NewRecorder()
		files.ServeHTTP(rr, req)
		return rr
	}

	g.It("links the SPA's stylesheets and script by their content", func() {
		shell := string(static.spaShell)
		for _, file := range []string{
			"css/style.css", "dist/spa.min.css", "dist/spa.min.js",
		} {
			Expect(static.versions).To(HaveKey(file))
			Expect(shell).To(ContainSubstring(
				`"/static/` + file + "?v=" + static.versions[file] + `"`,
			))
		}
		Expect(
			shell,
		).To(ContainSubstring(`href="/static/fonts/Inter-Regular.woff2"`))
	})

	g.It("links the API docs' bundle by its content", func() {
		Expect(string(static.apiDocs)).To(ContainSubstring(
			`"/static/js/docs.min.js?v=` + static.versions["js/docs.min.js"] + `"`,
		))
	})

	g.It("keeps a versioned file for good", func() {
		v := static.versions["dist/spa.min.js"]
		rr := get("/static/dist/spa.min.js?v="+v, "")

		Expect(rr.Code).To(Equal(http.StatusOK))
		Expect(rr.Header().Get("Cache-Control")).
			To(Equal("public, max-age=31536000, immutable"))
		Expect(rr.Header().Get("ETag")).To(Equal(`"` + v + `"`))
	})

	g.DescribeTable(
		"revalidates any other request",
		func(target string) {
			rr := get(target, "")

			Expect(rr.Code).To(Equal(http.StatusOK))
			Expect(rr.Header().Get("Cache-Control")).To(Equal("no-cache"))
			Expect(rr.Header().Get("ETag")).ToNot(BeEmpty())
		},
		g.Entry("no version", "/static/dist/spa.min.js"),
		g.Entry(
			"an earlier release's version",
			"/static/dist/spa.min.js?v=0123456789abcdef",
		),
		g.Entry("a font", "/static/fonts/Inter-Regular.woff2"),
	)

	g.It("answers an unchanged file with 304", func() {
		rr := get(
			"/static/dist/spa.min.js",
			`"`+static.versions["dist/spa.min.js"]+`"`,
		)

		Expect(rr.Code).To(Equal(http.StatusNotModified))
	})
})
