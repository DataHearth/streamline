package restapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/restart"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// startFakeOIDC spins up an httptest server that serves a minimal OIDC
// discovery document. Caller closes via DeferCleanup(srv.Close).
func startFakeOIDC() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(
				[]byte(
					`{"issuer":"x","authorization_endpoint":"y","token_endpoint":"z","jwks_uri":"w"}`,
				),
			)
		})
	return httptest.NewServer(mux)
}

var _ = Describe("Handler: Config API", Label("unit", "server", "config"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		configtest.SetupFile(map[string]any{
			"auth": map[string]any{
				"mode":           "full",
				"session_secret": "test-secret-key-for-jwt",
			},
			"metadata": map[string]any{"tmdb_api_key": "test-key"},
		})

		restart.ResetForTest()
		DeferCleanup(restart.ResetForTest)

		app = newAPIKeyApp()
		app.addMember("member@test.com")
	})

	Describe("GetConfigAuth", func() {
		It("returns current auth config to an admin", func() {
			resp := app.do(
				app.req(http.MethodGet, "/api/v1/config/auth", app.adminKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var got AuthConfigView
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(string(got.RegistrationMode)).To(Equal("disabled"))
			Expect(got.SessionTtl).To(Equal("168h"))
		})

		It("rejects a non-admin with 403", func() {
			resp := app.do(
				app.req(http.MethodGet, "/api/v1/config/auth", app.memberKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("UpdateConfigAuth", func() {
		It("applies a valid registration_mode patch", func() {
			body := strings.NewReader(`{"registration_mode":"open"}`)
			resp := app.do(
				jsonReq(app, http.MethodPatch, "/api/v1/config/auth", body),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(config.Get().Auth.RegistrationMode).To(Equal("open"))
		})

		It("returns 422 for invalid session_ttl", func() {
			body := strings.NewReader(`{"session_ttl":"not-a-duration"}`)
			resp := app.do(
				jsonReq(app, http.MethodPatch, "/api/v1/config/auth", body),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("does NOT set restart flag on auth-only changes", func() {
			body := strings.NewReader(`{"session_ttl":"30m"}`)
			resp := app.do(
				jsonReq(app, http.MethodPatch, "/api/v1/config/auth", body),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(restart.Pending()).To(BeFalse())
		})
	})

	Describe("Config library", func() {
		It("round-trips a monitor_specials patch", func() {
			body := strings.NewReader(`{"monitor_specials":true}`)
			resp := app.do(
				jsonReq(app, http.MethodPatch, "/api/v1/config/library", body),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(config.Get().Library.MonitorSpecials).To(BeTrue())

			get := app.do(
				app.req(http.MethodGet, "/api/v1/config/library", app.adminKey, nil),
			)
			defer get.Body.Close()
			var got LibraryConfigView
			Expect(json.NewDecoder(get.Body).Decode(&got)).To(Succeed())
			Expect(got.MonitorSpecials).To(BeTrue())
		})

		It("rejects a non-admin with 403", func() {
			resp := app.do(
				app.req(
					http.MethodGet,
					"/api/v1/config/library",
					app.memberKey,
					nil,
				),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("GET reflects the stored probe defaults, never null", func() {
			get := app.do(
				app.req(http.MethodGet, "/api/v1/config/library", app.adminKey, nil),
			)
			defer get.Body.Close()
			Expect(get.StatusCode).To(Equal(http.StatusOK))
			var got LibraryConfigView
			Expect(json.NewDecoder(get.Body).Decode(&got)).To(Succeed())
			Expect(got.Probe).NotTo(BeNil())
			Expect(got.Probe.AlwaysAsk).NotTo(BeNil())
			Expect(*got.Probe.AlwaysAsk).To(BeFalse())
			Expect(got.Probe.MinDurationRatio).NotTo(BeNil())
			Expect(*got.Probe.MinDurationRatio).To(Equal(0.5))
		})

		It("applies a probe-only patch without disturbing monitor_specials", func() {
			specialsBody := strings.NewReader(`{"monitor_specials":true}`)
			specialsResp := app.do(
				jsonReq(
					app,
					http.MethodPatch,
					"/api/v1/config/library",
					specialsBody,
				),
			)
			specialsResp.Body.Close()
			Expect(specialsResp.StatusCode).To(Equal(http.StatusOK))

			body := strings.NewReader(`{"probe":{"always_ask":true}}`)
			resp := app.do(
				jsonReq(app, http.MethodPatch, "/api/v1/config/library", body),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var got LibraryConfigView
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got.MonitorSpecials).To(BeTrue())
			Expect(*got.Probe.AlwaysAsk).To(BeTrue())
			Expect(*got.Probe.MinDurationRatio).To(Equal(0.5))
		})

		It(
			"applies a probe patch naming only one field without resetting the other",
			func() {
				first := strings.NewReader(`{"probe":{"always_ask":true}}`)
				firstResp := app.do(
					jsonReq(app, http.MethodPatch, "/api/v1/config/library", first),
				)
				firstResp.Body.Close()
				Expect(firstResp.StatusCode).To(Equal(http.StatusOK))

				ratioOnly := strings.NewReader(
					`{"probe":{"min_duration_ratio":0.75}}`,
				)
				resp := app.do(
					jsonReq(
						app,
						http.MethodPatch,
						"/api/v1/config/library",
						ratioOnly,
					),
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var got LibraryConfigView
				Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
				Expect(*got.Probe.MinDurationRatio).To(Equal(0.75))
				Expect(*got.Probe.AlwaysAsk).To(BeTrue())
			},
		)

		It("returns 422 for a min_duration_ratio above 1", func() {
			body := strings.NewReader(`{"probe":{"min_duration_ratio":1.5}}`)
			resp := app.do(
				jsonReq(app, http.MethodPatch, "/api/v1/config/library", body),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})
	})

	Describe("Config ffmpeg", func() {
		It("round-trips an enabled patch and reports the live probe result", func() {
			app.prober.EXPECT().Available().Return(false).Once()

			body := strings.NewReader(`{"enabled":true}`)
			resp := app.do(
				jsonReq(app, http.MethodPatch, "/api/v1/config/ffmpeg", body),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(config.Get().FFmpeg.Enabled).To(BeTrue())
			Expect(restart.Pending()).To(BeFalse())

			var patched FFmpegConfigView
			Expect(json.NewDecoder(resp.Body).Decode(&patched)).To(Succeed())
			Expect(patched.Enabled).To(BeTrue())
			Expect(patched.Found).NotTo(BeNil())
			Expect(*patched.Found).To(BeFalse())
			Expect(patched.RestartRequired).To(BeFalse())

			app.prober.EXPECT().Available().Return(true).Once()
			app.prober.EXPECT().ResolvedPath().Return("/usr/bin/ffprobe").Once()
			get := app.do(
				app.req(http.MethodGet, "/api/v1/config/ffmpeg", app.adminKey, nil),
			)
			defer get.Body.Close()
			Expect(get.StatusCode).To(Equal(http.StatusOK))
			var got FFmpegConfigView
			Expect(json.NewDecoder(get.Body).Decode(&got)).To(Succeed())
			Expect(got.Enabled).To(BeTrue())
			Expect(got.Found).NotTo(BeNil())
			Expect(*got.Found).To(BeTrue())
			Expect(got.ResolvedPath).NotTo(BeNil())
			Expect(*got.ResolvedPath).To(Equal("/usr/bin/ffprobe"))
		})

		It(
			"accepts a path-only partial patch, leaving enabled untouched, and marks a restart",
			func() {
				app.prober.EXPECT().Available().Return(true).Once()
				app.prober.EXPECT().ResolvedPath().Return("/usr/bin/ffprobe").Once()

				// ffmpeg.enabled defaults to true — this patch must not flip it.
				dir := GinkgoT().TempDir()
				body := strings.NewReader(`{"path":"` + dir + `"}`)
				resp := app.do(
					jsonReq(app, http.MethodPatch, "/api/v1/config/ffmpeg", body),
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(config.Get().FFmpeg.Path).To(Equal(dir))
				Expect(config.Get().FFmpeg.Enabled).To(BeTrue())
				Expect(restart.Pending()).To(BeTrue())

				var got FFmpegConfigView
				Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
				Expect(got.RestartRequired).To(BeTrue())
			},
		)

		It(
			"does NOT mark a restart when the patch re-sends the current path unchanged",
			func() {
				dir := GinkgoT().TempDir()

				app.prober.EXPECT().Available().Return(true).Once()
				app.prober.EXPECT().ResolvedPath().Return("/usr/bin/ffprobe").Once()
				first := app.do(
					jsonReq(app, http.MethodPatch, "/api/v1/config/ffmpeg",
						strings.NewReader(`{"path":"`+dir+`"}`)),
				)
				first.Body.Close()
				Expect(first.StatusCode).To(Equal(http.StatusOK))
				restart.ResetForTest()

				app.prober.EXPECT().Available().Return(true).Once()
				app.prober.EXPECT().ResolvedPath().Return("/usr/bin/ffprobe").Once()
				resp := app.do(
					jsonReq(app, http.MethodPatch, "/api/v1/config/ffmpeg",
						strings.NewReader(`{"path":"`+dir+`"}`)),
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(restart.Pending()).To(BeFalse())
			},
		)

		It(
			"preserves an existing non-default path on an enabled-only patch",
			func() {
				app.prober.EXPECT().Available().Return(true).Once()
				app.prober.EXPECT().ResolvedPath().Return("/usr/bin/ffprobe").Once()

				dir := GinkgoT().TempDir()
				pathBody := strings.NewReader(`{"path":"` + dir + `"}`)
				pathResp := app.do(
					jsonReq(
						app,
						http.MethodPatch,
						"/api/v1/config/ffmpeg",
						pathBody,
					),
				)
				pathResp.Body.Close()
				Expect(pathResp.StatusCode).To(Equal(http.StatusOK))
				restart.ResetForTest()

				app.prober.EXPECT().Available().Return(true).Once()
				app.prober.EXPECT().ResolvedPath().Return("/usr/bin/ffprobe").Once()
				enabledBody := strings.NewReader(`{"enabled":false}`)
				resp := app.do(
					jsonReq(
						app,
						http.MethodPatch,
						"/api/v1/config/ffmpeg",
						enabledBody,
					),
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(config.Get().FFmpeg.Path).To(Equal(dir))
				Expect(config.Get().FFmpeg.Enabled).To(BeFalse())
				Expect(restart.Pending()).To(BeFalse())
			},
		)

		It("ignores found and resolved_path sent in the patch body", func() {
			app.prober.EXPECT().Available().Return(false).Once()

			body := strings.NewReader(
				`{"enabled":true,"found":true,"resolved_path":"/usr/bin/ffprobe"}`,
			)
			resp := app.do(
				jsonReq(app, http.MethodPatch, "/api/v1/config/ffmpeg", body),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var got FFmpegConfigView
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got.Found).NotTo(BeNil())
			Expect(*got.Found).To(BeFalse())
		})

		// The path deliberately isn't validated for existence: ffmpeg being
		// absent is a graceful degrade surfaced by the live prober as
		// found=false, and an existence check here would also poison every
		// unrelated config.Update while the directory is unavailable.
		It("stores a path that doesn't exist and reports found=false", func() {
			app.prober.EXPECT().Available().Return(false).Once()

			body := strings.NewReader(`{"path":"/does/not/exist"}`)
			resp := app.do(
				jsonReq(app, http.MethodPatch, "/api/v1/config/ffmpeg", body),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var got FFmpegConfigView
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got.Found).NotTo(BeNil())
			Expect(*got.Found).To(BeFalse())
			Expect(config.Get().FFmpeg.Path).To(Equal("/does/not/exist"))
			Expect(restart.Pending()).To(BeTrue())
			restart.ResetForTest()
		})

		It("rejects a non-admin with 403", func() {
			resp := app.do(
				app.req(http.MethodGet, "/api/v1/config/ffmpeg", app.memberKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("OIDC provider CRUD", func() {
		var oidc *httptest.Server

		BeforeEach(func() {
			oidc = startFakeOIDC()
			DeferCleanup(oidc.Close)
		})

		It("full CRUD flow flips the restart flag on every mutation", func() {
			By("list returns empty + restart_required=false")
			resp := app.do(
				app.req(http.MethodGet, "/api/v1/config/oidc", app.adminKey, nil),
			)
			var list OIDCProviderListView
			Expect(json.NewDecoder(resp.Body).Decode(&list)).To(Succeed())
			resp.Body.Close()
			Expect(list.Providers).To(BeEmpty())
			Expect(list.RestartRequired).To(BeFalse())

			By("POST /config/oidc creates provider + flips restart")
			body := `{"name":"acme","issuer":"` + oidc.URL + `","client_id":"cid","client_secret":"s"}`
			resp = app.do(
				jsonReq(
					app,
					http.MethodPost,
					"/api/v1/config/oidc",
					strings.NewReader(body),
				),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			Expect(restart.Pending()).To(BeTrue())

			By("list reflects the new provider + restart_required=true")
			resp2 := app.do(
				app.req(http.MethodGet, "/api/v1/config/oidc", app.adminKey, nil),
			)
			Expect(json.NewDecoder(resp2.Body).Decode(&list)).To(Succeed())
			resp2.Body.Close()
			Expect(list.Providers).To(HaveLen(1))
			Expect(list.RestartRequired).To(BeTrue())

			By("GET /config/oidc/acme returns provider")
			respGet := app.do(
				app.req(
					http.MethodGet,
					"/api/v1/config/oidc/acme",
					app.adminKey,
					nil,
				),
			)
			var view OIDCProviderView
			Expect(json.NewDecoder(respGet.Body).Decode(&view)).To(Succeed())
			respGet.Body.Close()
			Expect(view.Name).To(Equal("acme"))
			Expect(view.ClientSecretSet).To(BeTrue())

			By("PATCH with blank client_secret keeps the stored secret")
			patch := `{"client_id":"new-cid","client_secret":""}`
			respP := app.do(
				jsonReq(
					app,
					http.MethodPatch,
					"/api/v1/config/oidc/acme",
					strings.NewReader(patch),
				),
			)
			defer respP.Body.Close()
			Expect(respP.StatusCode).To(Equal(http.StatusOK))
			Expect(config.Get().Auth.OIDC[0].ClientID).To(Equal("new-cid"))
			Expect(config.Get().Auth.OIDC[0].ClientSecret).To(Equal("s"))

			By("DELETE /config/oidc/acme removes it")
			respD := app.do(
				app.req(
					http.MethodDelete,
					"/api/v1/config/oidc/acme",
					app.adminKey,
					nil,
				),
			)
			respD.Body.Close()
			Expect(respD.StatusCode).To(Equal(http.StatusNoContent))
			Expect(config.Get().Auth.OIDC).To(BeEmpty())
		})

		It("rejects duplicate name with 409", func() {
			body := `{"name":"dup","issuer":"` + oidc.URL + `","client_id":"cid","client_secret":"s"}`
			resp := app.do(
				jsonReq(
					app,
					http.MethodPost,
					"/api/v1/config/oidc",
					strings.NewReader(body),
				),
			)
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))

			resp = app.do(
				jsonReq(
					app,
					http.MethodPost,
					"/api/v1/config/oidc",
					strings.NewReader(body),
				),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		It("rejects unreachable issuer with 422", func() {
			body := `{"name":"bad","issuer":"http://127.0.0.1:1","client_id":"c","client_secret":"s"}`
			resp := app.do(
				jsonReq(
					app,
					http.MethodPost,
					"/api/v1/config/oidc",
					strings.NewReader(body),
				),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("returns 404 on missing provider for GET / PATCH / DELETE", func() {
			resp := app.do(
				app.req(
					http.MethodGet,
					"/api/v1/config/oidc/ghost",
					app.adminKey,
					nil,
				),
			)
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))

			resp = app.do(jsonReq(app, http.MethodPatch, "/api/v1/config/oidc/ghost",
				strings.NewReader(`{"client_id":"x"}`)))
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))

			resp = app.do(
				app.req(
					http.MethodDelete,
					"/api/v1/config/oidc/ghost",
					app.adminKey,
					nil,
				),
			)
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("rejects non-admin callers with 403", func() {
			resp := app.do(
				app.req(http.MethodGet, "/api/v1/config/oidc", app.memberKey, nil),
			)
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})
})

// jsonReq is a small helper for JSON POST/PATCH calls as the admin.
func jsonReq(app *apiKeyApp, method, path string, body io.Reader) *http.Request {
	GinkgoHelper()
	req := app.req(method, path, app.adminKey, body)
	req.Header.Set("Content-Type", "application/json")
	return req
}
