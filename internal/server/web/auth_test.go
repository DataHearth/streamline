package web

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/auth"
	authmocks "github.com/datahearth/streamline/internal/auth/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var errInvalidCreds = errors.New("invalid credentials")

var _ = Describe("authLogin rate limiting", Label("unit", "server"), func() {
	const limit = 5

	var (
		handler *Handler
		manager *authmocks.MockManager
	)

	BeforeEach(func() {
		configtest.Setup()
		manager = authmocks.NewMockManager(GinkgoT())
		handler = New(Deps{
			Auth:    manager,
			Limiter: auth.NewLimiter(limit, time.Minute),
		})
	})

	post := func(password string) *httptest.ResponseRecorder {
		GinkgoHelper()
		req := httptest.NewRequest(
			http.MethodPost,
			"/auth/login",
			strings.NewReader(`{"email":"a@b.c","password":"`+password+`"}`),
		)
		req.Host = "streamline.example"
		req.RemoteAddr = "203.0.113.9:5000"
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler.authLogin(rr, req)
		return rr
	}

	// The bug this pins: allowAttempt runs before the credentials are known, so
	// without the refund the ceiling meters use rather than guessing. Behind a
	// proxy whose address is not in server.trusted_proxies every user shares one
	// budget, so five logins would lock out the whole deployment.
	It("never throttles an address whose logins all succeed", func() {
		manager.EXPECT().
			Login(mock.Anything, "a@b.c", "good", mock.Anything).
			Return("token", nil).
			Times(limit * 3)

		for range limit * 3 {
			Expect(post("good").Code).To(Equal(http.StatusNoContent))
		}
	})

	It("still throttles once the failures reach the limit", func() {
		manager.EXPECT().
			Login(mock.Anything, "a@b.c", "wrong", mock.Anything).
			Return("", errInvalidCreds).
			Times(limit)

		for range limit {
			Expect(post("wrong").Code).To(Equal(http.StatusUnauthorized))
		}

		rr := post("wrong")
		Expect(rr.Code).To(Equal(http.StatusTooManyRequests))
		Expect(rr.Header().Get("Retry-After")).NotTo(BeEmpty())
	})

	// A success must not launder the failures that came before it: the refund
	// returns one attempt, not the whole budget.
	It("keeps earlier failures charged when a login succeeds", func() {
		manager.EXPECT().
			Login(mock.Anything, "a@b.c", "wrong", mock.Anything).
			Return("", errInvalidCreds).
			Times(limit - 1)
		manager.EXPECT().
			Login(mock.Anything, "a@b.c", "good", mock.Anything).
			Return("token", nil).
			Once()

		for range limit - 1 {
			Expect(post("wrong").Code).To(Equal(http.StatusUnauthorized))
		}
		Expect(post("good").Code).To(Equal(http.StatusNoContent))

		manager.EXPECT().
			Login(mock.Anything, "a@b.c", "wrong", mock.Anything).
			Return("", errInvalidCreds).
			Once()
		Expect(post("wrong").Code).To(Equal(http.StatusUnauthorized))
		Expect(post("wrong").Code).To(Equal(http.StatusTooManyRequests))
	})
})

var _ = Describe("authLogin", Label("unit", "server"), func() {
	// login drives the handler with a Login that fails the given way and
	// returns everything a caller could observe about the rejection.
	login := func(email string, loginErr error) *httptest.ResponseRecorder {
		GinkgoHelper()
		mgr := authmocks.NewMockManager(GinkgoT())
		mgr.EXPECT().
			Login(mock.Anything, email, "hunter2", mock.Anything).
			Return("", loginErr).
			Once()
		h := New(Deps{Auth: mgr})

		req := httptest.NewRequest(
			http.MethodPost,
			"/auth/login",
			strings.NewReader(`{"email":"`+email+`","password":"hunter2"}`),
		)
		rr := httptest.NewRecorder()
		h.authLogin(rr, req)
		return rr
	}

	It(
		"answers identically for an unknown user, a wrong password and a lock",
		func() {
			unknown := login("ghost@example.com", errInvalidCreds)
			wrong := login("real@example.com", errInvalidCreds)
			locked := login("locked@example.com", auth.ErrAccountLockedT{
				LockedUntil: time.Now().Add(13 * time.Minute),
			})

			Expect(unknown.Code).To(Equal(http.StatusUnauthorized))
			Expect(wrong.Code).To(Equal(unknown.Code))
			Expect(locked.Code).To(Equal(unknown.Code))

			Expect(wrong.Body.String()).To(Equal(unknown.Body.String()))
			Expect(locked.Body.String()).To(Equal(unknown.Body.String()))

			Expect(wrong.Header()).To(Equal(unknown.Header()))
			Expect(locked.Header()).To(Equal(unknown.Header()))
		},
	)

	It("never hints at the lockout in the body it returns", func() {
		body := login("locked@example.com", auth.ErrAccountLockedT{
			LockedUntil: time.Now().Add(13 * time.Minute),
		}).Body.String()

		Expect(body).To(ContainSubstring("Invalid credentials"))
		Expect(strings.ToLower(body)).NotTo(ContainSubstring("lock"))
		Expect(body).NotTo(ContainSubstring("13"))
	})
})

