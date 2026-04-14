package indexer

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Test helpers for building Torznab XML responses

type testRSS struct {
	XMLName xml.Name       `xml:"rss"`
	Version string         `xml:"version,attr"`
	Channel testRSSChannel `xml:"channel"`
}

type testRSSChannel struct {
	Title string        `xml:"title"`
	Items []testRSSItem `xml:"item"`
}

type testRSSItem struct {
	Title     string        `xml:"title"`
	GUID      string        `xml:"guid"`
	Link      string        `xml:"link"`
	Size      int64         `xml:"size"`
	Enclosure testEnclosure `xml:"enclosure"`
	// InnerXML for torznab:attr elements since encoding/xml can't marshal namespace prefixes
	ExtraXML string `xml:",innerxml"`
}

type testEnclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

func torznabAttr(name, value string) string {
	return fmt.Sprintf(`<torznab:attr name="%s" value="%s"/>`, name, value)
}

func torznabAttrs(attrs map[string]string) string {
	size := 0
	for name, value := range attrs {
		// <torznab:attr name="X" value="Y"/> = 28 static chars
		size += 28 + len(name) + len(value)
	}
	b := make([]byte, 0, size)
	for name, value := range attrs {
		b = append(b, torznabAttr(name, value)...)
	}
	return string(b)
}

func torznabXML(items []testRSSItem) []byte {
	GinkgoHelper()
	rss := testRSS{
		Version: "2.0",
		Channel: testRSSChannel{
			Title: "Test Indexer",
			Items: items,
		},
	}
	b, err := xml.Marshal(rss)
	Expect(err).NotTo(HaveOccurred())
	// Inject the torznab namespace into the root element
	result := []byte(xml.Header)
	// Replace <rss with <rss xmlns:torznab="..."
	tagged := append(
		[]byte(
			`<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">`,
		),
		b[len(`<rss version="2.0">`):]...,
	)
	return append(result, tagged...)
}

// newTorznabServer wires a one-shot http handler into an httptest.Server,
// returns a Client pointed at it, and registers cleanup. Specs supply the
// handler that produces the canned response (or hangs up).
func newTorznabServer(handler http.HandlerFunc) Client {
	GinkgoHelper()
	ts := httptest.NewServer(handler)
	DeferCleanup(func() { ts.Close() })
	return NewTorznab(ts.URL, "test-key")
}

