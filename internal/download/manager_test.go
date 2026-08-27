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

	"github.com/anacrolix/torrent/metainfo"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
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
				_, err := mgr.GrabEpisode(
					ctx,
					indexer.SearchResult{Title: "x"},
					1,
					nil,
				)
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

		When(
			"a client-paused torrent belongs to a pending-selection record",
			func() {
				// spec §4.2: qBittorrent reports "paused" while stopped at metadata
				// during Flow B. A pending record must not flash the paused badge —
				// that state is RunSelectionPass's window to resolve, not something
				// the user did.
				It("syncs season state as NOT paused", func() {
					configtest.Setup(map[string]any{
						"download_clients": []map[string]any{{
							"name": "embedded", "client_type": "builtin",
							"download_dir": "/downloads", "enabled": true,
						}},
					})
					client := &fakePassClient{
						torrent: &Torrent{Hash: "abc", Status: StatusPaused},
					}
					pendingMgr := New(store, client)
					store.EXPECT().
						ListDownloadingRecordsWithMovie(mock.Anything).
						Return([]*ent.DownloadRecord{{
							ID:                 7,
							TorrentHash:        "abc",
							DownloadClientName: "embedded",
							SelectionState:     downloadrecord.SelectionStatePending,
						}}, nil).Once()
					store.EXPECT().
						SyncSeasonDownloadStateForRecord(mock.Anything, uint32(7), false).
						Return(nil).Once()

					completed, err := pendingMgr.CheckStatus(ctx)

					Expect(err).NotTo(HaveOccurred())
					Expect(completed).To(BeEmpty())
				})
			},
		)

		When(
			"a seeding-looking torrent belongs to a pending-selection record",
			func() {
				// A pending selection wants no file, so a client that scopes
				// progress to wanted files reports it complete with nothing
				// downloaded. Importing then imports an empty payload.
				It("does not flip the record to importing", func() {
					configtest.Setup(map[string]any{
						"download_clients": []map[string]any{{
							"name": "embedded", "client_type": "builtin",
							"download_dir": "/downloads", "enabled": true,
						}},
					})
					client := &fakePassClient{
						torrent: &Torrent{
							Hash: "abc", Name: "Show.S01", Status: StatusSeeding,
						},
					}
					pendingMgr := New(store, client)
					store.EXPECT().
						ListDownloadingRecordsWithMovie(mock.Anything).
						Return([]*ent.DownloadRecord{{
							ID:                 9,
							TorrentHash:        "abc",
							DownloadClientName: "embedded",
							SelectionState:     downloadrecord.SelectionStatePending,
						}}, nil).Once()
					store.EXPECT().
						SyncSeasonDownloadStateForRecord(mock.Anything, uint32(9), false).
						Return(nil).Once()

					completed, err := pendingMgr.CheckStatus(ctx)

					Expect(err).NotTo(HaveOccurred())
					Expect(completed).To(BeEmpty())
					// UpdateDownloadRecordStatus/MarkRecordEpisodesImporting carry
					// no expectation above: calling either would panic the mock.
				})
			},
		)

		When(
			"a client-paused torrent belongs to a resolved-selection record",
			func() {
				// Negative control for the spec above: only selection_state=pending
				// suppresses the paused mirror. A record whose selection already
				// resolved (applied here; skipped/unsupported take the same path)
				// is paused exactly like any other in-flight record — hardcoding
				// paused=false in CheckStatus would leave this green too.
				It("syncs season state as paused=true", func() {
					configtest.Setup(map[string]any{
						"download_clients": []map[string]any{{
							"name": "embedded", "client_type": "builtin",
							"download_dir": "/downloads", "enabled": true,
						}},
					})
					client := &fakePassClient{
						torrent: &Torrent{Hash: "abc", Status: StatusPaused},
					}
					pendingMgr := New(store, client)
					store.EXPECT().
						ListDownloadingRecordsWithMovie(mock.Anything).
						Return([]*ent.DownloadRecord{{
							ID:                 8,
							TorrentHash:        "abc",
							DownloadClientName: "embedded",
							SelectionState:     downloadrecord.SelectionStateApplied,
						}}, nil).Once()
					store.EXPECT().
						SyncSeasonDownloadStateForRecord(mock.Anything, uint32(8), true).
						Return(nil).Once()

					completed, err := pendingMgr.CheckStatus(ctx)

					Expect(err).NotTo(HaveOccurred())
					Expect(completed).To(BeEmpty())
				})
			},
		)
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

