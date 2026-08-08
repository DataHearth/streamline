package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	webassets "github.com/datahearth/streamline/web"
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

var _ = g.Describe("API docs bundle", g.Label("unit"), func() {
	var bundle string

	g.BeforeEach(func() {
		raw, err := webassets.Assets.ReadFile("static/js/docs.min.js")
		Expect(err).ToNot(HaveOccurred())
		bundle = string(raw)
	})

	// font-src is 'self', so Scalar's default theme — fourteen woff2 subsets
	// off https://fonts.scalar.com — has to be switched off and replaced with
	// the copies web/static/fonts already ships for the SPA. Dropping either
	// half is silent in Go and in the build: the page still renders, in a
	// fallback face, logging a blocked request per subset.
	//
	// The flag is read back out of minified output because that is where the
	// decision ends up; esbuild writes `false` as `!1`. A future minifier
	// spelling it a third way fails this spec, which is the safe direction —
	// a maintainer widens the pattern, rather than the CDN quietly coming
	// back.
	g.It("keeps Scalar's font CDN switched off", func() {
		Expect(bundle).To(MatchRegexp(`withDefaultFonts:\s*(!1|false)`))
	})

	// web/static/fonts is the only source of truth for the files themselves.
	// Renaming one there leaves docs.js pointing at a 404 that no other test
	// and no build step notices.
	g.It("names only font files the binary embeds", func() {
		refs := regexp.MustCompile(`/static/fonts/[^"')]+`).
			FindAllString(bundle, -1)

		Expect(refs).ToNot(BeEmpty(), "the docs bundle self-hosts no font")
		for _, ref := range refs {
			_, err := webassets.Assets.Open(strings.TrimPrefix(ref, "/"))
			Expect(err).ToNot(HaveOccurred(), ref)
		}
	})
})
