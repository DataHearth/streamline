package server

import (
	"context"
	"encoding/json"
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

var _ = Describe("self-authenticated mounts", Label("unit", "server"), func() {
	var (
		client *ent.Client
		ts     *httptest.Server
		hc     *http.Client
	)

	BeforeEach(func() {
		ctx := context.Background()
		client = dbtest.SetupTestDB(ctx)
		configtest.Setup(map[string]any{"auth": map[string]any{"mode": "full"}})

		authMW := middleware.NewAuth(mwmocks.NewMockAuthenticator(GinkgoT()), nil, authExcludePaths)
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

	subsonicCall := func(query string) map[string]any {
		GinkgoHelper()
		resp, err := hc.Get(ts.URL + "/rest/ping?f=json" + query)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var body map[string]map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
		return body["subsonic-response"]
	}

	Describe("/rest", func() {
		It("reaches the subsonic handler without a session", func() {
			res := subsonicCall("")
			Expect(res["status"]).To(Equal("failed"))
			Expect(res["error"].(map[string]any)["code"]).To(BeEquivalentTo(10))
		})

		It("answers wrong credentials for an unknown user", func() {
			res := subsonicCall("&u=nobody@example.com&p=x")
			Expect(res["error"].(map[string]any)["code"]).To(BeEquivalentTo(40))
		})

		It("accepts a user with a subsonic password", func() {
			client.User.Create().
				SetEmail("listener@example.com").
				SetSubsonicPassword("sesame").
				SaveX(context.Background())
			res := subsonicCall("&u=listener@example.com&p=sesame")
			Expect(res["status"]).To(Equal("ok"))
		})
	})

	It("keeps the exclude list narrow", func() {
		resp, err := hc.Get(ts.URL + "/movies")
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Or(Equal(http.StatusFound), Equal(http.StatusUnauthorized)))
	})
})