func (stubClient) ListFiles(context.Context, string) ([]TorrentFile, error) {
	return nil, nil
}

func (stubClient) SetWantedFiles(context.Context, string, []int) error { return nil }

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

// fakeSelectiveClient is a spy Client recording AddTorrent/SetWantedFiles
// calls for the selective-grab specs below — a hand-rolled fake instead of
// download/mocks for the same import-cycle reason as stubClient.
type fakeSelectiveClient struct {
	stubClient
	addTorrentCalls int
	addTorrentSrc   TorrentSource
	addHash         string
	addErr          error
	setWantedCalls  int
	setWantedHash   string
	setWantedFiles  []int
	setWantedErr    error
	listFilesCalls  int
	listFilesHash   string
	listFilesResult []TorrentFile
	listFilesErr    error
}

func (f *fakeSelectiveClient) ListFiles(
	_ context.Context, hash string,
) ([]TorrentFile, error) {
	f.listFilesCalls++
	f.listFilesHash = hash
	return f.listFilesResult, f.listFilesErr
}

func (f *fakeSelectiveClient) AddTorrent(
	_ context.Context, src TorrentSource,
) (string, error) {
	f.addTorrentCalls++
	f.addTorrentSrc = src
	if f.addErr != nil {
		return "", f.addErr
	}
	return f.addHash, nil
}

func (f *fakeSelectiveClient) SetWantedFiles(
	_ context.Context, hash string, wanted []int,
) error {
	f.setWantedCalls++
	f.setWantedHash = hash
	f.setWantedFiles = wanted
	return f.setWantedErr
}

