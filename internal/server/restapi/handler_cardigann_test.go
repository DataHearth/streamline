package restapi

import (
	"encoding/json"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Handler: Cardigann", Label("unit", "server", "indexers"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		configtest.SetupFile()
		app = newAPIKeyApp()
	})

	It("lists no held update against the embedded snapshot", func() {
		resp := app.do(app.req(http.MethodGet, "/api/v1/cardigann/updates", "", nil))
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var got CardigannUpdates
		Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
		emb, err := definitions.Snapshot("")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Upstream.Commit).To(Equal(emb.Upstream.Commit))
		Expect(got.Pending).To(BeEmpty())
	})

	It("answers 404 for an update nobody held", func() {
		for _, verb := range []string{"approve", "reject"} {
			resp := app.do(app.req(http.MethodPost,
				"/api/v1/cardigann/updates/yts/"+verb, "", nil))
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound), verb)
		}
	})
})
