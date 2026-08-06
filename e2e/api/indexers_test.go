package api

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/fakes"
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

// createIndexer adds a disabled indexer pointed at a closed local port.
// Connection tests against it refuse instantly.
func createIndexer(name string) indexerView {
	GinkgoHelper()
	return addIndexer(name, "127.0.0.1", 1, "e2e-indexer-key", false)
}

// createLiveIndexer starts a torznab fake and adds an enabled indexer pointed
// at it, so connection tests and searches reach a real endpoint. The fake is
// returned for specs that assert against the URLs it serves.
func createLiveIndexer(name string) *fakes.Torznab {
	GinkgoHelper()
	tz := fakes.NewTorznab()
	host, port := hostPort(tz.URL)
	addIndexer(name, host, port, fakes.APIKey, true)
	return tz
}

// addIndexer creates the indexer and schedules its removal. Cleanup is
// registered before the first assertion so a later failure cannot leak the
// entity; 404 covers specs that delete it themselves.
func addIndexer(
	name, host string,
	port uint16,
	apiKey string,
	enabled bool,
) indexerView {
	GinkgoHelper()
	resp := post("/api/v1/indexers", adminAuth, map[string]any{
		"name":     name,
		"host":     host,
		"port":     port,
		"api_key":  apiKey,
		"protocol": "torznab",
		"priority": 3,
		"enabled":  enabled,
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

// hostPort splits a fake's base URL into the host/port pair indexer config
// stores; the scheme is covered by the entry's use_ssl flag.
func hostPort(raw string) (string, uint16) {
	GinkgoHelper()
	parsed, err := url.Parse(raw)
	Expect(err).NotTo(HaveOccurred())
	host, port, err := net.SplitHostPort(parsed.Host)
	Expect(err).NotTo(HaveOccurred())
	num, err := strconv.ParseUint(port, 10, 16)
	Expect(err).NotTo(HaveOccurred())
	return host, uint16(num)
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
		It("tests a reachable torznab indexer", func() {
			createLiveIndexer("e2e-torznab-live")
			resp := post(
				"/api/v1/indexers/e2e-torznab-live/test",
				adminAuth,
				nil,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

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

		It("tests a reachable draft indexer", func() {
			host, port := hostPort(fakes.NewTorznab().URL)
			resp := post("/api/v1/indexers/test", adminAuth, map[string]any{
				"name":     "e2e-draft-live",
				"host":     host,
				"port":     port,
				"api_key":  fakes.APIKey,
				"protocol": "torznab",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
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

	It("returns the fake indexer's release for a movie search", func() {
		tz := createLiveIndexer("e2e-torznab-search")
		movieID := createLibraryMovie()

		resp := post(
			fmt.Sprintf("/api/v1/movies/%d/search", movieID),
			adminAuth,
			nil,
		)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var results []struct {
			Title       string    `json:"title"`
			InfoUrl     string    `json:"info_url"`
			DownloadUrl string    `json:"download_url"`
			Size        int64     `json:"size"`
			Seeders     uint32    `json:"seeders"`
			Leechers    uint32    `json:"leechers"`
			Resolution  string    `json:"resolution"`
			Indexer     string    `json:"indexer"`
			PublishedAt time.Time `json:"published_at"`
		}
		decode(resp, &results)
		Expect(results).To(HaveLen(1))
		Expect(results[0].Title).To(Equal(fakes.ReleaseTitle))
		Expect(results[0].InfoUrl).To(Equal(fakes.ReleaseGUID))
		Expect(results[0].DownloadUrl).To(Equal(tz.URL + fakes.DownloadPath))
		Expect(results[0].Size).To(BeEquivalentTo(fakes.ReleaseSize))
		Expect(results[0].Seeders).To(BeEquivalentTo(fakes.ReleaseSeeders))
		Expect(results[0].Leechers).To(BeEquivalentTo(fakes.ReleasePeers))
		Expect(results[0].Resolution).To(Equal("1080p"))
		Expect(results[0].Indexer).To(Equal("e2e-torznab-search"))

		published, err := time.Parse(time.RFC1123Z, fakes.ReleasePubDate)
		Expect(err).NotTo(HaveOccurred())
		Expect(results[0].PublishedAt).To(BeTemporally("==", published))
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
