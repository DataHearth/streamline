package indexer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func splitHostPort(rawURL string) (string, uint16) {
	GinkgoHelper()
	u, err := url.Parse(rawURL)
	Expect(err).NotTo(HaveOccurred())
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	Expect(err).NotTo(HaveOccurred())
	return u.Hostname(), uint16(port)
}

var _ = Describe("Service", Label("integration", "indexers"), func() {
	Describe("SearchMovie", func() {
		It(
			"searches all enabled indexers in parallel and merges results sorted by seeders",
			func() {
				ctx := context.Background()

				ts1 := httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/xml")
						w.WriteHeader(http.StatusOK)
						_, err := w.Write(torznabXML([]testRSSItem{
							{
								Title: "Interstellar 1080p BluRay-GROUP1",
								GUID:  "https://idx1.com/1",
								Link:  "https://idx1.com/dl/1",
								Size:  5000000000,
								Enclosure: testEnclosure{
									URL:    "https://idx1.com/dl/1",
									Length: 5000000000,
									Type:   "application/x-bittorrent",
								},
								ExtraXML: torznabAttrs(
									map[string]string{
										"seeders": "50",
										"peers":   "10",
									},
								),
							},
						}))
						Expect(err).NotTo(HaveOccurred())
					}),
				)
				defer ts1.Close()

				ts2 := httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/xml")
						w.WriteHeader(http.StatusOK)
						_, err := w.Write(torznabXML([]testRSSItem{
							{
								Title: "Interstellar 2160p BluRay-GROUP2",
								GUID:  "https://idx2.com/1",
								Link:  "https://idx2.com/dl/1",
								Size:  10000000000,
								Enclosure: testEnclosure{
									URL:    "https://idx2.com/dl/1",
									Length: 10000000000,
									Type:   "application/x-bittorrent",
								},
								ExtraXML: torznabAttrs(
									map[string]string{
										"seeders": "200",
										"peers":   "20",
									},
								),
							},
						}))
						Expect(err).NotTo(HaveOccurred())
					}),
				)
				defer ts2.Close()

				host1, port1 := splitHostPort(ts1.URL)
				host2, port2 := splitHostPort(ts2.URL)
				configtest.Setup(map[string]any{
					"indexers": []map[string]any{
						{
							"name":     "Indexer1",
							"host":     host1,
							"port":     int(port1),
							"api_key":  "key1",
							"protocol": "torznab",
							"enabled":  true,
						},
						{
							"name":     "Indexer2",
							"host":     host2,
							"port":     int(port2),
							"api_key":  "key2",
							"protocol": "torznab",
							"enabled":  true,
						},
						// disabled — should be skipped
						{
							"name":     "Disabled",
							"host":     "nope",
							"port":     80,
							"api_key":  "key3",
							"protocol": "torznab",
							"enabled":  false,
						},
					},
				})

				svc := New()
				results, err := svc.SearchMovie(
					ctx,
					[]string{"Interstellar"},
					157336,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(results).To(HaveLen(2))
				// Sorted by seeders desc: 200, then 50
				Expect(results[0].Seeders).To(Equal(uint32(200)))
				Expect(results[1].Seeders).To(Equal(uint32(50)))
			},
		)
	})
})
