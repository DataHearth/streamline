package api

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type indexerView struct {
	Name      string `json:"name"`
	Host      string `json:"host"`
	Port      uint16 `json:"port"`
	Protocol  string `json:"protocol"`
	Priority  uint8  `json:"priority"`
	Enabled   bool   `json:"enabled"`
	ApiKeySet bool   `json:"api_key_set"`
}

// createIndexer adds a disabled indexer pointed at a closed local port and
// schedules its removal. Connection tests against it refuse instantly. Cleanup
// is registered before the first assertion so a later failure cannot leak the
// entity; 404 covers specs that delete it themselves.
func createIndexer(name string) indexerView {
	GinkgoHelper()
	resp := post("/api/v1/indexers", adminAuth, map[string]any{
		"name":     name,
		"host":     "127.0.0.1",
		"port":     1,
		"api_key":  "e2e-indexer-key",
		"protocol": "torznab",
		"priority": 3,
		"enabled":  false,
	})
	defer resp.Body.Close()
	DeferCleanup(func() {
		cleanup := del("/api/v1/indexers/"+name, adminAuth, nil)
		defer cleanup.Body.Close()
		Expect(cleanup.StatusCode).To(BeElementOf(
			http.StatusNoContent, http.StatusNotFound,
		))
	})
	Expect(resp.StatusCode).To(Equal(http.StatusCreated))
	var indexer indexerView
	decode(resp, &indexer)
	return indexer
}

var _ = Describe("REST API indexers", Label("e2e"), func() {
	It("lists configured indexers", func() {
		resp := get("/api/v1/indexers", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var indexers []indexerView
		decode(resp, &indexers)
		Expect(indexers).NotTo(BeNil())
	})

	It("creates, updates and deletes an indexer", func() {
		indexer := createIndexer("e2e-indexer")
		Expect(indexer.Name).To(Equal("e2e-indexer"))
		Expect(indexer.Host).To(Equal("127.0.0.1"))
		Expect(indexer.Port).To(BeEquivalentTo(1))
		Expect(indexer.Protocol).To(Equal("torznab"))
		Expect(indexer.Enabled).To(BeFalse())
		Expect(indexer.ApiKeySet).To(BeTrue())

		updated := put("/api/v1/indexers/e2e-indexer", adminAuth, map[string]any{
			"name":     "e2e-indexer",
			"host":     "127.0.0.2",
			"port":     2,
			"api_key":  "",
			"protocol": "torznab",
			"priority": 7,
			"enabled":  false,
		})
		defer updated.Body.Close()
		Expect(updated.StatusCode).To(Equal(http.StatusOK))
		decode(updated, &indexer)
		Expect(indexer.Host).To(Equal("127.0.0.2"))
		Expect(indexer.Port).To(BeEquivalentTo(2))
		Expect(indexer.Priority).To(BeEquivalentTo(7))
		// A blank api_key preserves the stored secret.
		Expect(indexer.ApiKeySet).To(BeTrue())

		deleted := del("/api/v1/indexers/e2e-indexer", adminAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusNoContent))
	})

	It("409s a duplicate indexer name", func() {
		createIndexer("e2e-indexer-dup")
		resp := post("/api/v1/indexers", adminAuth, map[string]any{
			"name":     "e2e-indexer-dup",
			"host":     "127.0.0.1",
			"port":     1,
			"api_key":  "k",
			"protocol": "torznab",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusConflict))
	})

	It("422s an indexer that fails config validation", func() {
		resp := post("/api/v1/indexers", adminAuth, map[string]any{
			"name":     "e2e-indexer-bad",
			"host":     "127.0.0.1",
			"port":     0,
			"api_key":  "k",
			"protocol": "torznab",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
	})

	It("404s updating and deleting an unknown indexer", func() {
		updated := put("/api/v1/indexers/e2e-missing", adminAuth, map[string]any{
			"name":     "e2e-missing",
			"host":     "127.0.0.1",
			"port":     1,
			"api_key":  "k",
			"protocol": "torznab",
		})
		defer updated.Body.Close()
		Expect(updated.StatusCode).To(Equal(http.StatusNotFound))

		deleted := del("/api/v1/indexers/e2e-missing", adminAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusNotFound))
	})

	Describe("connection tests", func() {
		It("404s testing an unknown indexer", func() {
			resp := post("/api/v1/indexers/e2e-missing/test", adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("422s testing a saved indexer that refuses connections", func() {
			createIndexer("e2e-indexer-test")
			resp := post("/api/v1/indexers/e2e-indexer-test/test", adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("422s testing a draft indexer that refuses connections", func() {
			resp := post("/api/v1/indexers/test", adminAuth, map[string]any{
				"name":     "e2e-draft",
				"host":     "127.0.0.1",
				"port":     1,
				"api_key":  "k",
				"protocol": "torznab",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})
	})

	It("403s mutations and tests for a non-admin", func() {
		created := post("/api/v1/indexers", viewerAuth, map[string]any{
			"name":     "e2e-nope",
			"host":     "127.0.0.1",
			"port":     1,
			"api_key":  "k",
			"protocol": "torznab",
		})
		defer created.Body.Close()
		Expect(created.StatusCode).To(Equal(http.StatusForbidden))

		updated := put("/api/v1/indexers/e2e-nope", viewerAuth, map[string]any{
			"name":     "e2e-nope",
			"host":     "127.0.0.1",
			"port":     1,
			"api_key":  "k",
			"protocol": "torznab",
		})
		defer updated.Body.Close()
		Expect(updated.StatusCode).To(Equal(http.StatusForbidden))

		deleted := del("/api/v1/indexers/e2e-nope", viewerAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusForbidden))

		tested := post("/api/v1/indexers/e2e-nope/test", viewerAuth, nil)
		defer tested.Body.Close()
		Expect(tested.StatusCode).To(Equal(http.StatusForbidden))

		draft := post("/api/v1/indexers/test", viewerAuth, map[string]any{
			"name":     "e2e-nope",
			"host":     "127.0.0.1",
			"port":     1,
			"api_key":  "k",
			"protocol": "torznab",
		})
		defer draft.Body.Close()
		Expect(draft.StatusCode).To(Equal(http.StatusForbidden))
	})
})
