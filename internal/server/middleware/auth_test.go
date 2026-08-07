package middleware

import (
	"net/http"
	"net/http/httptest"

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

			authenticateAPI(svc, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusOK))
		})

		g.It("rejects a cookie when Sec-Fetch-Site is cross-site", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/movies", nil)
			req.AddCookie(
				&http.Cookie{Name: auth.SessionCookie, Value: sessionToken},
			)
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			rr := httptest.NewRecorder()

			authenticateAPI(svc, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusUnauthorized))
		})

		g.It("rejects a cookie when Sec-Fetch-Site is absent", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/movies", nil)
			req.AddCookie(
				&http.Cookie{Name: auth.SessionCookie, Value: sessionToken},
			)
			rr := httptest.NewRecorder()

			authenticateAPI(svc, next, rr, req)

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

			authenticateAPI(svc, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusUnauthorized))
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

			authenticateAPI(svc, next, rr, req)

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

			authenticateAPI(svc, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusOK))
		})

		g.It("rejects an unauthenticated request with no credentials", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/movies", nil)
			rr := httptest.NewRecorder()

			authenticateAPI(svc, next, rr, req)

			Expect(rr.Code).To(Equal(http.StatusUnauthorized))
		})
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
			NewAuth(mocks.NewMockAuthenticator(g.GinkgoT()), nil)(inner),
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
