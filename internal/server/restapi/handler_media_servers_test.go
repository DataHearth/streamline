package restapi

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/mediaserver"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func mediaServerOverride(entries ...map[string]any) map[string]any {
	return map[string]any{"media_server": map[string]any{"servers": entries}}
}

var _ = Describe(
	"Handler: MediaServers",
	Label("unit", "server", "media-servers"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			configtest.SetupFile()
			app = newAPIKeyApp()
		})

		Describe("ListMediaServers", func() {
			It("returns empty array when none exist", func() {
				resp, err := http.Get(app.srv.URL + "/api/v1/media-servers")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var view MediaServerListView
				Expect(json.NewDecoder(resp.Body).Decode(&view)).To(Succeed())
				items := view.Items
				Expect(items).To(BeEmpty())
			})

			It("returns servers with api_key_set populated", func() {
				configtest.SetupFile(mediaServerOverride(map[string]any{
					"name": "home", "server_type": "plex",
					"host": "http://plex:32400", "api_key": "tok", "enabled": true,
				}))

				resp, err := http.Get(app.srv.URL + "/api/v1/media-servers")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var view MediaServerListView
				Expect(json.NewDecoder(resp.Body).Decode(&view)).To(Succeed())
				items := view.Items
				Expect(items).To(HaveLen(1))
				Expect(items[0].Name).To(Equal("home"))
				Expect(items[0].ApiKeySet).To(BeTrue())
			})
		})

		Describe("GetMediaServer", func() {
			It("returns the server when present", func() {
				configtest.SetupFile(mediaServerOverride(map[string]any{
					"name": "home", "server_type": "plex",
					"host": "http://plex:32400", "api_key": "tok", "enabled": true,
				}))

				resp, err := http.Get(app.srv.URL + "/api/v1/media-servers/home")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var ms MediaServer
				Expect(json.NewDecoder(resp.Body).Decode(&ms)).To(Succeed())
				Expect(ms.Name).To(Equal("home"))
			})

			It("returns 404 for an unknown name", func() {
				resp, err := http.Get(app.srv.URL + "/api/v1/media-servers/ghost")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})

		Describe("CreateMediaServer", func() {
			It("creates a server and persists it to config", func() {
				body := `{"name": "home", "server_type": "plex", "host": "http://plex:32400", "api_key": "tok"}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/media-servers",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var ms MediaServer
				Expect(json.NewDecoder(resp.Body).Decode(&ms)).To(Succeed())
				Expect(ms.Name).To(Equal("home"))
				Expect(ms.ApiKeySet).To(BeTrue())

				got, ok := config.FindMediaServer("home")
				Expect(ok).To(BeTrue())
				Expect(got.Host).To(Equal("http://plex:32400"))
			})

			It("rejects library_section on non-Plex types with 422", func() {
				body := `{"name": "jf", "server_type": "jellyfin", "host": "http://jf", "api_key": "k", "library_section": "1"}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/media-servers",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			})

			It("returns 409 on duplicate name", func() {
				configtest.SetupFile(mediaServerOverride(map[string]any{
					"name": "dup", "server_type": "plex",
					"host": "http://plex", "api_key": "tok",
				}))
				body := `{"name": "dup", "server_type": "plex", "host": "http://other", "api_key": "tok"}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/media-servers",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})
		})

		Describe("UpdateMediaServer", func() {
			It("updates fields and returns 200, preserving blank api_key", func() {
				configtest.SetupFile(mediaServerOverride(map[string]any{
					"name": "home", "server_type": "plex",
					"host": "http://old", "api_key": "keep", "enabled": true,
				}))

				body := `{"name": "home", "server_type": "plex", "host": "http://new", "enabled": false}`
				req := app.req(http.MethodPatch, "/api/v1/media-servers/home", "",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				got, _ := config.FindMediaServer("home")
				Expect(got.Host).To(Equal("http://new"))
				Expect(got.APIKey).To(Equal("keep"))
			})

			It("returns 404 for nonexistent server", func() {
				body := `{"name": "x", "server_type": "plex", "host": "http://x"}`
				req := app.req(http.MethodPatch, "/api/v1/media-servers/ghost", "",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})

		Describe("DeleteMediaServer", func() {
			It("deletes existing server and returns 204", func() {
				configtest.SetupFile(mediaServerOverride(map[string]any{
					"name": "gone", "server_type": "plex",
					"host": "http://plex", "api_key": "tok",
				}))

				req := app.req(
					http.MethodDelete,
					"/api/v1/media-servers/gone",
					"",
					nil,
				)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

				_, ok := config.FindMediaServer("gone")
				Expect(ok).To(BeFalse())
			})

			It("returns 404 for nonexistent server", func() {
				req := app.req(
					http.MethodDelete,
					"/api/v1/media-servers/ghost",
					"",
					nil,
				)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})

		Describe("TestMediaServer", func() {
			It("returns 200 when reachable", func() {
				app.mediaServers.EXPECT().
					TestByName(mock.Anything, "home").
					Return(nil).
					Once()

				req := app.req(
					http.MethodPost,
					"/api/v1/media-servers/home/test",
					"",
					nil,
				)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})

			It("returns 404 when the server is unknown", func() {
				app.mediaServers.EXPECT().
					TestByName(mock.Anything, "ghost").
					Return(mediaserver.ErrServerNotFound).
					Once()

				req := app.req(
					http.MethodPost,
					"/api/v1/media-servers/ghost/test",
					"",
					nil,
				)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})

			It("returns 422 on a failed connection test", func() {
				app.mediaServers.EXPECT().
					TestByName(mock.Anything, "home").
					Return(mediaserver.ErrTestFailed).
					Once()

				req := app.req(
					http.MethodPost,
					"/api/v1/media-servers/home/test",
					"",
					nil,
				)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			})
		})
	},
)
