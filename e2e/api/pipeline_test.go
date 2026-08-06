package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/apptest"
	"github.com/datahearth/streamline/e2e/containers"
	"github.com/datahearth/streamline/e2e/fakes"
)

const (
	// hopBudget bounds one asynchronous hop of the pipeline: a queue refresh,
	// a recheck, a monitor pass. The slowest of them is qBittorrent hashing
	// the 64 MiB payload on recheck — a second or so — so a hop still
	// unfinished at this point is stuck rather than merely slow.
	hopBudget = 30 * time.Second
	hopPoll   = 500 * time.Millisecond

	// managedCategory is the category streamline files its own torrents
	// under; listing it keeps anything a neighbouring spec added out of view.
	managedCategory = "streamline"
)

type mediaRef struct {
	Id uint32 `json:"id"`
}

type queueEntryView struct {
	Status         string   `json:"status"`
	Title          string   `json:"title"`
	Indexer        string   `json:"indexer"`
	DownloadClient string   `json:"download_client"`
	Size           int64    `json:"size"`
	Movie          mediaRef `json:"movie"`
}

type historyEntryView struct {
	Status     string     `json:"status"`
	Title      string     `json:"title"`
	Movie      mediaRef   `json:"movie"`
	ImportedAt *time.Time `json:"imported_at"`
}

type mediaFileView struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Quality string `json:"quality"`
	Format  string `json:"format"`
}

type movieDetailView struct {
	Status     string          `json:"status"`
	MediaFiles []mediaFileView `json:"media_files"`
}

