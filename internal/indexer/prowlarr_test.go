package indexer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Prowlarr Client", Label("unit", "indexers"), func() {
	Describe("Search", func() {
		It(
			"sends the api key header + torrents-only filter and maps releases",
			func() {
				var gotKey, gotQuery, gotIndexerIDs string
				srv := httptest.NewServer(http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) {
						Expect(r.URL.Path).To(Equal("/api/v1/search"))
						gotKey = r.Header.Get("X-Api-Key")
						gotQuery = r.URL.Query().Get("query")
						gotIndexerIDs = r.URL.Query().Get("indexerIds")
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write([]byte(`[
						{"title":"Dune 2021 1080p","downloadUrl":"http://x/a.torrent",
						 "infoUrl":"http://x/a","size":8000000000,"seeders":50,
						 "leechers":3,"indexer":"TrackerA","protocol":"torrent",
						 "publishDate":"2021-10-22T00:00:00Z"},
						{"title":"Dune Usenet","downloadUrl":"http://x/b.nzb",
						 "size":900,"indexer":"NewsX","protocol":"usenet"},
						{"title":"Magnet Only","magnetUrl":"magnet:?xt=urn:btih:ff",
						 "size":42,"indexer":"TrackerB","protocol":"torrent"}
					]`))
					}))
				DeferCleanup(srv.Close)

				res, err := NewProwlarr(srv.URL, "secret").
					Search(context.Background(), SearchParams{Query: "Dune", TMDBID: 438631})
				Expect(err).NotTo(HaveOccurred())
				Expect(gotKey).To(Equal("secret"))
				Expect(gotQuery).To(Equal("Dune"))
				Expect(gotIndexerIDs).To(Equal("-2"))

				// Usenet release dropped; torrent + magnet kept.
				Expect(res).To(HaveLen(2))
				Expect(res[0].Title).To(Equal("Dune 2021 1080p"))
				Expect(res[0].Download).To(Equal("http://x/a.torrent"))
				Expect(res[0].Size).To(Equal(int64(8000000000)))
				Expect(res[0].Seeders).To(Equal(uint32(50)))
				Expect(res[0].Indexer).To(Equal("TrackerA"))
				Expect(res[0].PublishDate.IsZero()).To(BeFalse())
				// Falls back to magnetUrl when downloadUrl is absent.
				Expect(res[1].Download).To(Equal("magnet:?xt=urn:btih:ff"))
			},
		)

		It("tags movie vs TV searches with the right category root", func() {
			var gotCat string
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					gotCat = r.URL.Query().Get("categories")
					_, _ = w.Write([]byte(`[]`))
				}))
			DeferCleanup(srv.Close)

			c := NewProwlarr(srv.URL, "k")
			_, err := c.Search(
				context.Background(),
				SearchParams{Query: "x", TVDBID: 1, Season: 2},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(gotCat).To(Equal("5000"))
		})
	})

	Describe("Feed", func() {
		It("returns no items (Prowlarr has no aggregate feed)", func() {
			res, err := NewProwlarr("http://unused", "k").Feed(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(BeEmpty())
		})
	})

	Describe("TestConnection", func() {
		It("hits /api/v1/health with the key and succeeds on 200", func() {
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					Expect(r.URL.Path).To(Equal("/api/v1/health"))
					Expect(r.Header.Get("X-Api-Key")).To(Equal("k"))
					_, _ = w.Write([]byte(`[]`))
				}))
			DeferCleanup(srv.Close)

			Expect(NewProwlarr(srv.URL, "k").
				TestConnection(context.Background())).To(Succeed())
		})

		It("maps 401 to ErrUnauthorized", func() {
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}))
			DeferCleanup(srv.Close)

			err := NewProwlarr(srv.URL, "bad").TestConnection(context.Background())
			Expect(errors.Is(err, ErrUnauthorized)).To(BeTrue())
		})
	})
})
