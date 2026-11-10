package server

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	posmocks "github.com/datahearth/streamline/internal/posters/mocks"
	"github.com/datahearth/streamline/internal/server/middleware"
	mwmocks "github.com/datahearth/streamline/internal/server/middleware/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe("auth exclude list", Label("unit", "server"), func() {
	var (
		client *ent.Client
		ts     *httptest.Server
		hc     *http.Client
	)

	BeforeEach(func() {
		ctx := context.Background()
		client = dbtest.SetupTestDB(ctx)
		configtest.Setup(map[string]any{"auth": map[string]any{"mode": "full"}})

		authMW := middleware.NewAuth(
			mwmocks.NewMockAuthenticator(GinkgoT()),
			nil,
			authExcludePaths,
		)
		srv := New(Config{
			Ent:            client,
			Posters:        posmocks.NewMockManager(GinkgoT()),
			AuthMiddleware: authMW,
		})
		ts = httptest.NewServer(srv.Router())
		DeferCleanup(ts.Close)
		hc = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	})

	It("keeps the exclude list narrow", func() {
		resp, err := hc.Get(ts.URL + "/movies")
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(
			resp.StatusCode,
		).To(Or(Equal(http.StatusFound), Equal(http.StatusUnauthorized)))
	})
})