var _ = Describe("GrabEpisode selective files", Label("unit", "downloads"), func() {
	var (
		ctx    context.Context
		store  *dbmocks.MockStore
		mgr    Downloader
		client *fakeSelectiveClient
		srv    *httptest.Server
	)

	// twoEpisodeRelease serves a 2-file torrent (S01E01, S01E02, both above the
	// episode floor) over an httptest server addressed by one enabled indexer,
	// with a builtin download client wired to the fake, and returns the
	// release plus a show whose season holds the two matching episodes
	// (21, 22). resolveTorrentSource has to actually fetch bytes for Flow A to
	// engage (it gates on len(src.Bytes) > 0), which is why this fetches for
	// real instead of stubbing TorrentSource directly.
	twoEpisodeRelease := func(selectiveFiles bool) (indexer.SearchResult, *ent.TVShow) {
		raw := buildTorrentBytes(metainfo.Info{
			Name: "Show",
			Files: []metainfo.FileInfo{
				{Path: []string{"Show.S01E01.mkv"}, Length: aboveFloor},
				{Path: []string{"Show.S01E02.mkv"}, Length: aboveFloor},
			},
		})
		srv = httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(raw) },
		))
		DeferCleanup(srv.Close)
		endpoint, err := url.Parse(srv.URL)
		Expect(err).NotTo(HaveOccurred())
		port, err := strconv.Atoi(endpoint.Port())
		Expect(err).NotTo(HaveOccurred())
		configtest.Setup(map[string]any{
			"download": map[string]any{"selective_files": selectiveFiles},
			"indexers": []map[string]any{{
				"name": "tracker", "host": endpoint.Hostname(), "port": port,
				"api_key": "k", "protocol": "torznab", "enabled": true,
			}},
			"download_clients": []map[string]any{{
				"name": "embedded", "client_type": "builtin",
				"download_dir": "/downloads", "enabled": true,
			}},
		})
		show := &ent.TVShow{
			ID:   1,
			Type: tvshow.TypeStandard,
			Edges: ent.TVShowEdges{Seasons: []*ent.Season{{
				Number: 1,
				Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
					{ID: 21, Number: 1},
					{ID: 22, Number: 2},
				}},
			}}},
		}
		result := indexer.SearchResult{
			Title: "Show S01", Download: srv.URL + "/dl",
		}
		return result, show
	}

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		client = &fakeSelectiveClient{addHash: "abc123"}
		mgr = New(store, client)
		// The §4.6 hash pre-check runs before Flow A on every grab whose
		// selective_files is on; these specs are all about a hash with no live
		// record behind it. Maybe rather than Once because the flag-off spec
		// below must not reach it at all, and asserts that itself.
		store.EXPECT().
			FindWidenableDownloadRecordByHash(mock.Anything, mock.Anything).
			Return(nil, nil).Maybe()
	})

	It("zero matches: never calls AddTorrent, error wraps ErrNoWantedFiles", func() {
		result, show := twoEpisodeRelease(true)
		// Wanted episode 999 doesn't exist on the season, so nothing matches.
		store.EXPECT().
			TVShowForEpisode(mock.Anything, uint32(21)).
			Return(show, nil).Once()

		_, err := mgr.GrabEpisode(ctx, result, 21, []uint32{999})

		Expect(errors.Is(err, ErrNoWantedFiles)).To(BeTrue())
		Expect(client.addTorrentCalls).To(Equal(0))
	})

	It(
		"partial match: AddTorrent gets the keep-set, record applied, SetWantedFiles confirmed",
		func() {
			result, show := twoEpisodeRelease(true)
			store.EXPECT().
				TVShowForEpisode(mock.Anything, uint32(21)).
				Return(show, nil).Once()
			// The keep-set is part of the insert, not only of the post-add
			// confirmation: a confirmation that fails for any reason other than
			// ErrNotSupported leaves the record applied, and an applied record
			// with no selected_files renders "0 B of 24 GB selected".
			store.EXPECT().CreateDownloadRecord(mock.Anything, mock.MatchedBy(
				func(p db.CreateDownloadRecordParams) bool {
					return p.EpisodeID == 21 &&
						p.SelectionState == downloadrecord.SelectionStateApplied &&
						len(p.WantedEpisodes) == 1 && p.WantedEpisodes[0] == 21 &&
						len(p.SelectedFiles) == 1 && p.SelectedFiles[0] == 0 &&
						p.SelectedBytes == aboveFloor
				},
			)).Return(&ent.DownloadRecord{ID: 42}, nil).Once()
			store.EXPECT().SetDownloadRecordSelection(
				mock.Anything, uint32(42), downloadrecord.SelectionStateApplied,
				[]int{0}, aboveFloor,
			).Return(nil).Once()

			_, err := mgr.GrabEpisode(ctx, result, 21, []uint32{21})

			Expect(err).NotTo(HaveOccurred())
			Expect(client.addTorrentCalls).To(Equal(1))
			Expect(client.addTorrentSrc.Selective).To(BeTrue())
			Expect(client.addTorrentSrc.WantedFiles).To(Equal([]int{0}))
			Expect(client.setWantedCalls).To(Equal(1))
			Expect(client.setWantedHash).To(Equal("abc123"))
			Expect(client.setWantedFiles).To(Equal([]int{0}))
		},
	)

	It(
		"every candidate wanted: whole grab, record left at the skipped default",
		func() {
			// The matched == videoTotal arm: selecting here would be a no-op
			// selection, so the grab collapses back to a plain whole-torrent one
			// and the record never claims to be selective.
			result, show := twoEpisodeRelease(true)
			store.EXPECT().
				TVShowForEpisode(mock.Anything, uint32(21)).
				Return(show, nil).Once()
			store.EXPECT().CreateDownloadRecord(mock.Anything, mock.MatchedBy(
				func(p db.CreateDownloadRecordParams) bool {
					return p.SelectionState == "" &&
						p.SelectedFiles == nil && p.SelectedBytes == 0
				},
			)).Return(&ent.DownloadRecord{ID: 42}, nil).Once()

			_, err := mgr.GrabEpisode(ctx, result, 21, []uint32{21, 22})

			Expect(err).NotTo(HaveOccurred())
			Expect(client.addTorrentCalls).To(Equal(1))
			Expect(client.addTorrentSrc.Selective).To(BeFalse())
			Expect(client.addTorrentSrc.WantedFiles).To(BeNil())
			// SetDownloadRecordSelection carries no expectation above: calling
			// it would panic the mock.
			Expect(client.setWantedCalls).To(Equal(0))
		},
	)

	It(
		"SetWantedFiles ErrNotSupported flips the record unsupported; grab still succeeds",
		func() {
			result, show := twoEpisodeRelease(true)
			client.setWantedErr = ErrNotSupported
			store.EXPECT().
				TVShowForEpisode(mock.Anything, uint32(21)).
				Return(show, nil).Once()
			store.EXPECT().
				CreateDownloadRecord(mock.Anything, mock.Anything).
				Return(&ent.DownloadRecord{ID: 42}, nil).Once()
			store.EXPECT().SetDownloadRecordSelection(
				mock.Anything, uint32(42),
				downloadrecord.SelectionStateUnsupported, []int(nil), int64(0),
			).Return(nil).Once()

			_, err := mgr.GrabEpisode(ctx, result, 21, []uint32{21})

			Expect(err).NotTo(HaveOccurred())
			Expect(client.setWantedCalls).To(Equal(1))
		},
	)

	It(
		"nil wantedEpisodes: no decode, no SetWantedFiles, whole-torrent grab",
		func() {
			result, _ := twoEpisodeRelease(true)
			store.EXPECT().CreateDownloadRecord(mock.Anything, mock.MatchedBy(
				func(p db.CreateDownloadRecordParams) bool {
					return len(p.WantedEpisodes) == 0 && p.SelectionState == ""
				},
			)).Return(&ent.DownloadRecord{ID: 42}, nil).Once()

			_, err := mgr.GrabEpisode(ctx, result, 21, nil)

			Expect(err).NotTo(HaveOccurred())
			// TVShowForEpisode carries no expectation above: calling it would
			// panic the mock, so reaching here proves the gate never opened.
			Expect(client.addTorrentSrc.Selective).To(BeFalse())
			Expect(client.addTorrentSrc.WantedFiles).To(BeNil())
			Expect(client.setWantedCalls).To(Equal(0))
		},
	)

	It(
		"selective_files off: identical to a nil wanted set even with one present",
		func() {
			result, _ := twoEpisodeRelease(false)
			store.EXPECT().
				CreateDownloadRecord(mock.Anything, mock.Anything).
				Return(&ent.DownloadRecord{ID: 42}, nil).Once()

			_, err := mgr.GrabEpisode(ctx, result, 21, []uint32{21})

			Expect(err).NotTo(HaveOccurred())
			Expect(client.addTorrentSrc.Selective).To(BeFalse())
			Expect(client.setWantedCalls).To(Equal(0))
			// The §4.6 hash pre-check is part of the feature, so off means it
			// never runs — duplicate detection is AddTorrent's again.
			store.AssertNotCalled(
				GinkgoT(), "FindWidenableDownloadRecordByHash",
				mock.Anything, mock.Anything,
			)
		},
	)

	It(
		"magnet: no bytes to inspect, record created pending, no SetWantedFiles",
		func() {
			configtest.Setup(map[string]any{
				"download": map[string]any{"selective_files": true},
				"download_clients": []map[string]any{{
					"name": "embedded", "client_type": "builtin",
					"download_dir": "/downloads", "enabled": true,
				}},
			})
			result := indexer.SearchResult{
				Title:    "Show S01",
				Download: "magnet:?xt=urn:btih:deadbeef",
			}
			store.EXPECT().CreateDownloadRecord(mock.Anything, mock.MatchedBy(
				func(p db.CreateDownloadRecordParams) bool {
					return p.EpisodeID == 21 &&
						p.SelectionState == downloadrecord.SelectionStatePending &&
						len(p.WantedEpisodes) == 1 && p.WantedEpisodes[0] == 21
				},
			)).Return(&ent.DownloadRecord{ID: 43}, nil).Once()

			_, err := mgr.GrabEpisode(ctx, result, 21, []uint32{21})

			Expect(err).NotTo(HaveOccurred())
			Expect(client.addTorrentCalls).To(Equal(1))
			Expect(client.addTorrentSrc.Selective).To(BeTrue())
			Expect(client.addTorrentSrc.WantedFiles).To(BeNil())
			// TVShowForEpisode and SetWantedFiles carry no expectation above:
			// calling either would panic the mock — the pass owns resolution.
			Expect(client.setWantedCalls).To(Equal(0))
		},
	)
})

