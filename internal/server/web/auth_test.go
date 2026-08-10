package web

import (
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
