package restapi

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func dlClientOverride(entries ...map[string]any) map[string]any {
	return map[string]any{"download_clients": entries}
}

var _ = Describe(
	"Handler: DownloadClients",
	Label("unit", "server", "downloads"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			configtest.SetupFile()
			app = newAPIKeyApp()
		})

		Describe("ListDownloadClients", func() {
			It("returns empty array when none exist", func() {
				resp, err := http.Get(app.srv.URL + "/api/v1/download-clients")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var clients []DownloadClient
				Expect(json.NewDecoder(resp.Body).Decode(&clients)).To(Succeed())
				Expect(clients).To(BeEmpty())
			})

			It("returns clients with optional fields populated", func() {
				configtest.SetupFile(dlClientOverride(map[string]any{
					"name": "qbt-test", "client_type": "qbittorrent",
					"host": "localhost", "port": 8080, "auth_method": "password",
					"username": "admin", "password": "admin",
					"use_ssl": true, "priority": 5, "enabled": true,
				}))

				resp, err := http.Get(app.srv.URL + "/api/v1/download-clients")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var clients []DownloadClient
				Expect(json.NewDecoder(resp.Body).Decode(&clients)).To(Succeed())
				Expect(clients).To(HaveLen(1))
				Expect(clients[0].Name).To(Equal("qbt-test"))
				Expect(string(clients[0].ClientType)).To(Equal("qbittorrent"))
				Expect(clients[0].Username).NotTo(BeNil())
				Expect(*clients[0].Username).To(Equal("admin"))
				Expect(clients[0].PasswordSet).To(BeTrue())
				Expect(clients[0].UseSsl).NotTo(BeNil())
				Expect(*clients[0].UseSsl).To(BeTrue())
				Expect(clients[0].Priority).NotTo(BeNil())
				Expect(*clients[0].Priority).To(Equal(uint8(5)))
			})
		})

		Describe("CreateDownloadClient", func() {
			It("creates client with required fields", func() {
				body := `{"name": "qbt-min", "client_type": "qbittorrent", "host": "localhost", "port": 8080}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/download-clients",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var dc DownloadClient
				Expect(json.NewDecoder(resp.Body).Decode(&dc)).To(Succeed())
				Expect(dc.Name).To(Equal("qbt-min"))
				Expect(dc.Host).To(Equal("localhost"))
				Expect(dc.Port).To(Equal(uint16(8080)))

				got, ok := config.FindDownloadClient("qbt-min")
				Expect(ok).To(BeTrue())
				Expect(got.Host).To(Equal("localhost"))
			})

			It("creates client with all optional fields", func() {
				body := `{
					"name": "qbt-full",
					"client_type": "qbittorrent",
					"host": "localhost",
					"port": 8080,
					"username": "admin",
					"password": "secret",
					"use_ssl": true,
					"priority": 10,
					"enabled": false
				}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/download-clients",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var dc DownloadClient
				Expect(json.NewDecoder(resp.Body).Decode(&dc)).To(Succeed())
				Expect(dc.Enabled).To(BeFalse())
				Expect(dc.Username).NotTo(BeNil())
				Expect(*dc.Username).To(Equal("admin"))
				Expect(dc.PasswordSet).To(BeTrue())
			})

			It("returns 409 on duplicate name", func() {
				configtest.SetupFile(dlClientOverride(map[string]any{
					"name": "dup", "client_type": "qbittorrent",
					"host": "h", "port": 8080, "auth_method": "password",
				}))
				body := `{"name": "dup", "client_type": "qbittorrent", "host": "h2", "port": 8081}`
				resp, err := http.Post(
					app.srv.URL+"/api/v1/download-clients",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})
		})

		Describe("TestDownloadClient", func() {
			It("returns 404 when client does not exist", func() {
				app.downloads.EXPECT().
					TestByName(mock.Anything, "ghost").
					Return(config.ErrDownloadClientNotFound).
					Once()

				req := app.req(http.MethodPost,
					"/api/v1/download-clients/ghost/test", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})

			It("returns 200 when client is reachable", func() {
				app.downloads.EXPECT().
					TestByName(mock.Anything, "qbt").
					Return(nil).
					Once()

				req := app.req(http.MethodPost,
					"/api/v1/download-clients/qbt/test", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})

			It("returns 422 for unsupported client type", func() {
				app.downloads.EXPECT().
					TestByName(mock.Anything, "tx").
					Return(download.ErrUnsupportedClient).
					Once()

				req := app.req(http.MethodPost,
					"/api/v1/download-clients/tx/test", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			})
		})

		Describe("UpdateDownloadClient", func() {
			It("updates fields and returns 200", func() {
				configtest.SetupFile(dlClientOverride(map[string]any{
					"name": "qbt", "client_type": "qbittorrent",
					"host": "old", "port": 8080, "auth_method": "password",
					"password": "keep", "enabled": true,
				}))

				body := `{
					"name": "qbt",
					"client_type": "qbittorrent",
					"host": "new",
					"port": 2222,
					"enabled": false,
					"priority": 7
				}`
				req := app.req(http.MethodPut, "/api/v1/download-clients/qbt", "",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var updated DownloadClient
				Expect(json.NewDecoder(resp.Body).Decode(&updated)).To(Succeed())
				Expect(updated.Host).To(Equal("new"))
				Expect(updated.Port).To(Equal(uint16(2222)))
				Expect(updated.Enabled).To(BeFalse())
				Expect(updated.Priority).NotTo(BeNil())
				Expect(*updated.Priority).To(Equal(uint8(7)))

				// Blank password preserves the existing secret.
				got, _ := config.FindDownloadClient("qbt")
				Expect(got.Password).To(Equal("keep"))
			})

			It("returns 404 for nonexistent client", func() {
				body := `{"name": "x", "client_type": "qbittorrent", "host": "x", "port": 1}`
				req := app.req(http.MethodPut, "/api/v1/download-clients/ghost", "",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})

		Describe("DeleteDownloadClient", func() {
			It("deletes existing client and returns 204", func() {
				configtest.SetupFile(dlClientOverride(map[string]any{
					"name": "gone", "client_type": "qbittorrent",
					"host": "h", "port": 8080, "auth_method": "password",
				}))

				req := app.req(http.MethodDelete,
					"/api/v1/download-clients/gone", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

				_, ok := config.FindDownloadClient("gone")
				Expect(ok).To(BeFalse())
			})

			It("returns 404 for nonexistent client", func() {
				req := app.req(http.MethodDelete,
					"/api/v1/download-clients/ghost", "", nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})
	},
)