var _ = Describe("sanitizeNext", Label("unit", "server"), func() {
	DescribeTable("passes an in-app path through unchanged",
		func(next string) {
			Expect(sanitizeNext(next)).To(Equal(next))
		},
		Entry("a rooted path", "/movies"),
		Entry("a nested path", "/movies/12/files"),
		Entry("a path with a query", "/movies?page=2&sort=title"),
		Entry("a path with an encoded segment", "/movies/the%20thing"),
	)

	DescribeTable("falls back to the landing page",
		func(next string) {
			Expect(sanitizeNext(next)).To(Equal(defaultLandingURL))
		},
		Entry("empty", ""),
		Entry("relative", "movies"),
		Entry("protocol-relative", "//evil.com"),
		Entry("absolute http", "http://evil.com/x"),
		Entry("absolute https", "https://evil.com/x"),
		Entry("scheme-only opaque", "javascript:alert(1)"),
		Entry("userinfo", "https://user@evil.com/"),
		Entry("an /auth/ path", "/auth/oidc/google/start"),

		// Browsers fold "\" into "/" inside a Location value, so every one of
		// these reaches the wire as protocol-relative //evil.com.
		Entry("backslash after the leading slash", `/\evil.com`),
		Entry("double backslash", `\\evil.com`),
		Entry("mixed slash then backslash", `/\/evil.com`),
		Entry("mixed backslash then slash", `\/evil.com`),
		Entry("backslash mid-path", `/movies\..\..\evil.com`),
		Entry("backslash in the query", `/movies?next=\\evil.com`),
		Entry("backslash behind a scheme", `https:/\evil.com`),

		// Browsers strip these from a URL before resolving it, collapsing the
		// value into the protocol-relative form.
		Entry("tab", "/\t/evil.com"),
		Entry("newline", "/\n/evil.com"),
		Entry("carriage return", "/\r/evil.com"),

		// Decodes to "//evil.com", so the prefix checks run on the decoded path.
		Entry("encoded protocol-relative", "/%2f%2fevil.com"),
		Entry("encoded auth path", "/%61uth/oidc/google/start"),
	)

	It("never emits a backslash, whatever it is handed", func() {
		for _, next := range []string{
			`/\evil.com`, `/a\b`, `/%5cevil.com`, `/x?y=\z`, `\`,
		} {
			Expect(sanitizeNext(next)).NotTo(ContainSubstring(`\`))
		}
	})
})

var _ = Describe("oidcRedirectURI", Label("unit", "server"), func() {
	const provider = "keycloak"

	// The default RemoteAddr httptest.NewRequest hands out (192.0.2.1) sits
	// inside this range; untrustedRedirectPeer sits outside it.
	trustProxies := func() {
		GinkgoHelper()
		configtest.Setup(map[string]any{
			"server": map[string]any{
				"trusted_proxies": []string{"192.0.2.0/24"},
			},
		})
	}

	request := func(headers map[string]string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/auth/oidc/keycloak/start", nil)
		r.Host = "streamline.internal"
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		return r
	}

	BeforeEach(func() { configtest.Setup() })

	DescribeTable("builds an https callback off any forwarded TLS spelling",
		func(header, value string) {
			trustProxies()

			Expect(oidcRedirectURI(request(map[string]string{
				header: value,
			}), provider)).To(Equal(
				"https://streamline.internal/auth/oidc/keycloak/callback",
			))
		},
		Entry("RFC 7239 Forwarded", "Forwarded", "proto=https"),
		Entry("X-Forwarded-Ssl", "X-Forwarded-Ssl", "on"),
		Entry("uppercased scheme", "X-Forwarded-Proto", "HTTPS"),
	)

	It("keeps honoring X-Forwarded-Host alongside the forwarded scheme", func() {
		trustProxies()

		Expect(oidcRedirectURI(request(map[string]string{
			"Forwarded":        "proto=https",
			"X-Forwarded-Host": "media.example",
		}), provider)).To(Equal(
			"https://media.example/auth/oidc/keycloak/callback",
		))
	})

	It("ignores every forwarded header from an untrusted peer", func() {
		trustProxies()
		r := request(map[string]string{
			"Forwarded":         "proto=https",
			"X-Forwarded-Proto": "https",
			"X-Forwarded-Ssl":   "on",
			"X-Forwarded-Host":  "evil.example",
		})
		r.RemoteAddr = "198.51.100.7:5555"

		Expect(oidcRedirectURI(r, provider)).To(Equal(
			"http://streamline.internal/auth/oidc/keycloak/callback",
		))
	})

	It("uses https when the request itself arrived over TLS", func() {
		r := request(nil)
		r.TLS = &tls.ConnectionState{}

		Expect(oidcRedirectURI(r, provider)).To(Equal(
			"https://streamline.internal/auth/oidc/keycloak/callback",
		))
	})
})
