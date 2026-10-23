package indexers

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/containers"
	"github.com/datahearth/streamline/e2e/fakes"
	"github.com/datahearth/streamline/internal/indexer"
)

// containerHost is where a container reaches the spec process. testcontainers
// publishes the forwarded host port under this name.
const containerHost = "host.testcontainers.internal"

const (
	showTVDBID  = 81189
	otherTVDBID = 99999
	movieTMDBID = 438631
)

// One recorder and one Prowlarr serve the whole package: the container's
// route back to this process is a host port fixed when it starts, so a second
// recorder on a second port would be unreachable from inside it.
var (
	prowlarr *containers.Prowlarr
	// tvFull declares season, ep and tvdbid. tvNoIDs declares only q and
	// season/ep — the shape most private trackers have, and the one that
	// proves an id token would cost a whole indexer.
	tvFull, tvNoIDs, moviesOnly *fakes.TorznabEndpoint
)

// setup starts the recorder, then Prowlarr forwarding its port, then
// registers one indexer per endpoint. Ordered: the port must exist before the
// container starts, and Prowlarr probes each feed's caps as it saves it.
//
// Called from BeforeSuite, never BeforeEach: the recorder registers its
// shutdown with DeferCleanup, which runs at the end of whatever phase called
// it. Under a per-spec hook the fake was closed after the first spec while the
// endpoints kept naming its port, and Prowlarr — which cannot tell a dead
// tracker from a disappeared one — disabled every indexer for the backoff
// window, so nothing after the first spec reached the fake at all.
func setup() {
	GinkgoHelper()
	containers.Require()

	{
		rec := fakes.NewTorznabRecorder()

		tvFull = rec.Mount("/tv-full",
			fakes.TorznabCaps{
				TVSearchParams: []string{"q", "season", "ep", "tvdbid"},
				Categories:     []int{5000, 5040},
			},
			fakes.TorznabRelease{
				Title: "Show.S02E03.1080p.WEB-OURS", Size: 2 << 30,
				Seeders: 10, Category: 5040, TVDBID: showTVDBID,
			},
			fakes.TorznabRelease{
				Title: "Show.S02E03.2160p.WEB-OTHER", Size: 8 << 30,
				Seeders: 900, Category: 5040, TVDBID: otherTVDBID,
			},
		)
		tvNoIDs = rec.Mount("/tv-no-ids",
			fakes.TorznabCaps{
				TVSearchParams: []string{"q", "season", "ep"},
				Categories:     []int{5000, 5040},
			},
			fakes.TorznabRelease{
				Title: "Show.S02E03.720p.WEB-NOIDS", Size: 1 << 30,
				Seeders: 4, Category: 5040,
			},
		)
		moviesOnly = rec.Mount("/movies",
			fakes.TorznabCaps{
				MovieSearchParams: []string{"q"},
				Categories:        []int{2000, 2040},
			},
			fakes.TorznabRelease{
				Title: "Dune.2021.1080p.BluRay-FAKE", Size: 8 << 30,
				Seeders: 50, Category: 2040, TMDBID: movieTMDBID,
			},
		)

		prowlarr = containers.StartProwlarr(rec.Port())
		for _, ep := range []*fakes.TorznabEndpoint{tvFull, tvNoIDs, moviesOnly} {
			prowlarr.AddTorznabIndexer(containers.TorznabIndexerCaps{
				Name:       ep.Path,
				FeedURL:    ep.FeedURL(containerHost),
				APIKey:     fakes.APIKey,
				Categories: []int{5000, 2000},
			})
		}
	}
}

// client is the real streamline Prowlarr client, pointed at the container.
func client() indexer.Client {
	return indexer.NewProwlarr(prowlarr.BaseURL(), prowlarr.APIKey)
}

