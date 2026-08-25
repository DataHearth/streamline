package restapi

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/quality"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// customFormatOverride seeds the config with the given custom_formats entries.
func customFormatOverride(entries ...map[string]any) map[string]any {
	return map[string]any{"custom_formats": entries}
}

var _ = Describe(
	"Handler: CustomFormats",
	Label("unit", "server", "custom-formats"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			configtest.SetupFile()
			app = newAPIKeyApp()
			app.addMember("")
		})

		Describe("ListCustomFormats", func() {
			It("returns builtins first, flagged builtin, then user formats", func() {
				configtest.SetupFile(customFormatOverride(map[string]any{
					"name": "my-format",
					"conditions": []map[string]any{
						{
							"type":     "release_title",
							"pattern":  "(?i)mine",
							"required": true,
						},
					},
				}))

				resp, err := http.Get(app.srv.URL + "/api/v1/custom-formats")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var items []CustomFormat
				Expect(json.NewDecoder(resp.Body).Decode(&items)).To(Succeed())

				builtins := quality.Builtins()
				Expect(items).To(HaveLen(len(builtins) + 1))
				for i, b := range builtins {
					Expect(items[i].Name).To(Equal(b.Name))
					Expect(items[i].Builtin).NotTo(BeNil())
					Expect(*items[i].Builtin).To(BeTrue())
					Expect(items[i].Description).NotTo(BeNil())
					Expect(*items[i].Description).To(Equal(b.Description))
				}
				last := items[len(items)-1]
				Expect(last.Name).To(Equal("my-format"))
				Expect(last.Builtin).To(BeNil())
				Expect(last.Description).To(BeNil())
			})
		})

		Describe("GetCustomFormat", func() {
			It("returns a builtin flagged builtin:true", func() {
				resp, err := http.Get(app.srv.URL + "/api/v1/custom-formats/remux")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var cf CustomFormat
				Expect(json.NewDecoder(resp.Body).Decode(&cf)).To(Succeed())
				Expect(cf.Name).To(Equal("remux"))
				Expect(cf.Builtin).NotTo(BeNil())
				Expect(*cf.Builtin).To(BeTrue())
				Expect(cf.Description).NotTo(BeNil())
				Expect(*cf.Description).NotTo(BeEmpty())
			})

			It("returns a user format unflagged and without a description", func() {
				configtest.SetupFile(customFormatOverride(map[string]any{
					"name": "my-format",
					"conditions": []map[string]any{
						{
							"type":     "release_title",
							"pattern":  "(?i)mine",
							"required": true,
						},
					},
				}))

				resp, err := http.Get(
					app.srv.URL + "/api/v1/custom-formats/my-format",
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var cf CustomFormat
				Expect(json.NewDecoder(resp.Body).Decode(&cf)).To(Succeed())
				Expect(cf.Builtin).To(BeNil())
				Expect(cf.Description).To(BeNil())
			})

			It("returns 404 for an unknown name", func() {
				resp, err := http.Get(app.srv.URL + "/api/v1/custom-formats/ghost")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})

		Describe("CreateCustomFormat", func() {
			It("creates a format and persists it to config", func() {
				body := `{"name": "my-format", "conditions": [` +
					`{"type": "release_title", "pattern": "(?i)mine", "required": true}]}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/custom-formats",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var cf CustomFormat
				Expect(json.NewDecoder(resp.Body).Decode(&cf)).To(Succeed())
				Expect(cf.Name).To(Equal("my-format"))
				Expect(cf.Conditions).To(HaveLen(1))

				got, ok := config.FindCustomFormat("my-format")
				Expect(ok).To(BeTrue())
				Expect(got.Conditions).To(HaveLen(1))
			})

			It("returns 409 when the name collides with a builtin", func() {
				body := `{"name": "remux", "conditions": [` +
					`{"type": "release_title", "pattern": "(?i)mine", "required": true}]}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/custom-formats",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})

			It("returns 422 for an invalid condition", func() {
				body := `{"name": "my-format", "conditions": [` +
					`{"type": "release_title"}]}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/custom-formats",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			})

			It("returns 403 for a non-admin caller", func() {
				body := `{"name": "my-format", "conditions": [` +
					`{"type": "release_title", "pattern": "(?i)mine", "required": true}]}`
				req := app.req(http.MethodPost, "/api/v1/custom-formats",
					app.memberKey, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			})
		})

		Describe("UpdateCustomFormat", func() {
			It("replaces the conditions and returns 200", func() {
				configtest.SetupFile(customFormatOverride(map[string]any{
					"name": "my-format",
					"conditions": []map[string]any{
						{
							"type":     "release_title",
							"pattern":  "(?i)mine",
							"required": true,
						},
					},
				}))

				body := `{"name": "my-format", "conditions": [` +
					`{"type": "release_title", "pattern": "(?i)yours", "required": true},` +
					`{"type": "codec", "value": "hevc"}]}`
				req := app.req(
					http.MethodPut,
					"/api/v1/custom-formats/my-format",
					"",
					strings.NewReader(body),
				)
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				got, ok := config.FindCustomFormat("my-format")
				Expect(ok).To(BeTrue())
				Expect(got.Conditions).To(HaveLen(2))
			})

			It("returns 404 for an unknown name", func() {
				body := `{"name": "ghost", "conditions": [` +
					`{"type": "release_title", "pattern": "(?i)mine", "required": true}]}`
				req := app.req(http.MethodPut, "/api/v1/custom-formats/ghost", "",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})

			It("returns 409 when targeting a builtin name", func() {
				body := `{"name": "remux", "conditions": [` +
					`{"type": "release_title", "pattern": "(?i)mine", "required": true}]}`
				req := app.req(http.MethodPut, "/api/v1/custom-formats/remux", "",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})
		})

		Describe("DeleteCustomFormat", func() {
			It("deletes an unused format and returns 204", func() {
				configtest.SetupFile(customFormatOverride(map[string]any{
					"name": "my-format",
					"conditions": []map[string]any{
						{
							"type":     "release_title",
							"pattern":  "(?i)mine",
							"required": true,
						},
					},
				}))

				req := app.req(http.MethodDelete,
					"/api/v1/custom-formats/my-format", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

				_, ok := config.FindCustomFormat("my-format")
				Expect(ok).To(BeFalse())
			})

			It("returns 404 for an unknown name", func() {
				req := app.req(http.MethodDelete,
					"/api/v1/custom-formats/ghost", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})

			It("returns 409 when deleting a builtin name", func() {
				req := app.req(http.MethodDelete,
					"/api/v1/custom-formats/remux", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})

			It("returns 409 when the format is scored by a quality profile", func() {
				configtest.SetupFile(map[string]any{
					"custom_formats": []map[string]any{
						{
							"name": "my-format",
							"conditions": []map[string]any{
								{
									"type":     "release_title",
									"pattern":  "(?i)mine",
									"required": true,
								},
							},
						},
					},
					"quality_profiles": []map[string]any{
						{
							"name": "hd", "preferred_resolution": "1080p",
							"min_resolution": "720p",
							"formats": []map[string]any{
								{"name": "my-format", "score": 10},
							},
						},
					},
					"quality_default_profile": "hd",
				})

				req := app.req(http.MethodDelete,
					"/api/v1/custom-formats/my-format", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})
		})

		Describe("TestCustomFormat", func() {
			It("returns per-condition verdicts and the overall match", func() {
				body := `{"conditions": [` +
					`{"type": "release_title", "pattern": "(?i)\\bhevc\\b", "required": true},` +
					`{"type": "release_title", "pattern": "(?i)\\bxvid\\b", "required": true}` +
					`], "sample": {"title": "Movie.2020.1080p.HEVC-GROUP", "size": 1073741824, "seeders": 10}}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/custom-formats/test",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var result CustomFormatTestResult
				Expect(json.NewDecoder(resp.Body).Decode(&result)).To(Succeed())
				Expect(result.Matched).To(BeFalse())
				Expect(result.Conditions).To(HaveLen(2))
				Expect(result.Conditions[0].Index).To(Equal(0))
				Expect(result.Conditions[0].Passed).To(BeTrue())
				Expect(result.Conditions[1].Index).To(Equal(1))
				Expect(result.Conditions[1].Passed).To(BeFalse())
			})

			It("returns 422 for an invalid condition", func() {
				body := `{"conditions": [{"type": "release_title"}],` +
					` "sample": {"title": "Movie.2020.1080p"}}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/custom-formats/test",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			})

			It(
				"returns 422 for an empty conditions list instead of a vacuous match",
				func() {
					body := `{"conditions": [], "sample": {"title": "Movie.2020.1080p"}}`
					resp, err := http.Post(
						app.srv.URL+"/api/v1/custom-formats/test",
						"application/json",
						strings.NewReader(body),
					)
					Expect(err).NotTo(HaveOccurred())
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
				},
			)
		})
	},
)
