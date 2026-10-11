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

	"github.com/datahearth/streamline/internal/cardigann/engine"
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
				Expect(items[0].Private).To(BeFalse())
			})

			It("reports an indexer marked private", func() {
				configtest.SetupFile(indexerOverride(map[string]any{
					"name": "tracker", "host": "idx.example", "port": 443,
					"api_key": "secret", "protocol": "torznab", "private": true,
				}))

				resp, err := http.Get(app.srv.URL + "/api/v1/indexers")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var items []Indexer
				Expect(json.NewDecoder(resp.Body).Decode(&items)).To(Succeed())
				Expect(items).To(HaveLen(1))
				Expect(items[0].Private).To(BeTrue())
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

			It("persists private when the body sets it", func() {
				body := `{"name": "tz", "host": "idx", "port": 9117, "api_key": "k", "protocol": "torznab", "private": true}`
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
				Expect(idx.Private).To(BeTrue())

				got, ok := config.FindIndexer("tz")
				Expect(ok).To(BeTrue())
				Expect(got.Private).To(BeTrue())
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

		Describe("a cardigann indexer", func() {
			BeforeEach(func() {
				configtest.SetupFile(indexerOverride(map[string]any{
					"name": "YTS", "protocol": "cardigann", "enabled": true,
					"cardigann": map[string]any{"definition": "yts"},
				}))
			})

			It("lists with its definition and the tracker's site", func() {
				app.indexers.EXPECT().Session(mock.Anything, "YTS").
					Return(indexer.SessionState{LoginRequired: true, LastError: "nope"}, nil)
				resp, err := http.Get(app.srv.URL + "/api/v1/indexers")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				var items []Indexer
				Expect(json.NewDecoder(resp.Body).Decode(&items)).To(Succeed())
				Expect(items).To(HaveLen(1))
				Expect(items[0].Protocol).To(Equal(IndexerProtocolCardigann))
				Expect(items[0].Definition).To(HaveValue(Equal("yts")))
				Expect(items[0].Host).To(Equal("yts.gg"))
				Expect(items[0].Port).To(Equal(uint16(443)))
				Expect(items[0].UseSsl).To(HaveValue(BeTrue()))
				Expect(items[0].ApiKeySet).To(BeFalse())
				Expect(items[0].Session).NotTo(BeNil())
				Expect(items[0].Session.LoginRequired).To(BeTrue())
				Expect(items[0].Session.LastError).To(HaveValue(Equal("nope")))
			})

			It("hands out the login captcha as a data URL", func() {
				png := []byte("\x89PNG\r\n\x1a\n0000")
				app.indexers.EXPECT().Captcha(mock.Anything, "YTS").
					Return("tok", &engine.Captcha{ContentType: "text/html", Data: png}, nil)
				resp := app.do(
					app.req(
						http.MethodPost,
						"/api/v1/indexers/YTS/captcha",
						"",
						nil,
					),
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var got IndexerCaptcha
				Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
				Expect(got.Token).To(Equal("tok"))
				// The type is sniffed, not taken from the tracker.
				Expect(got.Image).To(HavePrefix("data:image/png;base64,"))
			})

			It("answers 204 when the page shows no captcha", func() {
				app.indexers.EXPECT().
					Captcha(mock.Anything, "YTS").
					Return("", nil, nil)
				resp := app.do(
					app.req(
						http.MethodPost,
						"/api/v1/indexers/YTS/captcha",
						"",
						nil,
					),
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			})

			It(
				"logs in with a captcha answer and shows the tracker's refusal",
				func() {
					app.indexers.EXPECT().Login(mock.Anything, "YTS", "tok", "abc").
						Return(fmt.Errorf("%w: Wrong captcha", indexer.ErrLoginFailed)).
						Once()
					req := app.req(http.MethodPost, "/api/v1/indexers/YTS/login", "",
						strings.NewReader(`{"token":"tok","answer":"abc"}`))
					req.Header.Set("Content-Type", "application/json")
					resp := app.do(req)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
					body, _ := io.ReadAll(resp.Body)
					Expect(string(body)).To(ContainSubstring("Wrong captcha"))
					Expect(string(body)).To(ContainSubstring("connection_failed"))

					app.indexers.EXPECT().
						Login(mock.Anything, "YTS", "", "").
						Return(nil).
						Once()
					app.indexers.EXPECT().Session(mock.Anything, "YTS").
						Return(indexer.SessionState{LoginRequired: true, LoggedIn: true}, nil)
					resp = app.do(
						app.req(
							http.MethodPost,
							"/api/v1/indexers/YTS/login",
							"",
							nil,
						),
					)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusOK))
					var got Indexer
					Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
					Expect(got.Session.LoggedIn).To(BeTrue())
				},
			)

			It("drops its tracker session when deleted", func() {
				app.indexers.EXPECT().Forget(mock.Anything, "YTS").Return(nil)
				resp := app.do(
					app.req(http.MethodDelete, "/api/v1/indexers/YTS", "", nil),
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			})

			It("refuses an update: it lives in the config file", func() {
				req := app.req(
					http.MethodPut,
					"/api/v1/indexers/YTS",
					"",
					strings.NewReader(
						`{"name": "YTS", "host": "h", "port": 1, "api_key": "k"}`,
					),
				)
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
				got, _ := config.FindIndexer("YTS")
				Expect(got.Host).To(BeEmpty())
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

			It("sets private when given and leaves it alone when omitted", func() {
				configtest.SetupFile(indexerOverride(map[string]any{
					"name": "tz", "host": "h", "port": 9117,
					"api_key": "keep", "protocol": "torznab",
				}))
				put := func(body string) {
					GinkgoHelper()
					req := app.req(http.MethodPut, "/api/v1/indexers/tz", "",
						strings.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					resp := app.do(req)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusOK))
				}

				put(`{"name": "tz", "host": "h", "port": 9117, "private": true}`)
				got, _ := config.FindIndexer("tz")
				Expect(got.Private).To(BeTrue())

				put(`{"name": "tz", "host": "h", "port": 9117}`)
				got, _ = config.FindIndexer("tz")
				Expect(got.Private).To(BeTrue())

				put(`{"name": "tz", "host": "h", "port": 9117, "private": false}`)
				got, _ = config.FindIndexer("tz")
				Expect(got.Private).To(BeFalse())
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
