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

var _ = Describe(
	"Handler: MusicQualityProfiles",
	Label("unit", "server", "music"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			configtest.SetupFile(map[string]any{
				"music_quality_profiles": []map[string]any{
					{
						"name":    "lossless",
						"formats": []string{"flac", "flac-24"},
						"cutoff":  "flac",
					},
					{
						"name":    "portable",
						"formats": []string{"mp3-320"},
						"cutoff":  "mp3-320",
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

		It("lists the profiles and flags the default", func() {
			resp := send(
				http.MethodGet,
				"/api/v1/music/quality-profiles",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var items []MusicQualityProfile
			Expect(json.NewDecoder(resp.Body).Decode(&items)).To(Succeed())
			Expect(items).To(HaveLen(2))
			Expect(items[0].IsDefault).To(BeTrue())
			Expect(items[1].IsDefault).To(BeFalse())
		})

		It("creates a profile and persists it to config", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/music/quality-profiles",
				app.adminKey,
				`{"name":"mid","formats":["mp3-v0","mp3-256"],"cutoff":"mp3-v0","upgrade_allowed":true}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))

			got, ok := config.ResolveMusicQualityProfile("mid")
			Expect(ok).To(BeTrue())
			Expect(got.Name).To(Equal("mid"))
			Expect(got.UpgradeAllowed).To(BeTrue())
		})

		It("answers 409 for a duplicate name", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/music/quality-profiles",
				app.adminKey,
				`{"name":"lossless","formats":["flac"],"cutoff":"flac","upgrade_allowed":false}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		It("answers 422 for a cutoff outside the format set", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/music/quality-profiles",
				app.adminKey,
				`{"name":"bad","formats":["flac"],"cutoff":"wav","upgrade_allowed":false}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("updates a profile by path name", func() {
			resp := send(
				http.MethodPut,
				"/api/v1/music/quality-profiles/portable",
				app.adminKey,
				`{"name":"ignored","formats":["mp3-320","mp3-v0"],"cutoff":"mp3-v0","upgrade_allowed":true}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			got, ok := config.ResolveMusicQualityProfile("portable")
			Expect(ok).To(BeTrue())
			Expect(got.Name).To(Equal("portable"))
			Expect(got.Cutoff).To(Equal("mp3-v0"))
		})

		It("404s when updating an unknown profile", func() {
			resp := send(
				http.MethodPut,
				"/api/v1/music/quality-profiles/ghost",
				app.adminKey,
				`{"name":"ghost","formats":["flac"],"cutoff":"flac","upgrade_allowed":false}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

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

		It("answers 403 to a non-admin writing a profile", func() {
			app.addMember("")
			resp := send(
				http.MethodPost,
				"/api/v1/music/quality-profiles",
				app.memberKey,
				`{"name":"mid","formats":["mp3-v0"],"cutoff":"mp3-v0","upgrade_allowed":false}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	},
)
