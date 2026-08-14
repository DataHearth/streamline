package restapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func indexerOverride(entries ...map[string]any) map[string]any {
	return map[string]any{"indexers": entries}
}

// errLeakyProbe mimics an unredacted transport failure: a torznab request URL
// carries the api key in its query string, so a handler that echoes the probe
// error hands the key to the caller.
var errLeakyProbe = fmt.Errorf("%w: Get %q: connection refused",
	indexer.ErrUnreachable,
	"http://idx.example:9117/api?t=caps&apikey=super-secret")

var _ = Describe(
	"Handler: Indexers",
	Label("unit", "server", "indexers"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			configtest.SetupFile()
			app = newAPIKeyApp()
		})

		Describe("ListIndexers", func() {
			It("returns empty array when none exist", func() {
				resp, err := http.Get(app.srv.URL + "/api/v1/indexers")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var items []Indexer
				Expect(json.NewDecoder(resp.Body).Decode(&items)).To(Succeed())
				Expect(items).To(BeEmpty())
			})

			It("returns indexers with optional fields populated", func() {
				configtest.SetupFile(indexerOverride(map[string]any{
					"name": "torznab", "host": "idx.example", "port": 443,
					"path": "/api", "use_ssl": true, "api_key": "secret",
					"protocol": "torznab", "priority": 10, "enabled": true,
				}))

				resp, err := http.Get(app.srv.URL + "/api/v1/indexers")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var items []Indexer
				Expect(json.NewDecoder(resp.Body).Decode(&items)).To(Succeed())
				Expect(items).To(HaveLen(1))
				Expect(items[0].Name).To(Equal("torznab"))
				Expect(items[0].ApiKeySet).To(BeTrue())
				Expect(items[0].Priority).NotTo(BeNil())
				Expect(*items[0].Priority).To(Equal(uint8(10)))
			})
		})

		Describe("CreateIndexer", func() {
			It("creates an indexer and persists it to config", func() {
				body := `{"name": "tz", "host": "idx", "port": 9117, "api_key": "k", "protocol": "torznab"}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/indexers",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var idx Indexer
				Expect(json.NewDecoder(resp.Body).Decode(&idx)).To(Succeed())
				Expect(idx.Name).To(Equal("tz"))
				Expect(idx.ApiKeySet).To(BeTrue())

				got, ok := config.FindIndexer("tz")
				Expect(ok).To(BeTrue())
				Expect(got.Host).To(Equal("idx"))
			})

			It("returns 409 on duplicate name", func() {
				configtest.SetupFile(indexerOverride(map[string]any{
					"name": "dup", "host": "h", "port": 9117,
					"api_key": "k", "protocol": "torznab",
				}))
				body := `{"name": "dup", "host": "h2", "port": 9118, "api_key": "k", "protocol": "torznab"}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/indexers",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})

			It("returns 403 when config is read-only", func() {
				configtest.SetupFile(map[string]any{"read_only": true})
				body := `{"name": "tz", "host": "idx", "port": 9117, "api_key": "k", "protocol": "torznab"}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/indexers",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			})
		})

		Describe("TestIndexer", func() {
			It("returns 404 when indexer does not exist", func() {
				app.indexers.EXPECT().
					TestByName(mock.Anything, "ghost").
					Return(config.ErrIndexerNotFound).
					Once()

				req := app.req(
					http.MethodPost,
					"/api/v1/indexers/ghost/test",
					"",
					nil,
				)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})

			It("returns 200 when reachable", func() {
				app.indexers.EXPECT().
					TestByName(mock.Anything, "tz").
					Return(nil).
					Once()

				req := app.req(http.MethodPost, "/api/v1/indexers/tz/test", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})

			It("returns 422 on a probe failure", func() {
				app.indexers.EXPECT().
					TestByName(mock.Anything, "tz").
					Return(indexer.ErrUnreachable).
					Once()

				req := app.req(http.MethodPost, "/api/v1/indexers/tz/test", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			})

			It("answers 422 with a fixed message, never the probe error", func() {
				app.indexers.EXPECT().
					TestByName(mock.Anything, "tz").
					Return(errLeakyProbe).
					Once()

				req := app.req(http.MethodPost, "/api/v1/indexers/tz/test", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))

				raw, err := io.ReadAll(resp.Body)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(raw)).NotTo(ContainSubstring("super-secret"))
				Expect(string(raw)).NotTo(ContainSubstring("apikey"))

				var body struct {
					Message string `json:"message"`
				}
				Expect(json.Unmarshal(raw, &body)).To(Succeed())
				Expect(body.Message).To(Equal(indexer.ErrUnreachable.Error()))
			})
		})

		Describe("TestDraftIndexer", func() {
			const draftBody = `{"name":"draft","host":"idx.example","port":9117,` +
				`"api_key":"super-secret","protocol":"torznab"}`

			It("answers 422 with a fixed message, never the probe error", func() {
				app.indexers.EXPECT().
					Test(mock.Anything, mock.Anything).
					Return(errLeakyProbe).
					Once()

				req := app.req(http.MethodPost, "/api/v1/indexers/test", "",
					strings.NewReader(draftBody))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))

				raw, err := io.ReadAll(resp.Body)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(raw)).NotTo(ContainSubstring("super-secret"))
				Expect(string(raw)).NotTo(ContainSubstring("apikey"))

				var body struct {
					Message string `json:"message"`
				}
				Expect(json.Unmarshal(raw, &body)).To(Succeed())
				Expect(body.Message).To(Equal(indexer.ErrUnreachable.Error()))
			})

			// The mock carries no Test expectation, so a handler that probed
			// the metadata service anyway fails the spec.
			It("refuses a link-local target without probing it", func() {
				body := `{"name":"draft","host":"169.254.169.254","port":80,` +
					`"api_key":"k","protocol":"torznab"}`

				req := app.req(http.MethodPost, "/api/v1/indexers/test", "",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			})

			It("returns 200 when the draft credentials work", func() {
				app.indexers.EXPECT().
					Test(mock.Anything, mock.Anything).
					Return(nil).
					Once()

				req := app.req(http.MethodPost, "/api/v1/indexers/test", "",
					strings.NewReader(draftBody))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})
		})

		Describe("UpdateIndexer", func() {
			It("updates fields and returns 200", func() {
				configtest.SetupFile(indexerOverride(map[string]any{
					"name": "tz", "host": "old", "port": 9117,
					"api_key": "keep", "protocol": "torznab", "enabled": true,
				}))

				body := `{"name": "tz", "host": "new", "port": 9200, "protocol": "torznab", "enabled": false, "priority": 3}`
				req := app.req(http.MethodPut, "/api/v1/indexers/tz", "",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				got, _ := config.FindIndexer("tz")
				Expect(got.Host).To(Equal("new"))
				Expect(got.APIKey).To(Equal("keep")) // blank api_key preserves
			})

			It("returns 404 for nonexistent indexer", func() {
				body := `{"name": "x", "host": "x", "port": 1, "protocol": "torznab"}`
				req := app.req(http.MethodPut, "/api/v1/indexers/ghost", "",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})

		Describe("DeleteIndexer", func() {
			It("deletes an existing indexer and returns 204", func() {
				configtest.SetupFile(indexerOverride(map[string]any{
					"name": "gone", "host": "h", "port": 9117,
					"api_key": "k", "protocol": "torznab",
				}))

				req := app.req(http.MethodDelete, "/api/v1/indexers/gone", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

				_, ok := config.FindIndexer("gone")
				Expect(ok).To(BeFalse())
			})

			It("returns 404 for nonexistent indexer", func() {
				req := app.req(http.MethodDelete, "/api/v1/indexers/ghost", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})
	},
)
