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

		DescribeTable(
			"keys the category root and search type off the media kind",
			// Off the kind, never off whichever id happens to be set. The
			// id-less rows are the retry in searchAll, which used to send no
			// categories at all and keyword-searched every indexer's whole
			// catalogue.
			func(params SearchParams, wantCat, wantType string) {
				var gotCat, gotType string
				srv := httptest.NewServer(http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) {
						gotCat = r.URL.Query().Get("categories")
						gotType = r.URL.Query().Get("type")
						_, _ = w.Write([]byte(`[]`))
					}))
				DeferCleanup(srv.Close)

				_, err := NewProwlarr(srv.URL, "k").
					Search(context.Background(), params)
				Expect(err).NotTo(HaveOccurred())
				Expect(gotCat).To(Equal(wantCat))
				Expect(gotType).To(Equal(wantType))
			},
			Entry("a series search",
				SearchParams{Query: "x", Kind: KindTV, TVDBID: 1, Season: 2},
				"5000", "tvsearch"),
			Entry("a movie search",
				SearchParams{Query: "x", Kind: KindMovie, TMDBID: 1},
				"2000", "movie"),
			Entry("a series retry, which carries no id",
				SearchParams{Query: "x", Kind: KindTV},
				"5000", "tvsearch"),
			Entry("a movie retry, which carries no id",
				SearchParams{Query: "x", Kind: KindMovie},
				"2000", "movie"),
			Entry("an unscoped search",
				SearchParams{Query: "x"},
				"", "search"),
		)

		DescribeTable(
			"writes season and episode as query tokens",
			// GET /api/v1/search has no season or episode param — the query
			// string is the only channel, and Prowlarr strips each token back
			// out before the trackers see the term.
			func(params SearchParams, want string) {
				var gotQuery string
				srv := httptest.NewServer(http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) {
						gotQuery = r.URL.Query().Get("query")
						_, _ = w.Write([]byte(`[]`))
					}))
				DeferCleanup(srv.Close)

				_, err := NewProwlarr(srv.URL, "k").
					Search(context.Background(), params)
				Expect(err).NotTo(HaveOccurred())
				Expect(gotQuery).To(Equal(want))
			},
			Entry("an episode",
				SearchParams{
					Query: "Breaking Bad", Kind: KindTV,
					TVDBID: 81189, Season: 2, Episode: 3,
				},
				"Breaking Bad{season:2}{episode:3}"),
			Entry("a season pack",
				SearchParams{Query: "Breaking Bad", Kind: KindTV, Season: 2},
				"Breaking Bad{season:2}"),
			Entry("a whole-series search names no season",
				SearchParams{Query: "Breaking Bad", Kind: KindTV, TVDBID: 81189},
				"Breaking Bad"),
			Entry("season 0 is not a named season",
				SearchParams{Query: "Breaking Bad", Kind: KindTV, Episode: 3},
				"Breaking Bad"),
			// The movie token set has no season or episode, and a film has
			// neither to say.
			Entry("a movie carries no tokens",
				SearchParams{Query: "Dune", Kind: KindMovie, TMDBID: 438631},
				"Dune"),
		)

		It("never sends an id token, whatever the search carries", func() {
			// Prowlarr drops an indexer from the fan-out entirely when handed
			// an id its caps don't declare, so an id token costs whole
			// trackers. The ids are read off the results instead.
			var gotQuery string
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					gotQuery = r.URL.Query().Get("query")
					_, _ = w.Write([]byte(`[]`))
				}))
			DeferCleanup(srv.Close)

			_, err := NewProwlarr(srv.URL, "k").Search(
				context.Background(),
				SearchParams{
					Query: "Breaking Bad", Kind: KindTV,
					TVDBID: 81189, TMDBID: 1396, Season: 2, Episode: 3,
				},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(gotQuery).NotTo(ContainSubstring("81189"))
			Expect(gotQuery).NotTo(ContainSubstring("1396"))
		})

		It("maps the provider ids a tracker published on a release", func() {
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(`[
					{"title":"Tagged","downloadUrl":"http://x/a.torrent",
					 "protocol":"torrent","tvdbId":81189,"tmdbId":1396},
					{"title":"Untagged","downloadUrl":"http://x/b.torrent",
					 "protocol":"torrent"}
				]`))
				}))
			DeferCleanup(srv.Close)

			res, err := NewProwlarr(srv.URL, "k").
				Search(context.Background(), SearchParams{Query: "x"})
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(HaveLen(2))
			Expect(res[0].TVDBID).To(Equal(uint32(81189)))
			Expect(res[0].TMDBID).To(Equal(uint32(1396)))
			// Absent means the tracker said nothing, and stays zero.
			Expect(res[1].TVDBID).To(BeZero())
			Expect(res[1].TMDBID).To(BeZero())
		})
	})

	Describe("Feed", func() {
		It("reports that it has no feed rather than an empty one", func() {
			// The distinction is what lets the scanners skip Prowlarr silently
			// instead of logging a 0-item fetch every tick, forever.
			res, err := NewProwlarr("http://unused", "k").Feed(context.Background())
			Expect(err).To(MatchError(ErrFeedUnsupported))
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