var _ = Describe(
	"Grab pipeline against qBittorrent",
	Label("e2e", "containers"),
	Ordered,
	func() {
		var (
			qb      *containers.QBittorrent
			tz      *fakes.Torznab
			movieID uint32
		)

		BeforeAll(func() {
			containers.Require()
			qb = containers.StartQBittorrent(downloadDir)
			// Before the indexer, client and movie exist: starting the loop
			// runs every job once, and a movie-rss-sync over a live indexer would
			// grab the release out from under the spec.
			stopScheduler := apptest.StartScheduler(app.Scheduler)

			tz = createLiveIndexer("e2e-pipeline-indexer")
			createLiveDownloadClient("e2e-pipeline-client", qb)
			movieID = createLibraryMovie()

			// The container outlives this suite (ryuk reaps it at process
			// exit), so a torrent left behind would meet the next run as an
			// untracked one to adopt. Its files stay — they live in the
			// suite's own temp dirs.
			DeferCleanup(func() {
				for _, t := range qb.Torrents(managedCategory) {
					qb.Remove(t.Hash)
				}
			})
			// Registered last, so LIFO teardown stops the jobs before the
			// entities they poll start disappearing.
			DeferCleanup(stopScheduler)
		})

		It("grabs the fake indexer's release into the download queue", func() {
			resp := post(
				fmt.Sprintf("/api/v1/movies/%d/search", movieID),
				adminAuth,
				nil,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var results []map[string]any
			decode(resp, &results)
			Expect(results).To(HaveLen(1))
			release := results[0]
			Expect(release).To(HaveKeyWithValue("title", fakes.ReleaseTitle))
			Expect(release).To(HaveKeyWithValue(
				"download_url", tz.URL+fakes.DownloadPath,
			))

			By("dispatching the release the search returned, verbatim")
			grabbed := post(
				fmt.Sprintf("/api/v1/movies/%d/grab", movieID),
				adminAuth,
				release,
			)
			defer grabbed.Body.Close()
			Expect(grabbed.StatusCode).To(
				Equal(http.StatusAccepted),
				"grab rejected: %s", bodyText(grabbed),
			)

			// The grab persists its record before answering, but the queue
			// view is served from a short-TTL snapshot that can still be the
			// pre-grab one.
			Eventually(downloadQueue).
				WithTimeout(hopBudget).
				WithPolling(hopPoll).
				Should(ContainElement(SatisfyAll(
					HaveField("Status", "downloading"),
					HaveField("Title", fakes.ReleaseTitle),
					HaveField("Movie.Id", movieID),
					HaveField("Indexer", "e2e-pipeline-indexer"),
					HaveField("DownloadClient", "e2e-pipeline-client"),
					HaveField("Size", int64(fakes.ReleaseSize)),
				)), func() string {
					return "the grabbed release never reached the queue\n" +
						pipelineState(qb)
				})
		})

		It("imports the release once qBittorrent reports it complete", func() {
			torrents := qb.Torrents(managedCategory)
			Expect(torrents).To(HaveLen(1), "the grab never reached the client")
			torrent := torrents[0]
			Expect(torrent.Name).To(Equal(fakes.TorrentName))
			// Single-file torrent, no category save paths: the payload lands
			// directly in the client's save path.
			Expect(torrent.ContentPath).To(
				Equal(filepath.Join(downloadDir, fakes.TorrentName)),
			)

			By("handing qBittorrent the bytes a real swarm would have")
			completeTorrent(qb, torrent, tz.Content())

			Eventually(qb.Torrents).
				WithArguments(managedCategory).
				WithTimeout(hopBudget).
				WithPolling(hopPoll).
				Should(ConsistOf(SatisfyAll(
					HaveField("Progress", BeNumerically("==", 1)),
					// The whole *UP family means "done downloading", which is
					// what the download monitor imports on.
					HaveField("State", HaveSuffix("UP")),
				)))

			By("importing the completed download")
			var detail movieDetailView
			Eventually(func() []mediaFileView {
				runDownloadMonitor()
				detail = movieDetail(movieID)
				return detail.MediaFiles
			}).
				WithTimeout(hopBudget).
				WithPolling(hopPoll).
				Should(HaveLen(1), func() string {
					return fmt.Sprintf(
						"the completed download never imported\nmovie: %+v\n%s",
						detail, pipelineState(qb),
					)
				})

			Expect(detail.Status).To(Equal("available"))
			file := detail.MediaFiles[0]
			Expect(file.Path).To(Equal(filepath.Join(
				moviesDir,
				fmt.Sprintf(
					"%s (%d) {tmdb-%d}",
					fakes.MovieTitle, fakes.MovieYear, fakes.MovieTMDBID,
				),
				fmt.Sprintf(
					"%s (%d) [1080p].mkv",
					fakes.MovieTitle,
					fakes.MovieYear,
				),
			)))
			Expect(file.Quality).To(Equal("1080p"))
			Expect(file.Format).To(Equal("mkv"))

			By("placing the bytes in the library, not just a row in the DB")
			info, err := os.Stat(file.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Size()).To(BeEquivalentTo(fakes.ReleaseSize))
			Expect(file.Size).To(Equal(info.Size()))

			By("retiring the queue entry into history")
			// Eventually, not a bare read: the queue is served from a
			// short-TTL snapshot that can still hold the importing entry.
			Eventually(downloadQueue).
				WithTimeout(hopBudget).
				WithPolling(hopPoll).
				ShouldNot(
					ContainElement(HaveField("Movie.Id", movieID)),
					func() string {
						return "the imported download never left the queue\n" +
							pipelineState(qb)
					},
				)
			Expect(downloadHistory()).To(ContainElement(SatisfyAll(
				HaveField("Status", "completed"),
				HaveField("Title", fakes.ReleaseTitle),
				HaveField("Movie.Id", movieID),
				HaveField("ImportedAt", Not(BeNil())),
			)))
		})
	},
)

// completeTorrent hands qBittorrent the payload a real swarm would have
// delivered. The torrent is stopped first so the write cannot race the
// session's own handle on the file; the recheck that follows hashes what is
// now on disk, which is what carries the torrent to 100%.
func completeTorrent(
	qb *containers.QBittorrent,
	torrent containers.Torrent,
	content []byte,
) {
	GinkgoHelper()
	qb.Stop(torrent.Hash)
	Expect(os.WriteFile(torrent.ContentPath, content, 0o644)).To(Succeed())
	qb.Recheck(torrent.Hash)
}

// pipelineState renders every moving part of the pipeline for a timed-out
// assertion, so a red run names the hop that stalled instead of only the shape
// that never arrived. Passed as a lazily-evaluated description: an eager
// argument would capture the state before the first poll.
func pipelineState(qb *containers.QBittorrent) string {
	GinkgoHelper()
	return fmt.Sprintf(
		"queue: %+v\nhistory: %+v\ntorrents: %+v",
		downloadQueue(), downloadHistory(), qb.Torrents(managedCategory),
	)
}

// runDownloadMonitor triggers one download-monitor pass. The job runs
// asynchronously, so callers poll for its effect rather than reading straight
// after; a conflict means a previous trigger is still in flight, which serves
// the same purpose.
func runDownloadMonitor() {
	GinkgoHelper()
	resp := post("/api/v1/schedules/download-monitor/run", adminAuth, nil)
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(
		BeElementOf(http.StatusOK, http.StatusConflict),
		"run rejected: %s", bodyText(resp),
	)
}

func downloadQueue() []queueEntryView {
	GinkgoHelper()
	resp := get("/api/v1/activity/queue", adminAuth)
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusOK))
	var queue struct {
		Items []queueEntryView `json:"items"`
	}
	decode(resp, &queue)
	return queue.Items
}

func downloadHistory() []historyEntryView {
	GinkgoHelper()
	resp := get("/api/v1/activity/history?limit=25", adminAuth)
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusOK))
	var history struct {
		Items []historyEntryView `json:"items"`
	}
	decode(resp, &history)
	return history.Items
}

func movieDetail(id uint32) movieDetailView {
	GinkgoHelper()
	resp := get(fmt.Sprintf("/api/v1/movies/%d", id), adminAuth)
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusOK))
	var detail movieDetailView
	decode(resp, &detail)
	return detail
}
