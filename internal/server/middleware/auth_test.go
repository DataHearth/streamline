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
