package web

import (
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("csrfGuard", Label("unit", "server"), func() {
	var (
		guarded http.Handler
		reached bool
	)

	BeforeEach(func() {
		configtest.Setup()
		reached = false
		guarded = csrfGuard(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusNoContent)
			}),
		)
	})

	// jsonPost builds a same-host POST carrying a JSON body; specs layer the
	// headers under test on top.
	jsonPost := func() *http.Request {
		req := httptest.NewRequest(
			http.MethodPost,
			"/auth/login",
			strings.NewReader(`{"email":"a@b.c"}`),
		)
		req.Host = "streamline.example"
		req.Header.Set("Content-Type", "application/json")
		return req
	}

	serve := func(req *http.Request) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		guarded.ServeHTTP(rr, req)
		return rr
	}

	It("passes a same-origin JSON request through", func() {
		req := jsonPost()
		req.Header.Set("Sec-Fetch-Site", "same-origin")

		Expect(serve(req).Code).To(Equal(http.StatusNoContent))
		Expect(reached).To(BeTrue())
	})

	DescribeTable("rejects a Sec-Fetch-Site that is not same-origin",
		func(site string) {
			req := jsonPost()
			req.Header.Set("Sec-Fetch-Site", site)
			// An attacker page controls the body and the cookie jar, so
			// neither can rescue a request the browser labelled foreign.
			req.Header.Set("Origin", "https://streamline.example")

			rr := serve(req)

			Expect(rr.Code).To(Equal(http.StatusForbidden))
			Expect(rr.Body.String()).To(ContainSubstring("cross_site_blocked"))
			Expect(reached).To(BeFalse())
		},
		Entry("cross-site", "cross-site"),
		Entry("sibling subdomain", "same-site"),
		Entry("user-initiated navigation", "none"),
	)

	It("rejects a foreign Sec-Fetch-Site even with no Origin at all", func() {
		req := jsonPost()
		req.Header.Set("Sec-Fetch-Site", "cross-site")

		Expect(serve(req).Code).To(Equal(http.StatusForbidden))
		Expect(reached).To(BeFalse())
	})

	// The shape every browser produces against a plain-http deployment: no
	// Fetch Metadata is emitted for a URL that is not potentially trustworthy,
	// so Origin alone has to carry the decision.
	It("accepts a matching Origin when Sec-Fetch-Site is absent", func() {
		req := jsonPost()
		req.Header.Set("Origin", "https://streamline.example")

		Expect(serve(req).Code).To(Equal(http.StatusNoContent))
		Expect(reached).To(BeTrue())
	})

	// A machine client — curl, the mobile app, a CLI — sends neither header,
	// and no browser omits both on a POST. POST /auth/login is the only way to
	// get a JWT, so this shape has to work.
	It("accepts a request carrying neither Sec-Fetch-Site nor Origin", func() {
		Expect(serve(jsonPost()).Code).To(Equal(http.StatusNoContent))
		Expect(reached).To(BeTrue())
	})

	DescribeTable("matches Origin against the host the request arrived on",
		func(host string) {
			req := jsonPost()
			req.Host = host
			req.Header.Set("Origin", "https://media.example")

			Expect(serve(req).Code).To(Equal(http.StatusNoContent))
			Expect(reached).To(BeTrue())
		},
		Entry("exactly", "media.example"),
		Entry("spelling out the default port", "media.example:443"),
		Entry("in a different case", "Media.Example"),
	)

	It("matches Origin against the configured public URL", func() {
		// A proxy that rewrites Host to an internal name leaves
		// STREAMLINE_PUBLIC_URL as the only trustworthy record of the host the
		// user typed.
		GinkgoT().Setenv("STREAMLINE_PUBLIC_URL", "https://media.example")
		req := jsonPost()
		req.Host = "streamline.internal:8080"
		req.Header.Set("Origin", "https://media.example")

		Expect(serve(req).Code).To(Equal(http.StatusNoContent))
		Expect(reached).To(BeTrue())
	})

	// X-Forwarded-Host is client-settable on any deployment that does not sit
	// behind a header-stripping proxy, so honouring it would let a caller
	// nominate its own Origin as one of ours.
	DescribeTable("never takes a served host from X-Forwarded-Host",
		func(forwarded string) {
			req := jsonPost()
			req.Header.Set("Origin", "https://evil.example")
			req.Header.Set("X-Forwarded-Host", forwarded)

			rr := serve(req)

			Expect(rr.Code).To(Equal(http.StatusForbidden))
			Expect(reached).To(BeFalse())
		},
		Entry("naming the attacker outright", "evil.example"),
		Entry("chained ahead of the real host", "evil.example, streamline.example"),
		Entry("chained behind the real host", "streamline.example, evil.example"),
	)

	// Header.Get reads the first value only, so whichever Origin an attacker
	// manages to smuggle in could decide the request depending on its position.
	DescribeTable("rejects a request carrying more than one Origin",
		func(first, second string) {
			req := jsonPost()
			req.Header.Add("Origin", first)
			req.Header.Add("Origin", second)

			rr := serve(req)

			Expect(rr.Code).To(Equal(http.StatusForbidden))
			Expect(reached).To(BeFalse())
		},
		Entry("ours first", "https://streamline.example", "https://evil.example"),
		Entry("ours second", "https://evil.example", "https://streamline.example"),
		Entry(
			"both ours",
			"https://streamline.example",
			"https://streamline.example",
		),
	)

	DescribeTable(
		"rejects an Origin no host of ours matches",
		func(origin string) {
			req := jsonPost()
			req.Header.Set("Origin", origin)

			rr := serve(req)

			Expect(rr.Code).To(Equal(http.StatusForbidden))
			Expect(reached).To(BeFalse())
		},
		Entry("foreign origin", "https://evil.example"),
		Entry(
			"host as a subdomain of the attacker",
			"https://streamline.example.evil.com",
		),
		Entry("opaque origin", "null"),
		Entry(
			"our host on a port we do not serve",
			"https://streamline.example:8443",
		),
	)

	DescribeTable("rejects a body an HTML form could have posted",
		func(contentType string) {
			req := jsonPost()
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			req.Header.Set("Content-Type", contentType)

			rr := serve(req)

			Expect(rr.Code).To(Equal(http.StatusUnsupportedMediaType))
			Expect(reached).To(BeFalse())
		},
		Entry("text/plain", "text/plain"),
		Entry("urlencoded", "application/x-www-form-urlencoded"),
		Entry("multipart", "multipart/form-data; boundary=x"),
		Entry("unparseable", "application/json; charset"),
	)

	It("accepts application/json with a charset parameter", func() {
		req := jsonPost()
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Content-Type", "application/json; charset=utf-8")

		Expect(serve(req).Code).To(Equal(http.StatusNoContent))
		Expect(reached).To(BeTrue())
	})

	It("accepts the SPA's bodyless logout POST", func() {
		req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		req.Header.Set("Sec-Fetch-Site", "same-origin")

		Expect(serve(req).Code).To(Equal(http.StatusNoContent))
		Expect(reached).To(BeTrue())
	})

	It("rejects a bodyless POST that declares a form content type", func() {
		req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Content-Type", "text/plain")

		Expect(serve(req).Code).To(Equal(http.StatusUnsupportedMediaType))
		Expect(reached).To(BeFalse())
	})
})
