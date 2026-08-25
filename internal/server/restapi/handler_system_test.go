package restapi

import (
	"encoding/json"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("GetSystemInfo", Label("unit", "restapi"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		app = newAPIKeyApp()
		app.addMember("member@test.com")
	})

	It("rejects a non-admin with 403", func() {
		resp := app.do(
			app.req(http.MethodGet, "/api/v1/system/info", app.memberKey, nil),
		)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
	})

	It(
		"sets ffmpeg_warn when ffmpeg is enabled and the prober can't find ffprobe",
		func() {
			configtest.Setup(map[string]any{
				"auth": map[string]any{
					"session_secret": "test-secret-key-for-jwt",
					"session_ttl":    "1h",
				},
				"metadata": map[string]any{"tmdb_api_key": "test-key"},
				"ffmpeg":   map[string]any{"enabled": true},
			})
			app.prober.EXPECT().Available().Return(false).Once()

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/system/info", app.adminKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var got SystemInfo
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got.FfmpegWarn).NotTo(BeNil())
			Expect(*got.FfmpegWarn).To(BeTrue())
		},
	)

	It(
		"leaves ffmpeg_warn unset when ffmpeg is disabled, even if the prober can't find ffprobe",
		func() {
			configtest.Setup(map[string]any{
				"auth": map[string]any{
					"session_secret": "test-secret-key-for-jwt",
					"session_ttl":    "1h",
				},
				"metadata": map[string]any{"tmdb_api_key": "test-key"},
				"ffmpeg":   map[string]any{"enabled": false},
			})

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/system/info", app.adminKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var got SystemInfo
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got.FfmpegWarn).To(BeNil())
		},
	)

	It(
		"leaves ffmpeg_warn unset when ffmpeg is enabled and the prober finds ffprobe",
		func() {
			configtest.Setup(map[string]any{
				"auth": map[string]any{
					"session_secret": "test-secret-key-for-jwt",
					"session_ttl":    "1h",
				},
				"metadata": map[string]any{"tmdb_api_key": "test-key"},
				"ffmpeg":   map[string]any{"enabled": true},
			})
			app.prober.EXPECT().Available().Return(true).Once()

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/system/info", app.adminKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var got SystemInfo
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got.FfmpegWarn).To(BeNil())
		},
	)
})
