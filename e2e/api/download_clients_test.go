package api

import (
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/containers"
)

type downloadClientView struct {
	Name        string `json:"name"`
	ClientType  string `json:"client_type"`
	Host        string `json:"host"`
	Port        uint16 `json:"port"`
	Priority    uint8  `json:"priority"`
	Enabled     bool   `json:"enabled"`
	PasswordSet bool   `json:"password_set"`
}

// deleteDownloadClientLater schedules the named client's removal. Callers
// register it before their first assertion so a later failure cannot leak the
// entity; 404 covers specs that delete it themselves.
func deleteDownloadClientLater(name string) {
	GinkgoHelper()
	DeferCleanup(func() {
		cleanup := del("/api/v1/download-clients/"+name, adminAuth, nil)
		defer cleanup.Body.Close()
		Expect(cleanup.StatusCode).To(BeElementOf(
			http.StatusNoContent, http.StatusNotFound,
		))
	})
}

// createDownloadClient adds a disabled qBittorrent entry pointed at a closed
// local port and schedules its removal.
func createDownloadClient(name string) downloadClientView {
	GinkgoHelper()
	resp := post("/api/v1/download-clients", adminAuth, map[string]any{
		"name":        name,
		"client_type": "qbittorrent",
		"host":        "127.0.0.1",
		"port":        1,
		"auth_method": "password",
		"username":    "e2e",
		"password":    "e2e-secret",
		"priority":    2,
		"enabled":     false,
	})
	defer resp.Body.Close()
	deleteDownloadClientLater(name)
	Expect(resp.StatusCode).To(Equal(http.StatusCreated))
	var dc downloadClientView
	decode(resp, &dc)
	return dc
}

