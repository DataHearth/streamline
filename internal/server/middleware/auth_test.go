package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/auth"
	mocks "github.com/datahearth/streamline/internal/server/middleware/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/utils/httputil"
)

var _ = g.Describe("authenticateAPI", g.Label("unit"), func() {
	const (
		sessionToken = "session.cookie.value"
		bearerToken  = "bearer.jwt.token"
		apiKey       = "test-api-key"
		jti          = "session-jti"
	)
	var (
		svc    *mocks.MockAuthenticator
		next   http.Handler
		claims *auth.Claims
		user   *ent.User
	)

	g.BeforeEach(func() {
		svc = mocks.NewMockAuthenticator(g.GinkgoT())
		claims = &auth.Claims{
			UserID: 1,
			Email:  "u@example.com",
			Role:   "admin",
			JTI:    jti,
		}
		user = &ent.User{
			ID:    1,
			Email: "u@example.com",
			Role:  entuser.Role("admin"),
		}
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	g.Context("session cookie on /api/v1", func() {
		g.It("accepts a valid cookie when Sec-Fetch-Site is same-origin", func() {
			svc.EXPECT().ValidateToken(sessionToken).Return(claims, nil).Once()
			svc.EXPECT().ValidateSession(mock.Anything, jti).Return(nil).Once()
			svc.EXPECT().TouchSessionAsync(jti).Return().Once()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/movies", nil)
			req.AddCookie(
				&http.Cookie{Name: auth.SessionCookie, Value: sessionToken},
			)
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			rr := httptest.NewRecorder()

			authenticateAPI(svc, nil, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusOK))
		})

		g.It("rejects a cookie when Sec-Fetch-Site is cross-site", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/movies", nil)
			req.AddCookie(
				&http.Cookie{Name: auth.SessionCookie, Value: sessionToken},
			)
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			rr := httptest.NewRecorder()

			authenticateAPI(svc, nil, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusUnauthorized))
		})

		g.It(
			"rejects a POST cookie when Sec-Fetch-Site and Origin are absent",
			func() {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/movies", nil)
				req.AddCookie(
					&http.Cookie{Name: auth.SessionCookie, Value: sessionToken},
				)
				rr := httptest.NewRecorder()

				authenticateAPI(svc, nil, next, rr, req)

				Expect(rr.Code).To(Equal(http.StatusUnauthorized))
			},
		)

		// A plain-http LAN install gets no Sec-Fetch-*, and Fetch omits Origin
		// on a same-origin GET, so the SPA's every read arrives with only a
		// Referer to prove where it came from.
		g.It("accepts a GET cookie on a same-origin Referer alone", func() {
			svc.EXPECT().ValidateToken(sessionToken).Return(claims, nil).Once()
			svc.EXPECT().ValidateSession(mock.Anything, jti).Return(nil).Once()
			svc.EXPECT().TouchSessionAsync(jti).Return().Once()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/movies", nil)
			req.AddCookie(
				&http.Cookie{Name: auth.SessionCookie, Value: sessionToken},
			)
			req.Header.Set("Referer", "http://"+req.Host+"/movies")
			rr := httptest.NewRecorder()

			authenticateAPI(svc, nil, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusOK))
		})

		g.It("rejects a GET cookie carrying a foreign Referer", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/movies", nil)
			req.AddCookie(
				&http.Cookie{Name: auth.SessionCookie, Value: sessionToken},
			)
			req.Header.Set("Referer", "http://evil.example/movies")
			rr := httptest.NewRecorder()

			authenticateAPI(svc, nil, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusUnauthorized))
		})

		g.It("rejects a POST cookie even with a same-origin Referer", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/movies", nil)
			req.AddCookie(
				&http.Cookie{Name: auth.SessionCookie, Value: sessionToken},
			)
			req.Header.Set("Referer", "http://"+req.Host+"/movies")
			rr := httptest.NewRecorder()

			authenticateAPI(svc, nil, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusUnauthorized))
		})

		g.It("keeps a present Sec-Fetch-Site authoritative over Referer", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/movies", nil)
			req.AddCookie(
				&http.Cookie{Name: auth.SessionCookie, Value: sessionToken},
			)
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			req.Header.Set("Referer", "http://"+req.Host+"/movies")
			rr := httptest.NewRecorder()

			authenticateAPI(svc, nil, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusUnauthorized))
		})

		g.It("rejects a cookie whose session has been revoked", func() {
			svc.EXPECT().ValidateToken(sessionToken).Return(claims, nil).Once()
			svc.EXPECT().
				ValidateSession(mock.Anything, jti).
				Return(auth.ErrSessionRevoked).
				Once()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/movies", nil)
			req.AddCookie(
				&http.Cookie{Name: auth.SessionCookie, Value: sessionToken},
			)
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			rr := httptest.NewRecorder()

			authenticateAPI(svc, nil, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusUnauthorized))
		})

		g.It("answers 503 when the session lookup itself failed", func() {
			// A DB failure is not a verdict about the credential. Answering
			// 401 here signs every user out for the duration of the outage
			// and the re-login storm lands on the same database.
			svc.EXPECT().ValidateToken(sessionToken).Return(claims, nil).Once()
			svc.EXPECT().
				ValidateSession(mock.Anything, jti).
				Return(errors.New("query session: database is locked")).
				Once()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/movies", nil)
			req.AddCookie(
				&http.Cookie{Name: auth.SessionCookie, Value: sessionToken},
			)
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			rr := httptest.NewRecorder()

			authenticateAPI(svc, nil, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusServiceUnavailable))
		})

		g.It("answers 503 on a bearer token whose session lookup failed", func() {
			svc.EXPECT().ValidateToken(bearerToken).Return(claims, nil).Once()
			svc.EXPECT().
				ValidateSession(mock.Anything, jti).
				Return(errors.New("query session: database is locked")).
				Once()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/movies", nil)
			req.Header.Set("Authorization", "Bearer "+bearerToken)
			rr := httptest.NewRecorder()

			authenticateAPI(svc, nil, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusServiceUnavailable))
		})
	})

	g.Context("existing auth transports stay intact", func() {
		g.It("accepts a Bearer token regardless of Sec-Fetch-Site", func() {
			svc.EXPECT().ValidateToken(bearerToken).Return(claims, nil).Once()
			svc.EXPECT().ValidateSession(mock.Anything, jti).Return(nil).Once()
			svc.EXPECT().TouchSessionAsync(jti).Return().Once()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/movies", nil)
			req.Header.Set("Authorization", "Bearer "+bearerToken)
			rr := httptest.NewRecorder()

			authenticateAPI(svc, nil, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusOK))
		})

		g.It("accepts an X-API-Key regardless of Sec-Fetch-Site", func() {
			svc.EXPECT().
				ValidateAPIKey(mock.Anything, apiKey).
				Return(user, nil).
				Once()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/movies", nil)
			req.Header.Set("X-API-Key", apiKey)
			rr := httptest.NewRecorder()

			authenticateAPI(svc, nil, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusOK))
		})

		g.It("rejects an unauthenticated request with no credentials", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/movies", nil)
			rr := httptest.NewRecorder()

			authenticateAPI(svc, nil, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusUnauthorized))
		})
	})

	g.Context("identity endpoints via API key", func() {
		keyed := func(method, path string) int {
			g.GinkgoHelper()
			svc.EXPECT().
				ValidateAPIKey(mock.Anything, apiKey).
				Return(user, nil).
				Once()
			req := httptest.NewRequest(method, path, nil)
			req.Header.Set("X-API-Key", apiKey)
			rr := httptest.NewRecorder()
			authenticateAPI(svc, nil, next, rr, req)
			return rr.Code
		}

		g.DescribeTable(
			"mutations are refused with 403",
			func(method, path string) {
				Expect(keyed(method, path)).To(Equal(http.StatusForbidden))
			},
			g.Entry("profile patch", http.MethodPatch, "/api/v1/auth/me"),
			g.Entry("key mint", http.MethodPost, "/api/v1/auth/me/api-keys"),
			g.Entry("key revoke", http.MethodDelete, "/api/v1/auth/me/api-keys/3"),
			g.Entry(
				"session revoke",
				http.MethodDelete,
				"/api/v1/auth/me/sessions/2",
			),
			g.Entry("password change", http.MethodPost, "/api/v1/auth/password"),
			g.Entry("invite create", http.MethodPost, "/api/v1/auth/invites"),
			g.Entry("user admin", http.MethodPatch, "/api/v1/users/2"),
			g.Entry("jwt rotate", http.MethodPost, "/api/v1/auth/jwt/rotate"),
		)

		g.DescribeTable("reads and non-identity mutations pass through",
			func(method, path string) {
				Expect(keyed(method, path)).To(Equal(http.StatusOK))
			},
			g.Entry("whoami", http.MethodGet, "/api/v1/auth/me"),
			g.Entry("key list", http.MethodGet, "/api/v1/auth/me/api-keys"),
			g.Entry("user list", http.MethodGet, "/api/v1/users"),
			g.Entry("media mutation", http.MethodPost, "/api/v1/movies"),
			g.Entry("prefix boundary", http.MethodPost, "/api/v1/userstats"),
		)

		g.It("allows the same mutation over Bearer", func() {
			svc.EXPECT().ValidateToken(bearerToken).Return(claims, nil).Once()
			svc.EXPECT().ValidateSession(mock.Anything, jti).Return(nil).Once()
			svc.EXPECT().TouchSessionAsync(jti).Return().Once()

			req := httptest.NewRequest(
				http.MethodPost, "/api/v1/auth/me/api-keys", nil,
			)
			req.Header.Set("Authorization", "Bearer "+bearerToken)
			rr := httptest.NewRecorder()

			authenticateAPI(svc, nil, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusOK))
		})
	})
})

