package restapi

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe(
	"Handler: MusicQualityProfiles",
	Label("unit", "server", "music"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			configtest.SetupFile(map[string]any{
				"music_quality_profiles": []map[string]any{
					{
						"name":      "lossless",
						"tiers":     []string{"hires", "lossless"},
						"preferred": "lossless",
					},
					{
						"name":      "portable",
						"tiers":     []string{"high"},
						"preferred": "high",
					},
				},
				"music_quality_default_profile": "lossless",
			})
			app = newAPIKeyApp()
		})

		send := func(method, path, key, body string) *http.Response {
			GinkgoHelper()
			var req *http.Request
			if body == "" {
				req = app.req(method, path, key, nil)
			} else {
				req = app.req(method, path, key, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
			}
			return app.do(req)
		}

		It("deletes a non-default profile", func() {
			resp := send(
				http.MethodDelete,
				"/api/v1/music/quality-profiles/portable",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("answers 409 when deleting the default profile", func() {
			resp := send(
				http.MethodDelete,
				"/api/v1/music/quality-profiles/lossless",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		It("404s when deleting an unknown profile", func() {
			resp := send(
				http.MethodDelete,
				"/api/v1/music/quality-profiles/ghost",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		decode := func(resp *http.Response, out any) {
			GinkgoHelper()
			defer resp.Body.Close()
			Expect(json.NewDecoder(resp.Body).Decode(out)).To(Succeed())
		}

		It(
			"lists the profiles with tiers best first and the default flagged",
			func() {
				resp := send(
					http.MethodGet,
					"/api/v1/music/quality-profiles",
					app.adminKey,
					"",
				)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var got []MusicQualityProfile
				decode(resp, &got)
				Expect(got).To(HaveLen(2))
				Expect(got[0].Name).To(Equal("lossless"))
				Expect(got[0].Tiers).To(Equal([]MusicTier{"hires", "lossless"}))
				Expect(got[0].IsDefault).To(BeTrue())
				Expect(got[1].IsDefault).To(BeFalse())
			},
		)

		It("lets a request-only user read the list for the request form", func() {
			resp := send(
				http.MethodGet,
				"/api/v1/music/quality-profiles",
				app.requestOnlyKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("creates a profile and stores its tiers best first", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/music/quality-profiles",
				app.adminKey,
				`{"name":"mixed","tiers":["high","hires"],"preferred":"hires","upgrade_allowed":true}`,
			)
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			var got MusicQualityProfile
			decode(resp, &got)
			Expect(got.Tiers).To(Equal([]MusicTier{"hires", "high"}))
			Expect(got.UpgradeAllowed).To(BeTrue())
			Expect(got.IsDefault).To(BeFalse())
		})

		It("answers 409 for a name already taken", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/music/quality-profiles",
				app.adminKey,
				`{"name":"lossless","tiers":["high"],"preferred":"high","upgrade_allowed":false}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		It("answers 422 when preferred is not one of the tiers", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/music/quality-profiles",
				app.adminKey,
				`{"name":"odd","tiers":["high"],"preferred":"hires","upgrade_allowed":false}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("answers 403 to a non-admin writing", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/music/quality-profiles",
				app.requestOnlyKey,
				`{"name":"x","tiers":["high"],"preferred":"high","upgrade_allowed":false}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("replaces a profile on PUT, ignoring the body name", func() {
			resp := send(
				http.MethodPut,
				"/api/v1/music/quality-profiles/portable",
				app.adminKey,
				`{"name":"ignored","tiers":["high","standard"],"preferred":"high","upgrade_allowed":true}`,
			)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var got MusicQualityProfile
			decode(resp, &got)
			Expect(got.Name).To(Equal("portable"))
			Expect(got.Tiers).To(Equal([]MusicTier{"high", "standard"}))
		})

		It("answers 404 updating an unknown profile and 422 for a bad one", func() {
			resp := send(
				http.MethodPut,
				"/api/v1/music/quality-profiles/ghost",
				app.adminKey,
				`{"name":"ghost","tiers":["high"],"preferred":"high","upgrade_allowed":false}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))

			resp = send(
				http.MethodPut,
				"/api/v1/music/quality-profiles/portable",
				app.adminKey,
				`{"name":"portable","tiers":["high"],"preferred":"lossless","upgrade_allowed":false}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("moves the default, which frees the old one for deletion", func() {
			resp := send(http.MethodPost,
				"/api/v1/music/quality-profiles/portable/default", app.adminKey, "")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

			resp = send(http.MethodDelete,
				"/api/v1/music/quality-profiles/lossless", app.adminKey, "")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("answers 404 setting an unknown profile as default", func() {
			resp := send(http.MethodPost,
				"/api/v1/music/quality-profiles/ghost/default", app.adminKey, "")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	},
)
