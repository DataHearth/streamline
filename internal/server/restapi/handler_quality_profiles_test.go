package restapi

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// qualityProfileOverride seeds the config with the given profiles and points
// quality_default_profile at the first one.
func qualityProfileOverride(entries ...map[string]any) map[string]any {
	def := ""
	if len(entries) > 0 {
		def, _ = entries[0]["name"].(string)
	}
	return map[string]any{
		"quality_profiles":        entries,
		"quality_default_profile": def,
	}
}

var _ = Describe(
	"Handler: QualityProfiles",
	Label("unit", "server", "quality-profiles"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			configtest.SetupFile()
			app = newAPIKeyApp()
		})

		Describe("ListQualityProfiles", func() {
			It("returns the configured profiles", func() {
				configtest.SetupFile(qualityProfileOverride(
					map[string]any{
						"name": "hd", "preferred_resolution": "1080p",
						"min_resolution": "720p", "upgrade_allowed": true,
					}))

				resp, err := http.Get(app.srv.URL + "/api/v1/quality-profiles")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var items []QualityProfile
				Expect(json.NewDecoder(resp.Body).Decode(&items)).To(Succeed())
				Expect(items).To(HaveLen(1))
				Expect(items[0].Name).To(Equal("hd"))
				Expect(string(items[0].PreferredResolution)).To(Equal("1080p"))
			})
		})

		Describe("CreateQualityProfile", func() {
			It("creates a profile and persists it to config", func() {
				body := `{"name": "uhd", "preferred_resolution": "2160p", "min_resolution": "1080p", "upgrade_allowed": true}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/quality-profiles",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var qp QualityProfile
				Expect(json.NewDecoder(resp.Body).Decode(&qp)).To(Succeed())
				Expect(qp.Name).To(Equal("uhd"))

				got, ok := config.ResolveQualityProfile("uhd")
				Expect(ok).To(BeTrue())
				Expect(got.PreferredResolution).To(Equal("2160p"))
			})

			It(
				"stores allowed_codecs and returns them in the create response",
				func() {
					body := `{"name": "uhd", "preferred_resolution": "2160p",` +
						` "min_resolution": "1080p", "allowed_codecs": ["hevc", "av1"]}`
					resp, err := http.Post(
						app.srv.URL+"/api/v1/quality-profiles",
						"application/json",
						strings.NewReader(body),
					)
					Expect(err).NotTo(HaveOccurred())
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusCreated))

					var qp QualityProfile
					Expect(json.NewDecoder(resp.Body).Decode(&qp)).To(Succeed())
					Expect(qp.AllowedCodecs).NotTo(BeNil())
					Expect(*qp.AllowedCodecs).To(Equal([]string{"hevc", "av1"}))

					got, _ := config.ResolveQualityProfile("uhd")
					Expect(got.AllowedCodecs).To(Equal([]string{"hevc", "av1"}))
				},
			)

			It("defaults allowed_codecs to [] (not null) when omitted", func() {
				body := `{"name": "uhd", "preferred_resolution": "2160p", "min_resolution": "1080p"}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/quality-profiles",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var raw map[string]any
				Expect(json.NewDecoder(resp.Body).Decode(&raw)).To(Succeed())
				Expect(raw).To(HaveKey("allowed_codecs"))
				Expect(raw["allowed_codecs"]).To(Equal([]any{}))
			})

			It(
				"stores formats/min_score/upgrade_until_score and returns them in the create response",
				func() {
					body := `{"name": "uhd", "preferred_resolution": "2160p",` +
						` "min_resolution": "1080p",` +
						` "formats": [{"name": "remux", "score": 10}],` +
						` "min_score": 5, "upgrade_until_score": 20}`
					resp, err := http.Post(
						app.srv.URL+"/api/v1/quality-profiles",
						"application/json",
						strings.NewReader(body),
					)
					Expect(err).NotTo(HaveOccurred())
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusCreated))

					var qp QualityProfile
					Expect(json.NewDecoder(resp.Body).Decode(&qp)).To(Succeed())
					Expect(qp.Formats).NotTo(BeNil())
					Expect(*qp.Formats).To(HaveLen(1))
					Expect((*qp.Formats)[0].Name).To(Equal("remux"))
					Expect(*(*qp.Formats)[0].Score).To(Equal(10))
					Expect(*qp.MinScore).To(Equal(5))
					Expect(*qp.UpgradeUntilScore).To(Equal(20))

					got, ok := config.ResolveQualityProfile("uhd")
					Expect(ok).To(BeTrue())
					Expect(got.Formats).To(Equal(
						[]config.QualityProfileFormatScore{
							{Name: "remux", Score: 10},
						},
					))
					Expect(got.MinScore).To(Equal(5))
					Expect(got.UpgradeUntilScore).To(Equal(20))
				},
			)

			It("returns 409 on duplicate name", func() {
				configtest.SetupFile(qualityProfileOverride(
					map[string]any{
						"name": "hd", "preferred_resolution": "1080p",
						"min_resolution": "720p",
					}))
				body := `{"name": "hd", "preferred_resolution": "2160p", "min_resolution": "1080p"}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/quality-profiles",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})
		})

		Describe("UpdateQualityProfile", func() {
			It("round-trips formats/min_score/upgrade_until_score", func() {
				configtest.SetupFile(qualityProfileOverride(
					map[string]any{
						"name": "hd", "preferred_resolution": "1080p",
						"min_resolution": "720p",
					}))

				body := `{"name": "hd", "preferred_resolution": "1080p",` +
					` "min_resolution": "720p",` +
					` "formats": [{"name": "hdr", "score": 15}],` +
					` "min_score": 3, "upgrade_until_score": 12}`
				req := app.req(http.MethodPut, "/api/v1/quality-profiles/hd", "",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var qp QualityProfile
				Expect(json.NewDecoder(resp.Body).Decode(&qp)).To(Succeed())
				Expect(*qp.Formats).To(HaveLen(1))
				Expect((*qp.Formats)[0].Name).To(Equal("hdr"))
				Expect(*qp.MinScore).To(Equal(3))
				Expect(*qp.UpgradeUntilScore).To(Equal(12))

				got, _ := config.ResolveQualityProfile("hd")
				Expect(got.Formats).To(Equal(
					[]config.QualityProfileFormatScore{{Name: "hdr", Score: 15}},
				))
				Expect(got.MinScore).To(Equal(3))
				Expect(got.UpgradeUntilScore).To(Equal(12))
			})

			It("updates fields and returns 200", func() {
				configtest.SetupFile(qualityProfileOverride(
					map[string]any{
						"name": "hd", "preferred_resolution": "1080p",
						"min_resolution": "720p",
					}))

				body := `{"name": "hd", "preferred_resolution": "2160p", "min_resolution": "1080p", "upgrade_allowed": true}`
				req := app.req(http.MethodPut, "/api/v1/quality-profiles/hd", "",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				got, _ := config.ResolveQualityProfile("hd")
				Expect(got.PreferredResolution).To(Equal("2160p"))
				Expect(got.UpgradeAllowed).To(BeTrue())
			})

			It("returns 404 for nonexistent profile", func() {
				body := `{"name": "x", "preferred_resolution": "1080p", "min_resolution": "720p"}`
				req := app.req(http.MethodPut, "/api/v1/quality-profiles/ghost", "",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})

			It("round-trips allowed_codecs", func() {
				configtest.SetupFile(qualityProfileOverride(
					map[string]any{
						"name": "hd", "preferred_resolution": "1080p",
						"min_resolution": "720p",
					}))

				body := `{"name": "hd", "preferred_resolution": "1080p",` +
					` "min_resolution": "720p", "allowed_codecs": ["hevc"]}`
				req := app.req(http.MethodPut, "/api/v1/quality-profiles/hd", "",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var qp QualityProfile
				Expect(json.NewDecoder(resp.Body).Decode(&qp)).To(Succeed())
				Expect(*qp.AllowedCodecs).To(Equal([]string{"hevc"}))

				got, _ := config.ResolveQualityProfile("hd")
				Expect(got.AllowedCodecs).To(Equal([]string{"hevc"}))
			})

			It(
				"leaves allowed_codecs untouched when the patch body omits it",
				func() {
					configtest.SetupFile(qualityProfileOverride(
						map[string]any{
							"name":                 "hd",
							"preferred_resolution": "1080p",
							"min_resolution":       "720p",
							"allowed_codecs":       []string{"hevc"},
						}))

					body := `{"name": "hd", "preferred_resolution": "2160p", "min_resolution": "1080p"}`
					req := app.req(http.MethodPut, "/api/v1/quality-profiles/hd", "",
						strings.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					resp := app.do(req)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusOK))

					got, _ := config.ResolveQualityProfile("hd")
					Expect(got.AllowedCodecs).To(Equal([]string{"hevc"}))
				},
			)

			It(
				"clears allowed_codecs back to [] when patched with an empty list",
				func() {
					configtest.SetupFile(qualityProfileOverride(
						map[string]any{
							"name":                 "hd",
							"preferred_resolution": "1080p",
							"min_resolution":       "720p",
							"allowed_codecs":       []string{"hevc"},
						}))

					body := `{"name": "hd", "preferred_resolution": "1080p",` +
						` "min_resolution": "720p", "allowed_codecs": []}`
					req := app.req(http.MethodPut, "/api/v1/quality-profiles/hd", "",
						strings.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					resp := app.do(req)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusOK))

					got, _ := config.ResolveQualityProfile("hd")
					Expect(got.AllowedCodecs).To(BeEmpty())
				},
			)
		})

		Describe("DeleteQualityProfile", func() {
			It("deletes a non-default profile and returns 204", func() {
				configtest.SetupFile(qualityProfileOverride(
					map[string]any{
						"name": "hd", "preferred_resolution": "1080p",
						"min_resolution": "720p",
					},
					map[string]any{
						"name": "uhd", "preferred_resolution": "2160p",
						"min_resolution": "1080p",
					}))

				req := app.req(http.MethodDelete,
					"/api/v1/quality-profiles/uhd", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

				_, ok := config.ResolveQualityProfile("uhd")
				Expect(ok).To(BeTrue()) // resolves to default now, not "uhd"
			})

			It("returns 409 when deleting the default profile", func() {
				configtest.SetupFile(qualityProfileOverride(
					map[string]any{
						"name": "hd", "preferred_resolution": "1080p",
						"min_resolution": "720p",
					}))

				req := app.req(http.MethodDelete,
					"/api/v1/quality-profiles/hd", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})

			It("returns 404 for nonexistent profile", func() {
				req := app.req(http.MethodDelete,
					"/api/v1/quality-profiles/ghost", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})
	},
)