// fakePrefetchClient adds a MagnetMetadataFetcher implementation on top of
// fakeSelectiveClient. It is a separate type (rather than a method on
// fakeSelectiveClient itself) so the existing magnet specs above, which use
// a plain *fakeSelectiveClient, keep exercising a client that does NOT carry
// the optional capability — client.(MagnetMetadataFetcher) must fail for them.
type fakePrefetchClient struct {
	fakeSelectiveClient
	fetchCalls  int
	fetchMagnet string
	fetchBytes  []byte
	fetchErr    error
}

func (f *fakePrefetchClient) FetchMagnetMetadata(
	_ context.Context, magnet string,
) ([]byte, error) {
	f.fetchCalls++
	f.fetchMagnet = magnet
	return f.fetchBytes, f.fetchErr
}

var _ = Describe("GrabEpisode magnet prefetch", Label("unit", "downloads"), func() {
	var (
		ctx    context.Context
		store  *dbmocks.MockStore
		mgr    Downloader
		client *fakePrefetchClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		client = &fakePrefetchClient{
			fakeSelectiveClient: fakeSelectiveClient{addHash: "abc123"},
		}
		mgr = New(store, client)
		// The §4.6 hash pre-check runs before Flow A on every grab.
		store.EXPECT().
			FindWidenableDownloadRecordByHash(mock.Anything, mock.Anything).
			Return(nil, nil).Once()
		configtest.Setup(map[string]any{
			"download": map[string]any{"selective_files": true},
			"download_clients": []map[string]any{{
				"name": "embedded", "client_type": "builtin",
				"download_dir": "/downloads", "enabled": true,
			}},
		})
	})

	magnetResult := func() indexer.SearchResult {
		return indexer.SearchResult{
			Title:    "Show S01",
			Download: "magnet:?xt=urn:btih:deadbeef",
		}
	}

	// twoEpisodeShow matches the two-file torrent both prefetch fixtures below
	// hand back: S01E01 id=21, S01E02 id=22.
	twoEpisodeShow := func() *ent.TVShow {
		return &ent.TVShow{
			ID:   1,
			Type: tvshow.TypeStandard,
			Edges: ent.TVShowEdges{Seasons: []*ent.Season{{
				Number: 1,
				Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
					{ID: 21, Number: 1},
					{ID: 22, Number: 2},
				}},
			}}},
		}
	}

	twoEpisodeMetainfo := func() []byte {
		return buildTorrentBytes(metainfo.Info{
			Name: "Show",
			Files: []metainfo.FileInfo{
				{Path: []string{"Show.S01E01.mkv"}, Length: aboveFloor},
				{Path: []string{"Show.S01E02.mkv"}, Length: aboveFloor},
			},
		})
	}

	It(
		"fetcher success: AddTorrent gets Bytes+WantedFiles, record applied immediately",
		func() {
			client.fetchBytes = twoEpisodeMetainfo()
			show := twoEpisodeShow()
			store.EXPECT().
				TVShowForEpisode(mock.Anything, uint32(21)).
				Return(show, nil).Once()
			store.EXPECT().CreateDownloadRecord(mock.Anything, mock.MatchedBy(
				func(p db.CreateDownloadRecordParams) bool {
					return p.EpisodeID == 21 &&
						p.SelectionState == downloadrecord.SelectionStateApplied
				},
			)).Return(&ent.DownloadRecord{ID: 42}, nil).Once()
			store.EXPECT().SetDownloadRecordSelection(
				mock.Anything, uint32(42), downloadrecord.SelectionStateApplied,
				[]int{0}, aboveFloor,
			).Return(nil).Once()

			_, err := mgr.GrabEpisode(ctx, magnetResult(), 21, []uint32{21})

			Expect(err).NotTo(HaveOccurred())
			Expect(client.fetchCalls).To(Equal(1))
			Expect(client.fetchMagnet).To(Equal("magnet:?xt=urn:btih:deadbeef"))
			Expect(client.addTorrentCalls).To(Equal(1))
			Expect(client.addTorrentSrc.Magnet).To(BeEmpty())
			Expect(client.addTorrentSrc.Bytes).NotTo(BeEmpty())
			Expect(client.addTorrentSrc.WantedFiles).To(Equal([]int{0}))
			Expect(client.setWantedCalls).To(Equal(1))
		},
	)

	It("fetcher error: falls back to magnet add, record pending", func() {
		client.fetchErr = errors.New("rpc timeout")
		store.EXPECT().CreateDownloadRecord(mock.Anything, mock.MatchedBy(
			func(p db.CreateDownloadRecordParams) bool {
				return p.EpisodeID == 21 &&
					p.SelectionState == downloadrecord.SelectionStatePending
			},
		)).Return(&ent.DownloadRecord{ID: 43}, nil).Once()

		_, err := mgr.GrabEpisode(ctx, magnetResult(), 21, []uint32{21})

		Expect(err).NotTo(HaveOccurred())
		Expect(client.fetchCalls).To(Equal(1))
		Expect(client.addTorrentCalls).To(Equal(1))
		Expect(client.addTorrentSrc.Magnet).To(Equal("magnet:?xt=urn:btih:deadbeef"))
		// TVShowForEpisode and SetWantedFiles carry no expectation above:
		// calling either would panic the mock.
		Expect(client.setWantedCalls).To(Equal(0))
	})

	It(
		"zero match on prefetched bytes: fails wrapping ErrNoWantedFiles, no AddTorrent",
		func() {
			client.fetchBytes = twoEpisodeMetainfo()
			show := twoEpisodeShow()
			store.EXPECT().
				TVShowForEpisode(mock.Anything, uint32(21)).
				Return(show, nil).Once()

			_, err := mgr.GrabEpisode(ctx, magnetResult(), 21, []uint32{999})

			Expect(errors.Is(err, ErrNoWantedFiles)).To(BeTrue())
			Expect(client.fetchCalls).To(Equal(1))
			Expect(client.addTorrentCalls).To(Equal(0))
		},
	)
})

