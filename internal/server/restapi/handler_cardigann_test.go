package restapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/testutil/cardiganntest"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

type cardigannRoundTrip func(*http.Request) (*http.Response, error)

func (f cardigannRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

var _ = Describe("Handler: Cardigann", Label("unit", "server", "indexers"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		configtest.SetupFile()
		app = newAPIKeyApp()
	})

	When("upstream cannot be reached and nothing was fetched", func() {
		BeforeEach(func() {
			prev := definitions.Install(nil)
			DeferCleanup(func() { definitions.Install(prev) })
			orig := otelx.HTTPClient.Transport
			otelx.HTTPClient.Transport = cardigannRoundTrip(
				func(*http.Request) (*http.Response, error) {
					return nil, errors.New("offline")
				},
			)
			DeferCleanup(func() { otelx.HTTPClient.Transport = orig })
		})

		It("answers the catalog and a definition with 503", func() {
			for _, path := range []string{
				"/api/v1/cardigann/catalog",
				"/api/v1/cardigann/catalog/yts",
			} {
				resp := app.do(app.req(http.MethodGet, path, "", nil))
				resp.Body.Close()
				Expect(
					resp.StatusCode,
				).To(Equal(http.StatusServiceUnavailable), path)
			}
		})
	})

	When("a catalog is in force", func() {
		BeforeEach(cardiganntest.Install)

		It("lists it", func() {
			resp := app.do(
				app.req(http.MethodGet, "/api/v1/cardigann/catalog", "", nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var got CardigannCatalog
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got.Upstream.Commit).To(Equal(cardiganntest.Upstream.Commit))
			files := make([]string, 0, len(got.Definitions))
			for _, d := range got.Definitions {
				files = append(files, d.File)
			}
			Expect(files).To(ConsistOf("13city", "3dtorrents", "yts"))
		})

		It("describes a definition with what an entry carries of it", func() {
			resp := app.do(
				app.req(http.MethodGet, "/api/v1/cardigann/catalog/yts", "", nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var got CardigannDefinition
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got.Name).To(Equal("YTS"))

			resp = app.do(
				app.req(http.MethodGet, "/api/v1/cardigann/catalog/nope", "", nil),
			)
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("pins an indexer it adds to the catalog's version, kept", func() {
			app.indexers.EXPECT().Session(mock.Anything, "YTS").
				Return(indexer.SessionState{}, nil)
			resp := app.do(app.req(
				http.MethodPost,
				"/api/v1/indexers",
				"",
				strings.NewReader(
					`{"name":"YTS","protocol":"cardigann","definition":"yts"}`,
				),
			))
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			e, ok := config.FindIndexer("YTS")
			Expect(ok).To(BeTrue())
			Expect(e.Cardigann.Commit).To(Equal(cardiganntest.Upstream.Commit))
			definitions.Install(nil)
			Expect(definitions.Pinned(
				config.Get().DataDir, "yts", e.Cardigann.Commit,
			)).To(Equal(cardiganntest.Spec("yts")))
		})

		It("describes the version an indexer is pinned to when asked", func() {
			saved := bytes.Replace(cardiganntest.Spec("yts"),
				[]byte(`"name": "YTS"`), []byte(`"name": "YTS as saved"`), 1)
			configtest.SetupFile(map[string]any{
				"indexers": []map[string]any{{
					"name": "YTS", "protocol": "cardigann", "enabled": true,
					"cardigann": map[string]any{
						"definition": "yts",
						"commit":     cardiganntest.Commit(saved),
					},
				}},
			})
			cardiganntest.Keep(config.Get().DataDir, "yts", saved)
			resp := app.do(app.req(http.MethodGet,
				"/api/v1/cardigann/catalog/yts?indexer=YTS", "", nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var got CardigannDefinition
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got.Name).To(Equal("YTS as saved"))

			resp = app.do(app.req(http.MethodGet,
				"/api/v1/cardigann/catalog/13city?indexer=YTS", "", nil))
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("lists no held update", func() {
			resp := app.do(
				app.req(http.MethodGet, "/api/v1/cardigann/updates", "", nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var got CardigannUpdates
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got.Upstream.Commit).To(Equal(cardiganntest.Upstream.Commit))
			Expect(got.Pending).To(BeEmpty())
		})
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
