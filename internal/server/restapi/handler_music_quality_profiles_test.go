package restapi

import (
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

	},
)