// widenShow is a two-episode show (S01E01 id=21, S01E02 id=22) matching the
// file names widenSelection's mock ListFiles responses use below.
func widenShow() *ent.TVShow {
	return &ent.TVShow{
		ID:   1,
		Type: tvshow.TypeStandard,
		Edges: ent.TVShowEdges{Seasons: []*ent.Season{{
			Number: 1,
			Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
				{ID: 21, Number: 1},
				{ID: 22, Number: 2},
			}},
		}}},
	}
}

var _ = Describe(
	"grab widens a live hash's selection",
	Label("unit", "downloads"),
	func() {
		var (
			ctx    context.Context
			store  *dbmocks.MockStore
			mgr    Downloader
			client *fakeSelectiveClient
		)

		const hash = "abc123"

		selectiveConfig := func() {
			configtest.Setup(map[string]any{
				"download": map[string]any{"selective_files": true},
				"download_clients": []map[string]any{{
					"name": "embedded", "client_type": "builtin",
					"download_dir": "/downloads", "enabled": true,
				}},
			})
		}

		BeforeEach(func() {
			ctx = context.Background()
			store = dbmocks.NewMockStore(GinkgoT())
			client = &fakeSelectiveClient{addHash: hash}
			mgr = New(store, client)
			// download.selective_files defaults false — most specs below that
			// need it on call selectiveConfig() themselves; this default is
			// exactly what the flag-off spec needs, unchanged.
			configtest.Setup(map[string]any{
				"download_clients": []map[string]any{{
					"name": "embedded", "client_type": "builtin",
					"download_dir": "/downloads", "enabled": true,
				}},
			})
		})

		It(
			"live selective record + same hash: no AddTorrent, union written, "+
				"SetWantedFiles called, status seeding->downloading, no failure bump",
			func() {
				selectiveConfig()
				live := &ent.DownloadRecord{
					ID:                 42,
					TorrentHash:        hash,
					DownloadClientName: "embedded",
					Status:             downloadrecord.StatusCompleted,
					WantedEpisodes:     []uint32{21},
					SelectionState:     downloadrecord.SelectionStateApplied,
				}
				client.listFilesResult = []TorrentFile{
					{Index: 0, Path: "Show.S01E01.mkv", Size: aboveFloor},
					{Index: 1, Path: "Show.S01E02.mkv", Size: aboveFloor},
				}
				store.EXPECT().
					FindWidenableDownloadRecordByHash(mock.Anything, hash).
					Return(live, nil).Once()
				store.EXPECT().
					TVShowForEpisode(mock.Anything, uint32(22)).
					Return(widenShow(), nil).Once()
				store.EXPECT().
					SetDownloadRecordWantedEpisodes(
						mock.Anything, uint32(42), []uint32{21, 22},
					).
					Return(nil).Once()
				store.EXPECT().
					SetDownloadRecordSelection(
						mock.Anything, uint32(42), downloadrecord.SelectionStateApplied,
						[]int{0, 1}, 2*aboveFloor,
					).
					Return(nil).Once()
				store.EXPECT().
					MarkEpisodeDownloading(mock.Anything, uint32(22)).
					Return(true, nil).Once()
				store.EXPECT().
					UpdateDownloadRecordStatus(
						mock.Anything, uint32(42), downloadrecord.StatusDownloading,
					).
					Return(nil).Once()

				result := indexer.SearchResult{
					Title: "Show S01", Download: "magnet:?xt=urn:btih:" + hash,
				}
				_, err := mgr.GrabEpisode(ctx, result, 22, []uint32{22})

				Expect(err).NotTo(HaveOccurred())
				Expect(client.addTorrentCalls).To(Equal(0))
				Expect(client.setWantedCalls).To(Equal(1))
				Expect(client.setWantedHash).To(Equal(hash))
				Expect(client.setWantedFiles).To(Equal([]int{0, 1}))
			},
		)

		It(
			"live skipped-state (non-selective) record + same hash: "+
				"ErrTorrentAlreadyExists exactly as today",
			func() {
				selectiveConfig()
				live := &ent.DownloadRecord{
					ID:                 42,
					TorrentHash:        hash,
					DownloadClientName: "embedded",
					Status:             downloadrecord.StatusDownloading,
				}
				store.EXPECT().
					FindWidenableDownloadRecordByHash(mock.Anything, hash).
					Return(live, nil).Once()

				result := indexer.SearchResult{
					Title: "Show S01", Download: "magnet:?xt=urn:btih:" + hash,
				}
				_, err := mgr.GrabEpisode(ctx, result, 22, []uint32{22})

				Expect(errors.Is(err, ErrTorrentAlreadyExists)).To(BeTrue())
				Expect(client.addTorrentCalls).To(Equal(0))
				// SetDownloadRecordWantedEpisodes carries no expectation above:
				// calling it would panic the mock.
			},
		)

		It(
			"hash hit with an unsupported-state record: ErrTorrentAlreadyExists, "+
				"no union write, no ListFiles call",
			func() {
				selectiveConfig()
				live := &ent.DownloadRecord{
					ID:                 42,
					TorrentHash:        hash,
					DownloadClientName: "embedded",
					Status:             downloadrecord.StatusDownloading,
					WantedEpisodes:     []uint32{21},
					SelectionState:     downloadrecord.SelectionStateUnsupported,
				}
				store.EXPECT().
					FindWidenableDownloadRecordByHash(mock.Anything, hash).
					Return(live, nil).Once()

				result := indexer.SearchResult{
					Title: "Show S01", Download: "magnet:?xt=urn:btih:" + hash,
				}
				_, err := mgr.GrabEpisode(ctx, result, 22, []uint32{22})

				Expect(errors.Is(err, ErrTorrentAlreadyExists)).To(BeTrue())
				Expect(client.addTorrentCalls).To(Equal(0))
				Expect(client.listFilesCalls).To(Equal(0))
				// SetDownloadRecordWantedEpisodes carries no expectation above:
				// calling it would panic the mock.
			},
		)

		It(
			"selective_files off: no hash pre-check at all, AddTorrent owns "+
				"duplicate detection",
			func() {
				// Outer BeforeEach's config leaves download.selective_files at
				// its default (false) — deliberately not calling
				// selectiveConfig() here. Off has to be the pre-feature path:
				// the whole pre-check is skipped, so the grab reaches the client
				// exactly as it did before this feature and a duplicate hash is
				// whatever AddTorrent says it is.
				store.EXPECT().
					CreateDownloadRecord(mock.Anything, mock.Anything).
					Return(&ent.DownloadRecord{ID: 99}, nil).Once()

				result := indexer.SearchResult{
					Title: "Show S01", Download: "magnet:?xt=urn:btih:" + hash,
				}
				_, err := mgr.GrabEpisode(ctx, result, 22, []uint32{22})

				Expect(err).NotTo(HaveOccurred())
				Expect(client.addTorrentCalls).To(Equal(1))
				Expect(client.listFilesCalls).To(Equal(0))
				// FindWidenableDownloadRecordByHash carries no expectation
				// above: calling it would panic the mock.
			},
		)

		It("no live record: normal grab proceeds", func() {
			selectiveConfig()
			store.EXPECT().
				FindWidenableDownloadRecordByHash(mock.Anything, hash).
				Return(nil, nil).Once()
			store.EXPECT().
				CreateDownloadRecord(mock.Anything, mock.Anything).
				Return(&ent.DownloadRecord{ID: 99}, nil).Once()

			result := indexer.SearchResult{
				Title: "Show S01", Download: "magnet:?xt=urn:btih:" + hash,
			}
			_, err := mgr.GrabEpisode(ctx, result, 22, []uint32{22})

			Expect(err).NotTo(HaveOccurred())
			Expect(client.addTorrentCalls).To(Equal(1))
		})

		It(
			"widen with a metadata-less client: record flipped pending, no SetWantedFiles",
			func() {
				selectiveConfig()
				live := &ent.DownloadRecord{
					ID:                 42,
					TorrentHash:        hash,
					DownloadClientName: "embedded",
					Status:             downloadrecord.StatusDownloading,
					WantedEpisodes:     []uint32{21},
					SelectionState:     downloadrecord.SelectionStatePending,
				}
				client.listFilesResult = nil // metadata not yet resolved
				store.EXPECT().
					FindWidenableDownloadRecordByHash(mock.Anything, hash).
					Return(live, nil).Once()
				store.EXPECT().
					SetDownloadRecordWantedEpisodes(
						mock.Anything, uint32(42), []uint32{21, 22},
					).
					Return(nil).Once()
				store.EXPECT().
					SetDownloadRecordSelection(
						mock.Anything, uint32(42), downloadrecord.SelectionStatePending,
						[]int(nil), int64(0),
					).
					Return(nil).Once()

				result := indexer.SearchResult{
					Title: "Show S01", Download: "magnet:?xt=urn:btih:" + hash,
				}
				rec, err := mgr.GrabEpisode(ctx, result, 22, []uint32{22})

				Expect(err).NotTo(HaveOccurred())
				Expect(
					rec.SelectionState,
				).To(Equal(downloadrecord.SelectionStatePending))
				Expect(client.addTorrentCalls).To(Equal(0))
				Expect(client.setWantedCalls).To(Equal(0))
				// TVShowForEpisode/MarkEpisodeDownloading/UpdateDownloadRecordStatus
				// carry no expectation above: calling any of them would panic the
				// mock, so reaching here proves the pending flip returned early.
			},
		)

		It(
			"widen where the keep-set matches nothing: no SetWantedFiles call, "+
				"no union write",
			func() {
				selectiveConfig()
				live := &ent.DownloadRecord{
					ID:                 42,
					TorrentHash:        hash,
					DownloadClientName: "embedded",
					Status:             downloadrecord.StatusDownloading,
					WantedEpisodes:     []uint32{21},
					SelectionState:     downloadrecord.SelectionStateApplied,
				}
				// A video file the show's own season/episode list has no
				// counterpart for — matches neither 21 nor 22.
				client.listFilesResult = []TorrentFile{
					{Index: 0, Path: "Show.S05E99.mkv", Size: aboveFloor},
				}
				store.EXPECT().
					FindWidenableDownloadRecordByHash(mock.Anything, hash).
					Return(live, nil).Once()
				store.EXPECT().
					TVShowForEpisode(mock.Anything, uint32(22)).
					Return(widenShow(), nil).Once()

				result := indexer.SearchResult{
					Title: "Show S01", Download: "magnet:?xt=urn:btih:" + hash,
				}
				_, err := mgr.GrabEpisode(ctx, result, 22, []uint32{22})

				Expect(errors.Is(err, ErrTorrentAlreadyExists)).To(BeTrue())
				Expect(client.setWantedCalls).To(Equal(0))
				// SetDownloadRecordWantedEpisodes/SetDownloadRecordSelection
				// carry no expectation above: calling either would panic the
				// mock, so reaching here proves no DB write happened.
			},
		)
	},
)
