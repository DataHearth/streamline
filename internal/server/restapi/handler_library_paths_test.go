package restapi

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/roottest"
)

var _ = Describe("Handler: Library path migration",
	Label("unit", "server", "library"), func() {
		var (
			app  *apiKeyApp
			root string
		)

		BeforeEach(func() {
			app = newAPIKeyApp()
			root = filepath.Join(GinkgoT().TempDir(), "movies")
			configtest.SetupFile(map[string]any{
				"library": map[string]any{"movie_path": root},
			})
		})

		Describe("accept-missing", func() {
			const url = "/api/v1/library/roots/music/accept-missing"

			It("removes what is gone and answers the count", func() {
				roottest.Offline(db.RootMusic)
				app.store.EXPECT().
					ListRootFilesForDrift(mock.Anything, db.RootMusic, uint32(0), mock.Anything).
					Return([]db.DriftRow{{ID: 7, Path: "/gone/file.flac"}}, nil).
					Once()
				app.store.EXPECT().
					ListRootFilesForDrift(mock.Anything, db.RootMusic, uint32(7), mock.Anything).
					Return(nil, nil).Once()
				app.store.EXPECT().FindMediaFileWithOwners(mock.Anything, uint32(7)).
					Return(&ent.MediaFile{ID: 7, Edges: ent.MediaFileEdges{
						Track: &ent.Track{
							Edges: ent.TrackEdges{Album: &ent.Album{ID: 3}},
						},
					}}, nil).Once()
				app.store.EXPECT().
					HoldsOtherFile(mock.Anything, mock.Anything).
					Return(false, nil).Once()
				app.store.EXPECT().
					DeleteMediaFileAndRevertAlbum(mock.Anything, uint32(7), uint32(3)).
					Return(nil).Once()

				resp := app.do(app.req(http.MethodPost, url, app.adminKey, nil))
				defer resp.Body.Close()

				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var body AcceptMissingResult
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Removed).To(Equal(1))
			})

			gone := func() []db.DriftRow {
				return []db.DriftRow{{ID: 7, Path: "/gone/file.flac"}}
			}
			expectRemoval := func() {
				app.store.EXPECT().FindMediaFileWithOwners(mock.Anything, uint32(7)).
					Return(&ent.MediaFile{ID: 7, Edges: ent.MediaFileEdges{
						Track: &ent.Track{
							Edges: ent.TrackEdges{Album: &ent.Album{ID: 3}},
						},
					}}, nil).Once()
				app.store.EXPECT().
					HoldsOtherFile(mock.Anything, mock.Anything).
					Return(false, nil).Once()
				app.store.EXPECT().
					DeleteMediaFileAndRevertAlbum(mock.Anything, uint32(7), uint32(3)).
					Return(nil).Once()
			}

			It("outlives the listener's write timeout", func() {
				app.srv.Config.WriteTimeout = 50 * time.Millisecond
				roottest.Offline(db.RootMusic)
				app.store.EXPECT().
					ListRootFilesForDrift(mock.Anything, db.RootMusic, uint32(0), mock.Anything).
					RunAndReturn(func(context.Context, db.LibraryRoot, uint32, int) ([]db.DriftRow, error) {
						time.Sleep(200 * time.Millisecond)
						return gone(), nil
					}).
					Once()
				app.store.EXPECT().
					ListRootFilesForDrift(mock.Anything, db.RootMusic, uint32(7), mock.Anything).
					Return(nil, nil).Once()
				expectRemoval()

				resp := app.do(app.req(http.MethodPost, url, app.adminKey, nil))
				defer resp.Body.Close()

				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})

			It("finishes the walk when the client goes away", func() {
				roottest.Offline(db.RootMusic)
				reqCtx, cancel := context.WithCancel(context.Background())
				defer cancel()
				finished := make(chan struct{})
				app.store.EXPECT().
					ListRootFilesForDrift(mock.Anything, db.RootMusic, uint32(0), mock.Anything).
					RunAndReturn(func(context.Context, db.LibraryRoot, uint32, int) ([]db.DriftRow, error) {
						cancel()
						time.Sleep(200 * time.Millisecond)
						return gone(), nil
					}).
					Once()
				app.store.EXPECT().
					ListRootFilesForDrift(mock.Anything, db.RootMusic, uint32(7), mock.Anything).
					RunAndReturn(func(context.Context, db.LibraryRoot, uint32, int) ([]db.DriftRow, error) {
						close(finished)
						return nil, nil
					}).
					Once()
				expectRemoval()

				go func() {
					defer GinkgoRecover()
					resp, err := http.DefaultClient.Do(
						app.req(http.MethodPost, url, app.adminKey, nil).
							WithContext(reqCtx),
					)
					if err == nil {
						Expect(resp.Body.Close()).To(Succeed())
					}
				}()

				Eventually(finished, "5s").Should(BeClosed())
			})

			It("refuses a member", func() {
				app.addMember("")
				roottest.Offline(db.RootMusic)

				resp := app.do(app.req(http.MethodPost, url, app.memberKey, nil))
				defer resp.Body.Close()

				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			})

			It("answers 409 once the root is back", func() {
				roottest.Offline()

				resp := app.do(app.req(http.MethodPost, url, app.adminKey, nil))
				defer resp.Body.Close()

				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})

			It("rejects a root that does not exist", func() {
				resp := app.do(app.req(
					http.MethodPost,
					"/api/v1/library/roots/downloads/accept-missing",
					app.adminKey,
					nil,
				))
				defer resp.Body.Close()

				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			})
		})

		It("GET reports an idle migration before anything has run", func() {
			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/path-migration", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body PathMigration
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Running).To(BeFalse())
			Expect(body.Total).To(BeZero())
		})

		It("GET roots reports each configured root and its tracked count", func() {
			app.store.EXPECT().
				ListMediaFilesByPathPrefix(mock.Anything, root).
				Return([]*ent.MediaFile{
					{ID: 1, Path: filepath.Join(root, "Dune (2021)/d.mkv")},
				}, nil).Once()
			app.store.EXPECT().CountMovieMediaFiles(mock.Anything).
				Return(1, nil).Once()
			app.store.EXPECT().
				ListMediaFilesByPathPrefix(mock.Anything, mock.Anything).
				Return(nil, nil).Times(4)
			app.store.EXPECT().CountEpisodeMediaFiles(mock.Anything).
				Return(0, nil).Once()
			app.store.EXPECT().CountRootFiles(mock.Anything, mock.Anything).
				Return(0, nil).Times(3)
			app.store.EXPECT().
				ListDownloadRecordsByPathPrefix(mock.Anything, mock.Anything).
				Return(nil, nil).Once()
			app.store.EXPECT().
				ListTorrentSessionsByPathPrefix(mock.Anything, mock.Anything).
				Return(nil, nil).Once()
			app.store.EXPECT().CountLiveDownloadRecords(mock.Anything).
				Return(0, nil).Once()
			app.store.EXPECT().CountTorrentSessions(mock.Anything).
				Return(0, nil).Once()

			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/path-migration/roots", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body PathMigrationRootList
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(6))
			Expect(body.Items[0].Path).To(Equal(root))
			Expect(body.Items[0].Tracked).To(Equal(1))
			Expect(body.Items[0].Total).To(Equal(1))
		})

		It("POST preview counts the rows under the configured root", func() {
			app.store.EXPECT().
				ListMediaFilesByPathPrefix(mock.Anything, root).
				Return([]*ent.MediaFile{
					{ID: 1, Path: filepath.Join(root, "Dune (2021)/d.mkv")},
				}, nil).Once()

			resp := app.do(app.req(http.MethodPost,
				"/api/v1/library/path-migration/preview", app.adminKey,
				strings.NewReader(`{"root":"movies","to":"/data/movies"}`)))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body PathMigrationPreview
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.From).To(Equal(root))
			Expect(body.To).To(Equal("/data/movies"))
			Expect(body.Total).To(Equal(1))
			// The file isn't at the destination, so the row would be left alone.
			Expect(body.Skipped).To(Equal(1))
			Expect(body.CanMove).To(BeTrue())
			Expect(body.Samples).To(HaveLen(1))
		})

		It("POST preview rejects a relative destination with 422", func() {
			resp := app.do(app.req(http.MethodPost,
				"/api/v1/library/path-migration/preview", app.adminKey,
				strings.NewReader(`{"root":"movies","to":"movies"}`)))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("POST rejects moving files for the download root with 422", func() {
			resp := app.do(app.req(http.MethodPost,
				"/api/v1/library/path-migration", app.adminKey,
				strings.NewReader(
					`{"root":"downloads","to":"/data/dl","move_files":true}`,
				)))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("refuses a non-admin caller", func() {
			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/path-migration", app.requestOnlyKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})