var _ = Describe("Torznab Client", Label("unit", "indexers"), func() {
	Describe("Search", func() {
		When("the server returns a valid feed", func() {
			It("parses each item and decodes torznab attributes", func() {
				client := newTorznabServer(
					func(w http.ResponseWriter, r *http.Request) {
						Expect(r.URL.Query().Get("apikey")).To(Equal("test-key"))
						w.Header().Set("Content-Type", "application/xml")
						_, err := w.Write(torznabXML([]testRSSItem{
							{
								Title: "Interstellar 2014 1080p BluRay x264-SPARKS",
								GUID:  "https://example.com/details/123",
								Link:  "https://example.com/download/123",
								Size:  5368709120,
								Enclosure: testEnclosure{
									URL:    "https://example.com/download/123",
									Length: 5368709120,
									Type:   "application/x-bittorrent",
								},
								ExtraXML: torznabAttrs(map[string]string{
									"seeders":  "150",
									"peers":    "30",
									"category": "2040",
								}),
							},
						}))
						Expect(err).NotTo(HaveOccurred())
					},
				)

				results, err := client.Search(context.Background(), SearchParams{
					Query:  "Interstellar",
					IMDBID: "tt0816692",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(results).To(HaveLen(1))
				Expect(
					results[0].Title,
				).To(Equal("Interstellar 2014 1080p BluRay x264-SPARKS"))
				Expect(results[0].Seeders).To(Equal(uint32(150)))
				Expect(results[0].Leechers).To(Equal(uint32(30)))
				Expect(results[0].Category).To(Equal("2040"))
				Expect(results[0].Size).To(Equal(int64(5368709120)))
				Expect(
					results[0].Download,
				).To(Equal("https://example.com/download/123"))
			})
		})

		When("the feed has no items", func() {
			It("returns an empty slice without error", func() {
				client := newTorznabServer(
					func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/xml")
						_, err := w.Write(torznabXML(nil))
						Expect(err).NotTo(HaveOccurred())
					},
				)

				results, err := client.Search(
					context.Background(),
					SearchParams{Query: "x"},
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(results).To(BeEmpty())
			})
		})

		When("torznab attributes are absent on an item", func() {
			It("leaves Seeders/Leechers/Category at their zero values", func() {
				client := newTorznabServer(
					func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/xml")
						_, err := w.Write(torznabXML([]testRSSItem{
							{
								Title: "Bare Item",
								GUID:  "g",
								Link:  "l",
								Enclosure: testEnclosure{
									URL:    "u",
									Length: 1,
									Type:   "t",
								},
							},
						}))
						Expect(err).NotTo(HaveOccurred())
					},
				)

				results, err := client.Search(
					context.Background(),
					SearchParams{Query: "x"},
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(results).To(HaveLen(1))
				Expect(results[0].Seeders).To(Equal(uint32(0)))
				Expect(results[0].Leechers).To(Equal(uint32(0)))
				Expect(results[0].Category).To(BeEmpty())
			})
		})

		When("the body is not valid XML", func() {
			It("returns ErrBadResponse", func() {
				client := newTorznabServer(
					func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/xml")
						_, err := w.Write([]byte("<<not xml"))
						Expect(err).NotTo(HaveOccurred())
					},
				)

				_, err := client.Search(
					context.Background(),
					SearchParams{Query: "x"},
				)
				Expect(err).To(MatchError(ErrBadResponse))
			})
		})

		When("the server rejects the credentials", func() {
			It("returns ErrUnauthorized for 401 and 403", func() {
				for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
					client := newTorznabServer(
						func(w http.ResponseWriter, _ *http.Request) {
							w.WriteHeader(status)
						},
					)
					_, err := client.Search(
						context.Background(),
						SearchParams{Query: "x"},
					)
					Expect(err).To(MatchError(ErrUnauthorized))
				}
			})
		})

		When("the server returns an unexpected status", func() {
			It("returns ErrUnexpectedStatus", func() {
				client := newTorznabServer(
					func(w http.ResponseWriter, _ *http.Request) {
						w.WriteHeader(http.StatusInternalServerError)
					},
				)

				_, err := client.Search(
					context.Background(),
					SearchParams{Query: "x"},
				)
				Expect(err).To(MatchError(ErrUnexpectedStatus))
			})
		})

		When("the server hangs up before responding", func() {
			It("returns ErrUnreachable", func() {
				ts := httptest.NewServer(
					http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
				)
				client := NewTorznab(ts.URL, "test-key")
				ts.Close()

				_, err := client.Search(
					context.Background(),
					SearchParams{Query: "x"},
				)
				Expect(err).To(MatchError(ErrUnreachable))
			})
		})
	})

	Describe("Feed", func() {
		It("queries t=search with no q and parses items", func() {
			client := newTorznabServer(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Query().Get("t")).To(Equal("search"))
				Expect(r.URL.Query().Get("q")).To(BeEmpty())
				Expect(r.URL.Query().Get("apikey")).To(Equal("test-key"))
				w.Header().Set("Content-Type", "application/xml")
				_, err := w.Write(torznabXML([]testRSSItem{
					{
						Title: "Test Movie 2024 1080p WEB-DL",
						GUID:  "g",
						Enclosure: testEnclosure{
							URL:    "magnet:?xt=urn:btih:abc",
							Length: 1000,
						},
						ExtraXML: torznabAttr("seeders", "42"),
					},
				}))
				Expect(err).NotTo(HaveOccurred())
			})

			results, err := client.Feed(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Title).To(Equal("Test Movie 2024 1080p WEB-DL"))
			Expect(results[0].Seeders).To(Equal(uint32(42)))
		})

		It("returns an empty slice on empty feed", func() {
			client := newTorznabServer(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				_, err := w.Write(torznabXML(nil))
				Expect(err).NotTo(HaveOccurred())
			})
			results, err := client.Feed(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})

		It("returns ErrUnexpectedStatus on HTTP failure", func() {
			client := newTorznabServer(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			})
			_, err := client.Feed(context.Background())
			Expect(err).To(MatchError(ErrUnexpectedStatus))
		})
	})

	Describe("TestConnection", func() {
		It("succeeds when the server returns a caps document", func() {
			client := newTorznabServer(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Query().Get("t")).To(Equal("caps"))
				w.Header().Set("Content-Type", "application/xml")
				_, err := w.Write([]byte(xml.Header + `<caps>
					<server title="Prowlarr"/>
					<limits max="100" default="100"/>
					<searching>
						<search available="yes" supportedParams="q"/>
						<movie-search available="yes" supportedParams="q,imdbid,tmdbid"/>
					</searching>
					<categories>
						<category id="2000" name="Movies"/>
					</categories>
				</caps>`))
				Expect(err).NotTo(HaveOccurred())
			})

			Expect(client.TestConnection(context.Background())).To(Succeed())
		})

		It("returns ErrBadResponse when the body is not a caps document", func() {
			client := newTorznabServer(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				_, err := w.Write(torznabXML(nil))
				Expect(err).NotTo(HaveOccurred())
			})

			Expect(client.TestConnection(context.Background())).
				To(MatchError(ErrBadResponse))
		})
	})

	Describe("parsePubDate", func() {
		It("parses RFC1123Z with a zone offset, normalising to UTC", func() {
			t := parsePubDate("Mon, 02 Jan 2006 15:04:05 +0200")
			Expect(t.IsZero()).To(BeFalse())
			Expect(t.Location()).To(Equal(time.UTC))
			Expect(
				t,
			).To(Equal(time.Date(2006, time.January, 2, 13, 4, 5, 0, time.UTC)))
		})

		It("parses RFC1123 with a named zone", func() {
			t := parsePubDate("Mon, 02 Jan 2006 15:04:05 GMT")
			Expect(
				t,
			).To(Equal(time.Date(2006, time.January, 2, 15, 4, 5, 0, time.UTC)))
		})

		It("yields the zero time for empty or unparseable input", func() {
			Expect(parsePubDate("").IsZero()).To(BeTrue())
			Expect(parsePubDate("not a date").IsZero()).To(BeTrue())
		})
	})
})
