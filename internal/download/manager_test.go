package download

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/internal/config"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// downloadSpans captures everything the package tracer emits. It is installed
// at package init rather than per-spec because otel binds an already-created
// tracer to its delegate on the first SetTracerProvider call and never
// re-binds: a provider installed inside a spec would reach a tracer that has
// already resolved to the no-op default.
var downloadSpans = func() *tracetest.SpanRecorder {
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(
		sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)),
	)
	return rec
}()

func endedSpan(rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	GinkgoHelper()

	for _, s := range rec.Ended() {
		if s.Name() == name {
			return s
		}
	}
	Fail("no " + name + " span was recorded")
	return nil
}

func spanEvent(span sdktrace.ReadOnlySpan, name string) sdktrace.Event {
	GinkgoHelper()

	for _, ev := range span.Events() {
		if ev.Name == name {
			return ev
		}
	}
	Fail("span " + span.Name() + " carries no " + name + " event")
	return sdktrace.Event{}
}

func eventAttr(ev sdktrace.Event, key string) string {
	GinkgoHelper()

	for _, kv := range ev.Attributes {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}
	Fail("event " + ev.Name + " carries no " + key + " attribute")
	return ""
}

var _ = Describe("Manager", Label("unit", "downloads"), func() {
	var (
		ctx   context.Context
		store *dbmocks.MockStore
		mgr   Downloader
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		mgr = New(store, nil)
	})

	Describe("Grab", func() {
		When("no enabled download client exists", func() {
			It("returns the no-client error", func() {
				configtest.Setup()
				_, err := mgr.Grab(ctx, indexer.SearchResult{Title: "x"}, 1)
				Expect(
					err,
				).To(MatchError(ContainSubstring("no enabled download client")))
			})
		})
	})

	Describe("downloadSavePath", func() {
		BeforeEach(func() {
			configtest.Setup(map[string]any{
				"library": map[string]any{"download_path": "/downloads"},
			})
		})

		It("joins a plain torrent name under the download path", func() {
			path, err := downloadSavePath("The.Batman.2022.1080p")
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/downloads/The.Batman.2022.1080p"))
		})

		It("rejects a traversing torrent name", func() {
			_, err := downloadSavePath("../../etc/passwd")
			Expect(err).To(MatchError(ErrUnsafeTorrentName))
		})

		It("rejects a name carrying a separator", func() {
			_, err := downloadSavePath("sub/dir")
			Expect(err).To(MatchError(ErrUnsafeTorrentName))
		})

		It("rejects a dot-dot name", func() {
			_, err := downloadSavePath("..")
			Expect(err).To(MatchError(ErrUnsafeTorrentName))
		})
	})

	Describe("PathUnderRoot", func() {
		It("accepts the root itself and its children", func() {
			Expect(PathUnderRoot("/downloads", "/downloads")).To(BeTrue())
			Expect(PathUnderRoot("/downloads/a/b", "/downloads")).To(BeTrue())
		})

		It("rejects a sibling sharing the root's prefix", func() {
			Expect(PathUnderRoot("/downloads-evil/a", "/downloads")).To(BeFalse())
		})
	})

	Describe("resolveTorrentSource", func() {
		// One enabled indexer on the default HTTPS port plus one on an explicit
		// port, so both the implied-port and explicit-port paths are covered.
		BeforeEach(func() {
			configtest.Setup(map[string]any{
				"indexers": []map[string]any{
					{
						"name":     "public",
						"host":     "tracker.example",
						"port":     443,
						"use_ssl":  true,
						"api_key":  "k",
						"protocol": "torznab",
						"enabled":  true,
					},
					{
						"name":     "lan",
						"host":     "192.168.1.5",
						"port":     9696,
						"api_key":  "k",
						"protocol": "prowlarr",
						"enabled":  true,
					},
					{
						"name":     "off",
						"host":     "disabled.example",
						"port":     80,
						"api_key":  "k",
						"protocol": "torznab",
						"enabled":  false,
					},
				},
			})
		})

		It("passes magnet links through without a fetch", func() {
			src, err := resolveTorrentSource(ctx, "magnet:?xt=urn:btih:abc")
			Expect(err).NotTo(HaveOccurred())
			Expect(src.Magnet).To(Equal("magnet:?xt=urn:btih:abc"))
		})

		DescribeTable("rejects URLs outside the configured indexers",
			func(dl string) {
				_, err := resolveTorrentSource(ctx, dl)
				Expect(err).To(MatchError(ErrUntrustedSource))
			},
			Entry("cloud metadata", "http://169.254.169.254/latest/meta-data/"),
			Entry("loopback", "http://127.0.0.1:8080/admin"),
			Entry("an unconfigured LAN host", "http://192.168.1.9:9696/dl"),
			Entry(
				"another port on a configured host",
				"http://192.168.1.5:8080/admin",
			),
			Entry("a disabled indexer", "http://disabled.example/dl"),
			Entry("a non-HTTP scheme", "file:///etc/passwd"),
			Entry("a scheme-relative URL", "//tracker.example/dl"),
		)

		It("accepts a link to a configured indexer", func() {
			// Reaching the transport (and failing there) proves the guard let
			// the URL through — resolution of a non-existent host cannot
			// succeed, but it is no longer ErrUntrustedSource.
			_, err := resolveTorrentSource(ctx, "https://tracker.example/dl?id=1")
			Expect(err).NotTo(MatchError(ErrUntrustedSource))
		})
	})

	// Indexer release links authenticate through the query string — Jackett
	// emits ?jackett_apikey=, Prowlarr ?apikey= (its header auth does not cover
	// download links) — so the *url.Error a failed fetch produces carries the
	// credential in its message. grab hands that error to RecordSpanError,
	// which writes it to the span status and an exception event, both of which
	// are exported to the OTLP backend.
	Describe("grab telemetry", func() {
		const key = "PASSKEYSECRET"

		It("keeps the release link's credentials off the span", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			)
			endpoint, err := url.Parse(ts.URL)
			Expect(err).NotTo(HaveOccurred())
			port, err := strconv.Atoi(endpoint.Port())
			Expect(err).NotTo(HaveOccurred())
			// Closed before the grab so the fetch fails in the transport, which
			// is what produces the *url.Error under test.
			ts.Close()

			configtest.Setup(map[string]any{
				"indexers": []map[string]any{
					{
						"name":     "tracker",
						"host":     endpoint.Hostname(),
						"port":     port,
						"api_key":  key,
						"protocol": "torznab",
						"enabled":  true,
					},
				},
				"download_clients": []map[string]any{
					{
						"name":        "qb",
						"client_type": "qbittorrent",
						"host":        "127.0.0.1",
						"port":        8080,
						"auth_method": "password",
						"username":    "u",
						"password":    "p",
						"enabled":     true,
					},
				},
			})

			download := ts.URL + "/dl?apikey=" + key + "&id=99"
			downloadSpans.Reset()
			_, err = mgr.Grab(ctx, indexer.SearchResult{
				Title:    "Some Movie 2160p",
				Download: download,
			}, 1)
			Expect(err).To(HaveOccurred())

			span := endedSpan(downloadSpans, "download.grab")

			// Both carry the rendered error, so both have to be checked, and
			// asserting the endpoint survives in each proves the check is
			// reading the failure and not an empty string.
			Expect(span.Status().Description).NotTo(ContainSubstring(key))
			Expect(span.Status().Description).To(ContainSubstring(ts.URL + "/dl"))

			message := eventAttr(spanEvent(span, "exception"), "exception.message")
			Expect(message).NotTo(ContainSubstring(key))
			Expect(message).To(ContainSubstring(ts.URL + "/dl"))
		})
	})

	Describe("GrabEpisode", func() {
		When("no enabled download client exists", func() {
			It("returns the no-client error", func() {
				configtest.Setup()
				_, err := mgr.GrabEpisode(ctx, indexer.SearchResult{Title: "x"}, 1)
				Expect(
					err,
				).To(MatchError(ContainSubstring("no enabled download client")))
			})
		})
	})

	Describe("CheckStatus", func() {
		When("the store fails to list downloading records", func() {
			It("returns the wrapped error", func() {
				boom := errors.New("db boom")
				store.EXPECT().
					ListDownloadingRecordsWithMovie(mock.Anything).
					Return(nil, boom).Once()

				_, err := mgr.CheckStatus(ctx)
				Expect(err).To(MatchError(boom))
			})
		})

		When("there are no downloading records", func() {
			It("returns an empty slice without polling any client", func() {
				store.EXPECT().
					ListDownloadingRecordsWithMovie(mock.Anything).
					Return(nil, nil).Once()

				completed, err := mgr.CheckStatus(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(completed).To(BeEmpty())
			})
		})

		When("a record references no known download client", func() {
			It("skips it and returns no completions", func() {
				configtest.Setup()
				store.EXPECT().
					ListDownloadingRecordsWithMovie(mock.Anything).
					Return([]*ent.DownloadRecord{
						{ID: 1, TorrentHash: "abc"},
					}, nil).Once()

				completed, err := mgr.CheckStatus(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(completed).To(BeEmpty())
			})
		})
	})

	Describe("RemoveTorrent", func() {
		When("the download client is unknown", func() {
			It("returns a not-found error", func() {
				configtest.Setup()
				Expect(
					mgr.RemoveTorrent(ctx, "ghost", "abc", false),
				).To(MatchError(ContainSubstring("not found")))
			})
		})
	})

	Describe("Test", func() {
		When("the supplied client type is unsupported", func() {
			It("returns ErrUnsupportedClient", func() {
				// Free-form TestParams bypass config's oneof validation, so an
				// unknown type still reaches buildClient's default guard.
				err := mgr.Test(ctx, TestParams{
					ClientType: "rtorrent",
					Host:       "rt.local",
				})
				Expect(err).To(MatchError(ErrUnsupportedClient))
			})
		})
	})

	Describe("TestByName", func() {
		When("the entry is missing", func() {
			It("returns ErrDownloadClientNotFound", func() {
				configtest.Setup()
				Expect(mgr.TestByName(ctx, "ghost")).
					To(MatchError(ContainSubstring("not found")))
			})
		})
	})

	Describe("PurgeOldRecords", func() {
		It("returns nil when both deletes succeed with zero rows", func() {
			cleaner := mgr.(Cleaner)
			store.EXPECT().DeleteCompletedDownloadRecordsBefore(
				mock.Anything, mock.AnythingOfType("time.Time"),
			).Return(0, nil).Once()
			store.EXPECT().DeleteFailedDownloadRecordsBefore(
				mock.Anything, mock.AnythingOfType("time.Time"),
			).Return(0, nil).Once()
			Expect(cleaner.PurgeOldRecords(ctx)).To(Succeed())
		})

		It("passes the right cutoffs to each delete", func() {
			cleaner := mgr.(Cleaner)
			var compCutoff, failCutoff time.Time
			store.EXPECT().DeleteCompletedDownloadRecordsBefore(
				mock.Anything, mock.AnythingOfType("time.Time"),
			).Run(func(_ context.Context, c time.Time) { compCutoff = c }).
				Return(2, nil).Once()
			store.EXPECT().DeleteFailedDownloadRecordsBefore(
				mock.Anything, mock.AnythingOfType("time.Time"),
			).Run(func(_ context.Context, c time.Time) { failCutoff = c }).
				Return(1, nil).Once()

			Expect(cleaner.PurgeOldRecords(ctx)).To(Succeed())
			Expect(
				time.Since(compCutoff),
			).To(BeNumerically("~", completedRecordRetention, time.Second))
			Expect(
				time.Since(failCutoff),
			).To(BeNumerically("~", failedRecordRetention, time.Second))
		})

		It("joins errors when both deletes fail", func() {
			cleaner := mgr.(Cleaner)
			store.EXPECT().DeleteCompletedDownloadRecordsBefore(
				mock.Anything, mock.Anything,
			).Return(0, errors.New("comp")).Once()
			store.EXPECT().DeleteFailedDownloadRecordsBefore(
				mock.Anything, mock.Anything,
			).Return(0, errors.New("fail")).Once()
			err := cleaner.PurgeOldRecords(ctx)
			Expect(err).To(MatchError(ContainSubstring("comp")))
			Expect(err).To(MatchError(ContainSubstring("fail")))
		})
	})

	Describe("Queue", func() {
		It("wraps the store error", func() {
			boom := errors.New("db boom")
			store.EXPECT().ListActiveDownloadRecords(mock.Anything).
				Return(nil, boom).Once()
			_, err := mgr.Queue(ctx)
			Expect(err).To(MatchError(boom))
		})

		It("maps importing records to progress 1.0 without polling", func() {
			rec := &ent.DownloadRecord{
				ID: 7, Title: "Dune", Status: downloadrecord.StatusImporting,
			}
			rec.Edges.Movie = &ent.Movie{ID: 1, Title: "Dune"}
			store.EXPECT().ListActiveDownloadRecords(mock.Anything).
				Return([]*ent.DownloadRecord{rec}, nil).Once()
			snap, err := mgr.Queue(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(snap.Items).To(HaveLen(1))
			Expect(snap.Items[0].Status).To(Equal("importing"))
			Expect(snap.Items[0].Progress).To(Equal(1.0))
		})

		It("serves the cached snapshot within the TTL (one store call)", func() {
			// Two calls in immediate succession land inside the 2s TTL
			// window, so the store is queried exactly once.
			store.EXPECT().ListActiveDownloadRecords(mock.Anything).
				Return(nil, nil).Once()
			_, err := mgr.Queue(ctx)
			Expect(err).NotTo(HaveOccurred())
			_, err = mgr.Queue(ctx)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("CancelQueueItem", func() {
		It("propagates NotFound when the record is absent", func() {
			store.EXPECT().
				FindActiveDownloadRecordByID(mock.Anything, uint32(9)).
				Return(nil, &ent.NotFoundError{}).Once()
			err := mgr.CancelQueueItem(ctx, 9)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})

		It("deletes the record and reverts the movie (no client edge)", func() {
			rec := &ent.DownloadRecord{
				ID: 3, Status: downloadrecord.StatusDownloading,
			}
			rec.Edges.Movie = &ent.Movie{ID: 5}
			store.EXPECT().
				FindActiveDownloadRecordByID(mock.Anything, uint32(3)).
				Return(rec, nil).Once()
			store.EXPECT().
				DeleteDownloadRecord(mock.Anything, uint32(3)).
				Return(nil).Once()
			store.EXPECT().
				RevertMovieToWantedIfNoFile(mock.Anything, uint32(5)).
				Return(nil).Once()
			Expect(mgr.CancelQueueItem(ctx, 3)).To(Succeed())
		})

		It("refuses a held record without touching it", func() {
			store.EXPECT().
				FindActiveDownloadRecordByID(mock.Anything, uint32(4)).
				Return(&ent.DownloadRecord{
					ID: 4, Status: downloadrecord.StatusHeld,
				}, nil).Once()
			Expect(mgr.CancelQueueItem(ctx, 4)).To(MatchError(ErrRecordHeld))
		})
	})

	Describe("PurgeRecordForHash", func() {
		It("is a no-op when no record tracks the hash", func() {
			store.EXPECT().
				FindLiveDownloadRecordByHash(mock.Anything, "H").
				Return(nil, nil).Once()
			Expect(mgr.PurgeRecordForHash(ctx, "H")).To(Succeed())
		})

		It("deletes the record and reverts its movie", func() {
			rec := &ent.DownloadRecord{
				ID: 7, Status: downloadrecord.StatusDownloading, TorrentHash: "H",
			}
			rec.Edges.Movie = &ent.Movie{ID: 5}
			store.EXPECT().
				FindLiveDownloadRecordByHash(mock.Anything, "H").
				Return(rec, nil).Once()
			store.EXPECT().
				DeleteDownloadRecord(mock.Anything, uint32(7)).
				Return(nil).Once()
			store.EXPECT().
				RevertMovieToWantedIfNoFile(mock.Anything, uint32(5)).
				Return(nil).Once()
			store.EXPECT().
				RevertOrphanedDownloadingEpisodes(mock.Anything).
				Return(0, nil).Once()
			Expect(mgr.PurgeRecordForHash(ctx, "H")).To(Succeed())
		})
	})

	Describe("PauseQueueItem", func() {
		It("propagates NotFound when the record is absent", func() {
			store.EXPECT().
				FindActiveDownloadRecordByID(mock.Anything, uint32(1)).
				Return(nil, &ent.NotFoundError{}).Once()
			Expect(ent.IsNotFound(mgr.PauseQueueItem(ctx, 1))).To(BeTrue())
		})

		It("refuses a held record", func() {
			store.EXPECT().
				FindActiveDownloadRecordByID(mock.Anything, uint32(2)).
				Return(&ent.DownloadRecord{
					ID: 2, Status: downloadrecord.StatusHeld,
				}, nil).Once()
			Expect(mgr.PauseQueueItem(ctx, 2)).To(MatchError(ErrRecordHeld))
		})
	})
})

// stubClient is a no-op Client used to assert buildClient returns the injected
// builtin engine by pointer identity. A local stub (rather than
// download/mocks) keeps this internal test package free of the import cycle
// download/mocks → download.
type stubClient struct{}

func (stubClient) AddTorrent(context.Context, TorrentSource) (string, error) {
	return "", nil
}

func (stubClient) GetTorrent(context.Context, string) (*Torrent, error) {
	return nil, nil
}

func (stubClient) ListTorrents(context.Context) ([]Torrent, error) {
	return nil, nil
}
func (stubClient) RemoveTorrent(context.Context, string, bool) error { return nil }
func (stubClient) PauseTorrent(context.Context, string) error        { return nil }
func (stubClient) ResumeTorrent(context.Context, string) error       { return nil }
func (stubClient) TestConnection(context.Context) error              { return nil }

var _ = Describe("buildClient builtin", Label("unit", "downloads"), func() {
	It("returns the injected engine", func() {
		engine := &stubClient{}
		d := New(nil, engine).(*download)
		c, err := d.buildClient(config.DownloadClientEntry{
			ClientType: "builtin", Name: "embedded",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(c).To(BeIdenticalTo(engine))
	})

	It("errors when no engine is running", func() {
		d := New(nil, nil).(*download)
		_, err := d.buildClient(config.DownloadClientEntry{
			ClientType: "builtin", Name: "embedded",
		})
		Expect(err).To(MatchError(ErrUnsupportedClient))
		Expect(err.Error()).To(ContainSubstring("restart"))
	})
})