var _ = Describe("REST API download clients", Label("e2e"), func() {
	It("lists configured download clients", func() {
		resp := get("/api/v1/download-clients", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var clients []downloadClientView
		decode(resp, &clients)
		Expect(clients).NotTo(BeNil())
	})

	It("creates, updates and deletes a download client", func() {
		dc := createDownloadClient("e2e-dc")
		Expect(dc.Name).To(Equal("e2e-dc"))
		Expect(dc.ClientType).To(Equal("qbittorrent"))
		Expect(dc.Port).To(BeEquivalentTo(1))
		Expect(dc.PasswordSet).To(BeTrue())
		Expect(dc.Enabled).To(BeFalse())

		updated := put("/api/v1/download-clients/e2e-dc", adminAuth, map[string]any{
			"name":        "e2e-dc",
			"client_type": "qbittorrent",
			"host":        "127.0.0.2",
			"port":        2,
			"auth_method": "password",
			"priority":    9,
			"enabled":     false,
		})
		defer updated.Body.Close()
		Expect(updated.StatusCode).To(Equal(http.StatusOK))
		decode(updated, &dc)
		Expect(dc.Host).To(Equal("127.0.0.2"))
		Expect(dc.Priority).To(BeEquivalentTo(9))
		// A blank password preserves the stored secret.
		Expect(dc.PasswordSet).To(BeTrue())

		deleted := del("/api/v1/download-clients/e2e-dc", adminAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusNoContent))
	})

	It("409s a duplicate download client name", func() {
		createDownloadClient("e2e-dc-dup")
		resp := post("/api/v1/download-clients", adminAuth, map[string]any{
			"name":        "e2e-dc-dup",
			"client_type": "qbittorrent",
			"host":        "127.0.0.1",
			"port":        1,
			"auth_method": "password",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusConflict))
	})

	It("422s a download client that fails config validation", func() {
		resp := post("/api/v1/download-clients", adminAuth, map[string]any{
			"name":        "e2e-dc-bad",
			"client_type": "qbittorrent",
			"port":        1,
			"auth_method": "password",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
	})

	It("404s updating and deleting an unknown download client", func() {
		updated := put(
			"/api/v1/download-clients/e2e-missing",
			adminAuth,
			map[string]any{
				"name":        "e2e-missing",
				"client_type": "qbittorrent",
				"host":        "127.0.0.1",
				"port":        1,
				"auth_method": "password",
			},
		)
		defer updated.Body.Close()
		Expect(updated.StatusCode).To(Equal(http.StatusNotFound))

		deleted := del("/api/v1/download-clients/e2e-missing", adminAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusNotFound))
	})

	Describe("connection tests", func() {
		It("404s testing an unknown download client", func() {
			resp := post("/api/v1/download-clients/e2e-missing/test", adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("422s testing a saved client that refuses connections", func() {
			createDownloadClient("e2e-dc-test")
			resp := post("/api/v1/download-clients/e2e-dc-test/test", adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("422s testing a draft client that refuses connections", func() {
			resp := post("/api/v1/download-clients/test", adminAuth, map[string]any{
				"name":        "e2e-draft",
				"client_type": "qbittorrent",
				"host":        "127.0.0.1",
				"port":        1,
				"auth_method": "password",
				"username":    "e2e",
				"password":    "e2e",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})
	})

	It("403s mutations and tests for a non-admin", func() {
		created := post("/api/v1/download-clients", viewerAuth, map[string]any{
			"name":        "e2e-nope",
			"client_type": "qbittorrent",
			"host":        "127.0.0.1",
			"port":        1,
			"auth_method": "password",
		})
		defer created.Body.Close()
		Expect(created.StatusCode).To(Equal(http.StatusForbidden))

		updated := put(
			"/api/v1/download-clients/e2e-nope",
			viewerAuth,
			map[string]any{
				"name":        "e2e-nope",
				"client_type": "qbittorrent",
				"host":        "127.0.0.1",
				"port":        1,
				"auth_method": "password",
			},
		)
		defer updated.Body.Close()
		Expect(updated.StatusCode).To(Equal(http.StatusForbidden))

		deleted := del("/api/v1/download-clients/e2e-nope", viewerAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusForbidden))

		tested := post("/api/v1/download-clients/e2e-nope/test", viewerAuth, nil)
		defer tested.Body.Close()
		Expect(tested.StatusCode).To(Equal(http.StatusForbidden))

		draft := post("/api/v1/download-clients/test", viewerAuth, map[string]any{
			"name":        "e2e-nope",
			"client_type": "qbittorrent",
			"host":        "127.0.0.1",
			"port":        1,
			"auth_method": "password",
		})
		defer draft.Body.Close()
		Expect(draft.StatusCode).To(Equal(http.StatusForbidden))
	})
})

var _ = Describe(
	"Download clients against qBittorrent",
	Label("e2e", "containers"),
	func() {
		It("tests a live qbittorrent client", func() {
			containers.Require()
			qb := containers.StartQBittorrent(downloadDir)

			By("serving the suite download dir as the container's save path")
			prefs, err := httpClient.Get(fmt.Sprintf(
				"http://%s:%d/api/v2/app/preferences", qb.Host, qb.Port,
			))
			Expect(err).NotTo(HaveOccurred())
			defer prefs.Body.Close()
			Expect(prefs.StatusCode).To(Equal(http.StatusOK))
			var settings struct {
				SavePath string `json:"save_path"`
			}
			decode(prefs, &settings)
			// A follow-up pipeline spec will write release bytes here and
			// expect qBittorrent to recheck them in place.
			Expect(settings.SavePath).To(Equal(downloadDir))

			const name = "e2e-qbit-live"
			created := post("/api/v1/download-clients", adminAuth, map[string]any{
				"name":        name,
				"client_type": "qbittorrent",
				"host":        qb.Host,
				"port":        qb.Port,
				"auth_method": "password",
				// The container whitelists every subnet, so these are accepted
				// without being validated: the test below proves the client
				// reaches the container and its login endpoint answers, not
				// that credentials are carried correctly.
				"username": "e2e",
				"password": "e2e-secret",
				"enabled":  true,
			})
			defer created.Body.Close()
			deleteDownloadClientLater(name)
			Expect(created.StatusCode).To(Equal(http.StatusCreated))

			resp := post("/api/v1/download-clients/"+name+"/test", adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(
				Equal(http.StatusOK),
				"connection test failed: %s", bodyText(resp),
			)
		})
	},
)