var _ = g.Describe("api credential throttling", g.Label("unit"), func() {
	const apiKey = "wrong-key"

	var (
		svc     *mocks.MockAuthenticator
		limiter auth.Limiter
		next    http.Handler
	)

	// attempt sends one X-API-Key request from addr and reports the status.
	attempt := func(addr, key string) *httptest.ResponseRecorder {
		g.GinkgoHelper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/movies", nil)
		req.RemoteAddr = addr
		req.Header.Set("X-API-Key", key)
		rr := httptest.NewRecorder()
		authenticateAPI(svc, limiter, next, rr, req)
		return rr
	}

	g.BeforeEach(func() {
		svc = mocks.NewMockAuthenticator(g.GinkgoT())
		limiter = auth.NewLimiter(3, 15*time.Minute)
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	g.It("answers 429 once an address has spent its failures", func() {
		// Four: the throttled request is still validated before rejectAPI
		// refuses it — the limiter sits at the rejection, not at the door.
		svc.EXPECT().
			ValidateAPIKey(mock.Anything, apiKey).
			Return(nil, errors.New("nope")).
			Times(4)

		for range 3 {
			Expect(attempt("203.0.113.5:4000", apiKey).Code).
				To(Equal(http.StatusUnauthorized))
		}

		rr := attempt("203.0.113.5:4000", apiKey)
		Expect(rr.Code).To(Equal(http.StatusTooManyRequests))
		Expect(rr.Header().Get("Retry-After")).To(Equal("900"))
	})

	g.It("charges the failing address only", func() {
		svc.EXPECT().
			ValidateAPIKey(mock.Anything, apiKey).
			Return(nil, errors.New("nope")).
			Times(4)

		for range 3 {
			attempt("203.0.113.5:4000", apiKey)
		}

		Expect(attempt("203.0.113.9:4000", apiKey).Code).
			To(Equal(http.StatusUnauthorized))
	})

	g.It("never charges a request that authenticates", func() {
		good := &ent.User{ID: 1, Email: "u@example.com", Role: entuser.Role("admin")}
		svc.EXPECT().
			ValidateAPIKey(mock.Anything, "good-key").
			Return(good, nil).
			Times(10)
		svc.EXPECT().
			ValidateAPIKey(mock.Anything, apiKey).
			Return(nil, errors.New("nope")).
			Once()

		for range 10 {
			Expect(attempt("203.0.113.5:4000", "good-key").Code).
				To(Equal(http.StatusOK))
		}

		Expect(attempt("203.0.113.5:4000", apiKey).Code).
			To(Equal(http.StatusUnauthorized))
	})

	g.It("leaves the web transport unmetered", func() {
		serve := func() int {
			req := httptest.NewRequest(http.MethodGet, "/movies", nil)
			req.RemoteAddr = "203.0.113.5:4000"
			rr := httptest.NewRecorder()
			authenticateWeb(svc, next, rr, req)
			return rr.Code
		}

		for range 5 {
			Expect(serve()).To(Equal(http.StatusFound))
		}
	})
})

var _ = g.Describe("NewAuth in trusted-network mode", g.Label("unit"), func() {
	const roleHeader = "X-Test-Role"

	// serve wires the client-IP resolver ahead of the auth middleware, exactly
	// as the server does, and reports the role the request was granted.
	serve := func(trustedProxies []string, remoteAddr, xff string) (int, string) {
		g.GinkgoHelper()
		configtest.Setup(map[string]any{
			"server": map[string]any{"trusted_proxies": trustedProxies},
			"auth": map[string]any{
				"mode":             "trusted-network",
				"trusted_networks": []string{"192.168.50.0/24"},
				"trusted_role":     "admin",
			},
		})

		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c := auth.ClaimsFromContext(r.Context()); c != nil {
				w.Header().Set(roleHeader, c.Role)
			}
			w.WriteHeader(http.StatusOK)
		})
		h := httputil.ClientIPResolver()(
			NewAuth(mocks.NewMockAuthenticator(g.GinkgoT()), nil, nil)(inner),
		)

		req := httptest.NewRequest(http.MethodGet, "/movies", nil)
		req.RemoteAddr = remoteAddr
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code, rr.Header().Get(roleHeader)
	}

	g.It("grants the trusted role to a peer inside the network", func() {
		code, role := serve(nil, "192.168.50.7:4000", "")
		Expect(code).To(Equal(http.StatusOK))
		Expect(role).To(Equal("admin"))
	})

	g.It("ignores a forged X-Forwarded-For from a direct client", func() {
		code, role := serve(nil, "203.0.113.9:4000", "192.168.50.7")
		Expect(code).To(Equal(http.StatusFound))
		Expect(role).To(BeEmpty())
	})

	g.It("honours X-Forwarded-For from a configured proxy", func() {
		code, role := serve(
			[]string{"10.1.0.0/16"}, "10.1.0.5:9000", "192.168.50.7",
		)
		Expect(code).To(Equal(http.StatusOK))
		Expect(role).To(Equal("admin"))
	})
})