var _ = Describe(
	"the Prowlarr client against a real Prowlarr",
	Label("e2e", "containers"),
	func() {
		It("delivers the season and episode to the tracker", func() {
			// The whole point of the container: prowlarrQuery writes
			// {season:2}{episode:3} into the query string, and only a real
			// Prowlarr can say whether QueryToParams turns that back into the
			// season and ep a tracker is actually asked for.
			_, err := client().Search(context.Background(), indexer.SearchParams{
				Query: "Show", Kind: indexer.KindTV,
				Season: 2, Episode: 3,
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(tvFull.SearchQueries).Should(ContainElement(
				SatisfyAll(
					HaveKeyWithValue("t", []string{"tvsearch"}),
					HaveKeyWithValue("season", []string{"2"}),
					HaveKeyWithValue("ep", []string{"3"}),
				),
			), "the tokens must reach the tracker as real params")
		})

		It("still queries a tracker whose caps omit the ids", func() {
			// Season and ep are absent from Prowlarr's capability guard, so
			// narrowing with them can never exclude an indexer. This is what
			// makes sending those tokens safe, and it is the assertion that
			// fails first if that stops being true.
			before := len(tvNoIDs.SearchQueries())

			results, err := client().Search(
				context.Background(),
				indexer.SearchParams{
					Query: "Show", Kind: indexer.KindTV,
					Season: 2, Episode: 3,
				},
			)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() int { return len(tvNoIDs.SearchQueries()) }).
				Should(BeNumerically(">", before))
			Expect(titles(results)).To(ContainElement(ContainSubstring("NOIDS")))
		})

		It("scopes the fan-out to the category's own indexers", func() {
			// A movies-only indexer must not see a TV search. This is what
			// the category root buys, and what the id-less retry lost by
			// deriving it from whichever id happened to be set.
			before := len(moviesOnly.SearchQueries())

			_, err := client().Search(context.Background(), indexer.SearchParams{
				Query: "Show", Kind: indexer.KindTV, Season: 2, Episode: 3,
			})
			Expect(err).NotTo(HaveOccurred())

			Consistently(func() int { return len(moviesOnly.SearchQueries()) }).
				Should(Equal(before))
		})

		It("carries a release's provider id back to the caller", func() {
			// Prowlarr parses the tracker's torznab attrs and re-emits them on
			// ReleaseResource; this is the round trip filterProviderIDs rests
			// on, and the release deliberately labelled with another show's id
			// is the one it has to be able to tell apart.
			results, err := client().Search(
				context.Background(),
				indexer.SearchParams{
					Query: "Show", Kind: indexer.KindTV,
					Season: 2, Episode: 3,
				},
			)
			Expect(err).NotTo(HaveOccurred())

			byTitle := map[string]indexer.SearchResult{}
			for _, r := range results {
				byTitle[r.Title] = r
			}
			Expect(byTitle).To(HaveKey("Show.S02E03.1080p.WEB-OURS"))
			Expect(byTitle["Show.S02E03.1080p.WEB-OURS"].TVDBID).
				To(Equal(uint32(showTVDBID)))
			Expect(byTitle["Show.S02E03.2160p.WEB-OTHER"].TVDBID).
				To(Equal(uint32(otherTVDBID)))
			// Unlabelled stays zero, which is what "the tracker said nothing"
			// has to look like downstream.
			Expect(byTitle["Show.S02E03.720p.WEB-NOIDS"].TVDBID).To(BeZero())
		})

		It("drops an indexer handed an id its caps omit", func() {
			// The reason the client sends no id token. Prowlarr answers an
			// unsupported id with an empty result for that indexer rather
			// than falling back to a keyword search, so the tracker is never
			// asked at all — asserted here by driving the token in by hand,
			// since the client deliberately never writes one.
			before := len(tvNoIDs.SearchQueries())

			_, err := client().Search(context.Background(), indexer.SearchParams{
				// Kind is TV so the type is tvsearch and the token parses;
				// the id rides in the query the way Prowlarr's own UI writes
				// it.
				Query: "Show{tvdbid:81189}", Kind: indexer.KindTV,
			})
			Expect(err).NotTo(HaveOccurred())

			Consistently(func() int { return len(tvNoIDs.SearchQueries()) }).
				Should(Equal(before), "an id-blind tracker must be skipped, proving the cost")

			// The tracker that does declare tvdbid is still asked, and gets
			// the id as a real param.
			Eventually(tvFull.SearchQueries).Should(ContainElement(
				HaveKeyWithValue("tvdbid", []string{"81189"}),
			))
		})
	},
)

func titles(results []indexer.SearchResult) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Title
	}
	return out
}
