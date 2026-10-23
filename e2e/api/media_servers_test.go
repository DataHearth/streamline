package api

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type mediaServerView struct {
	Name           string  `json:"name"`
	ServerType     string  `json:"server_type"`
	Host           string  `json:"host"`
	LibrarySection *string `json:"library_section"`
	Enabled        bool    `json:"enabled"`
	ApiKeySet      bool    `json:"api_key_set"`
}

// createMediaServer adds a disabled entry pointed at a closed local port and
// schedules its removal. library_section is Plex-only, so it is set only for
// serverType "plex". Cleanup is registered before the first assertion so a
// later failure cannot leak the entity; 404 covers specs that delete it
// themselves.
func createMediaServer(name, serverType string) mediaServerView {
	GinkgoHelper()
	body := map[string]any{
		"name":        name,
		"server_type": serverType,
		"host":        "http://127.0.0.1:1",
		"api_key":     "e2e-ms-key",
		"enabled":     false,
	}
	if serverType == "plex" {
		body["library_section"] = "7"
	}
	resp := post("/api/v1/media-servers", adminAuth, body)
	defer resp.Body.Close()
	DeferCleanup(func() {
		cleanup := del("/api/v1/media-servers/"+name, adminAuth, nil)
		defer cleanup.Body.Close()
		Expect(cleanup.StatusCode).To(BeElementOf(
			http.StatusNoContent, http.StatusNotFound,
		))
	})
	Expect(resp.StatusCode).To(Equal(http.StatusCreated))
	var server mediaServerView
	decode(resp, &server)
	return server
}

var _ = Describe("REST API media servers", Label("e2e"), func() {
	It("lists configured media servers", func() {
		resp := get("/api/v1/media-servers", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var list struct {
			Items []mediaServerView `json:"items"`
		}
		decode(resp, &list)
		Expect(list.Items).NotTo(BeNil())
	})

	It("creates, reads, patches and deletes a media server", func() {
		// library_section is a Plex-only field, so the CRUD walk uses a Plex
		// entry to cover it.
		server := createMediaServer("e2e-ms", "plex")
		Expect(server.Name).To(Equal("e2e-ms"))
		Expect(server.ServerType).To(Equal("plex"))
		Expect(server.ApiKeySet).To(BeTrue())
		Expect(server.LibrarySection).NotTo(BeNil())
		Expect(*server.LibrarySection).To(Equal("7"))

		read := get("/api/v1/media-servers/e2e-ms", adminAuth)
		defer read.Body.Close()
		Expect(read.StatusCode).To(Equal(http.StatusOK))
		decode(read, &server)
		Expect(server.Host).To(Equal("http://127.0.0.1:1"))

		patched := patch("/api/v1/media-servers/e2e-ms", adminAuth, map[string]any{
			"host": "http://127.0.0.2:1",
		})
		defer patched.Body.Close()
		Expect(patched.StatusCode).To(Equal(http.StatusOK))
		decode(patched, &server)
		Expect(server.Host).To(Equal("http://127.0.0.2:1"))
		// A blank api_key preserves the stored secret.
		Expect(server.ApiKeySet).To(BeTrue())

		deleted := del("/api/v1/media-servers/e2e-ms", adminAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusNoContent))
	})

	It("409s a duplicate media server name", func() {
		createMediaServer("e2e-ms-dup", "jellyfin")
		resp := post("/api/v1/media-servers", adminAuth, map[string]any{
			"name":        "e2e-ms-dup",
			"server_type": "jellyfin",
			"host":        "http://127.0.0.1:1",
			"api_key":     "k",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusConflict))
	})

	It("422s a media server that fails config validation", func() {
		resp := post("/api/v1/media-servers", adminAuth, map[string]any{
			"name":        "e2e-ms-bad",
			"server_type": "kodi",
			"host":        "http://127.0.0.1:1",
			"api_key":     "k",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
	})

	It("422s a library_section on a non-Plex server", func() {
		resp := post("/api/v1/media-servers", adminAuth, map[string]any{
			"name":            "e2e-ms-section",
			"server_type":     "jellyfin",
			"host":            "http://127.0.0.1:1",
			"api_key":         "k",
			"library_section": "7",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
	})

	It("404s reading, patching and deleting an unknown media server", func() {
		read := get("/api/v1/media-servers/e2e-missing", adminAuth)
		defer read.Body.Close()
		Expect(read.StatusCode).To(Equal(http.StatusNotFound))

		patched := patch(
			"/api/v1/media-servers/e2e-missing",
			adminAuth,
			map[string]any{
				"host": "http://127.0.0.1:1",
			},
		)
		defer patched.Body.Close()
		Expect(patched.StatusCode).To(Equal(http.StatusNotFound))

		deleted := del("/api/v1/media-servers/e2e-missing", adminAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusNotFound))
	})

	Describe("connection tests and discovery", func() {
		It("404s testing an unknown media server", func() {
			resp := post("/api/v1/media-servers/e2e-missing/test", adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("422s testing a saved server that refuses connections", func() {
			createMediaServer("e2e-ms-test", "jellyfin")
			resp := post("/api/v1/media-servers/e2e-ms-test/test", adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("422s testing a draft server that refuses connections", func() {
			resp := post("/api/v1/media-servers/test", adminAuth, map[string]any{
				"server_type": "jellyfin",
				"host":        "http://127.0.0.1:1",
				"api_key":     "k",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		// Section discovery is Plex-only; non-Plex types short-circuit to an
		// empty list without touching the network.
		It("returns no sections for a non-Plex draft server", func() {
			resp := post("/api/v1/media-servers/discover", adminAuth, map[string]any{
				"server_type": "jellyfin",
				"host":        "http://127.0.0.1:1",
				"api_key":     "k",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var discovered struct {
				Sections []struct {
					Key string `json:"key"`
				} `json:"sections"`
			}
			decode(resp, &discovered)
			Expect(discovered.Sections).To(BeEmpty())
		})
	})

	It("403s mutations, tests and discovery for a non-admin", func() {
		created := post("/api/v1/media-servers", viewerAuth, map[string]any{
			"name":        "e2e-nope",
			"server_type": "jellyfin",
			"host":        "http://127.0.0.1:1",
			"api_key":     "k",
		})
		defer created.Body.Close()
		Expect(created.StatusCode).To(Equal(http.StatusForbidden))

		patched := patch(
			"/api/v1/media-servers/e2e-nope",
			viewerAuth,
			map[string]any{
				"host": "http://127.0.0.1:1",
			},
		)
		defer patched.Body.Close()
		Expect(patched.StatusCode).To(Equal(http.StatusForbidden))

		deleted := del("/api/v1/media-servers/e2e-nope", viewerAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusForbidden))

		tested := post("/api/v1/media-servers/e2e-nope/test", viewerAuth, nil)
		defer tested.Body.Close()
		Expect(tested.StatusCode).To(Equal(http.StatusForbidden))

		draft := post("/api/v1/media-servers/test", viewerAuth, map[string]any{
			"server_type": "jellyfin",
			"host":        "http://127.0.0.1:1",
			"api_key":     "k",
		})
		defer draft.Body.Close()
		Expect(draft.StatusCode).To(Equal(http.StatusForbidden))

		discover := post(
			"/api/v1/media-servers/discover",
			viewerAuth,
			map[string]any{
				"server_type": "jellyfin",
				"host":        "http://127.0.0.1:1",
				"api_key":     "k",
			},
		)
		defer discover.Body.Close()
		Expect(discover.StatusCode).To(Equal(http.StatusForbidden))
	})
})