var _ = g.Describe("redirectToLogin", g.Label("unit"), func() {
	// The SPA reads next back through URLSearchParams (which percent-decodes)
	// and hands it to window.location.assign, so a next that resolves off-site
	// is an open redirect however it was spelled on the way in.
	g.DescribeTable("never sends an off-site next to the login page",
		func(target string) {
			g.GinkgoHelper()
			req := httptest.NewRequest(http.MethodGet, target, nil)
			rr := httptest.NewRecorder()

			redirectToLogin(rr, req)

			Expect(rr.Code).To(Equal(http.StatusFound))
			loc, err := url.Parse(rr.Header().Get("Location"))
			Expect(err).NotTo(HaveOccurred())

			next := loc.Query().Get("next")
			resolved, err := url.Parse("http://victim.local/login")
			Expect(err).NotTo(HaveOccurred())
			ref, err := url.Parse(strings.ReplaceAll(next, `\`, "/"))
			Expect(err).NotTo(HaveOccurred())

			Expect(resolved.ResolveReference(ref).Host).To(Equal("victim.local"))
		},
		g.Entry("an ordinary path", "/movies"),
		g.Entry("a backslash after the root", `/\evil.example`),
		g.Entry("an encoded backslash", "/%5Cevil.example"),
		g.Entry("a double backslash", `/\\evil.example`),
		g.Entry("a backslash after a valid path", `/movies\evil.example`),
		g.Entry("an encoded double slash", "/%2f%2fevil.example"),
	)
})
