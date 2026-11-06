package hygiene

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Service.RunDriftCheck", Label("unit", "hygiene"), func() {
	var (
		ctx    context.Context
		tmpDir string
		store  *dbmocks.MockStore
		meta   *metamocks.MockProvider
		svc    *Service
		// replaced is what the sweep's last query answers.
		replaced []uint32
	)

	BeforeEach(func() {
		ctx = context.Background()
		tmpDir = GinkgoT().TempDir()
		replaced = nil
		configtest.Setup(map[string]any{"library": map[string]any{
			"missing_file_redownload_after": "0",
		}})
		store = dbmocks.NewMockStore(GinkgoT())
		store.EXPECT().ListReplacedMissingFiles(mock.Anything).
			RunAndReturn(func(context.Context, ...db.LibraryRoot) ([]uint32, error) {
				return replaced, nil
			}).Maybe()
		meta = metamocks.NewMockProvider(GinkgoT())
		svc = New(
			store,
			meta,
			metamocks.NewMockTVProvider(GinkgoT()),
			&config.LibraryConfig{
				MoviePath:       tmpDir,
				DriftGraceTicks: 3,
			},
		)
	})

	// removed scripts what a revert asks of the store before it reverts.
	removed := func(id uint32) {
		GinkgoHelper()
		store.EXPECT().
			ConfirmMediaFileMissing(mock.Anything, id).
			Return(nil).
			Once()
		store.EXPECT().
			HoldsOtherFile(mock.Anything, mock.Anything).
			Return(false, nil).
			Once()
	}

	// pages scripts the keyset walk: one page of rows, then an empty page to
	// end the loop. Every spec here fits in a single page.
	pages := func(rows ...db.DriftRow) {
		GinkgoHelper()
		store.EXPECT().
			ListMediaFilesForDrift(mock.Anything, uint32(0), driftPageSize).
			Return(rows, nil).Once()
		if len(rows) > 0 {
			last := rows[len(rows)-1].ID
			store.EXPECT().
				ListMediaFilesForDrift(mock.Anything, last, driftPageSize).
				Return(nil, nil).Once()
		}
	}

	It("writes nothing for files that are present and were never missing", func() {
		// No write and no FindMediaFileWithOwners expectation: the mock fails
		// on any call.
		rows := make([]db.DriftRow, 0, 2)
		for _, id := range []uint32{42, 43} {
			path := filepath.Join(tmpDir, fmt.Sprintf("Inception-%d.mkv", id))
			Expect(os.WriteFile(path, []byte("data"), 0o644)).To(Succeed())
			rows = append(rows, db.DriftRow{ID: id, Path: path})
		}
		pages(rows...)

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	It("clears missing_since only on files that came back", func() {
		since := time.Now().Add(-30 * time.Minute)
		back := filepath.Join(tmpDir, "Back.mkv")
		steady := filepath.Join(tmpDir, "Steady.mkv")
		Expect(os.WriteFile(back, []byte("data"), 0o644)).To(Succeed())
		Expect(os.WriteFile(steady, []byte("data"), 0o644)).To(Succeed())
		pages(
			db.DriftRow{ID: 5, Path: back, MissingSince: &since},
			db.DriftRow{ID: 6, Path: steady},
		)
		store.EXPECT().
			ClearMediaFilesMissing(mock.Anything, []uint32{5}).
			Return(nil).Once()

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	It("starts the grace clock on the first tick that finds a file gone", func() {
		// No delete expectation: however long the file has really been gone,
		// a first sighting never reverts anything.
		path := filepath.Join(tmpDir, "Gone.mkv")
		pages(db.DriftRow{ID: 7, Path: path})
		store.EXPECT().
			FindMediaFileWithOwners(mock.Anything, uint32(7)).
			Return(&ent.MediaFile{
				ID: 7, Path: path,
				Edges: ent.MediaFileEdges{Movie: &ent.Movie{ID: 88}},
			}, nil).
			Once()
		store.EXPECT().
			MarkMediaFileMissing(mock.Anything, uint32(7)).
			Return(true, nil).
			Once()

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	It("no-ops while still within the grace window", func() {
		seen := time.Now().Add(-30 * time.Minute) // grace = 15m × 3 = 45m
		path := filepath.Join(tmpDir, "Gone.mkv")
		pages(db.DriftRow{ID: 11, Path: path, MissingSince: &seen})
		store.EXPECT().
			FindMediaFileWithOwners(mock.Anything, uint32(11)).
			Return(&ent.MediaFile{ID: 11, Path: path, MissingSince: &seen}, nil).
			Once()
		store.EXPECT().
			MarkMediaFileMissing(mock.Anything, uint32(11)).
			Return(false, nil).
			Once()

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	It("loads no owner for a confirmed file inside its wait", func() {
		configtest.Setup(map[string]any{"library": map[string]any{
			"missing_file_redownload_after": "168h",
		}})
		seen := time.Now().Add(-2 * time.Hour)
		confirmed := time.Now().Add(-time.Hour)
		// No expectations past the walk: the mock fails on any other call.
		pages(db.DriftRow{
			ID: 12, Path: filepath.Join(tmpDir, "Gone.mkv"),
			MissingSince: &seen, MissingConfirmedAt: &confirmed,
		})

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	DescribeTable("drops a row its owner has replaced, whatever the wait",
		func(wait string) {
			configtest.Setup(map[string]any{"library": map[string]any{
				"missing_file_redownload_after": wait,
			}})
			replaced = []uint32{9}
			pages()
			store.EXPECT().FindMediaFileWithOwners(mock.Anything, uint32(9)).
				Return(&ent.MediaFile{
					ID: 9, Path: filepath.Join(tmpDir, "Old.mkv"),
					Edges: ent.MediaFileEdges{Movie: &ent.Movie{ID: 88}},
				}, nil).Once()
			// Only the row: DeleteMediaFile is the one write that moves no status.
			store.EXPECT().
				DeleteMediaFile(mock.Anything, uint32(9)).
				Return(nil).
				Once()

			Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
		},
		Entry("under off", "off"),
		Entry("inside the wait", "168h"),
	)

	It("reverts the movie when grace expires and the file is missing", func() {
		seen := time.Now().Add(-2 * time.Hour)
		path := filepath.Join(tmpDir, "Gone.mkv")
		pages(db.DriftRow{ID: 99, Path: path, MissingSince: &seen})
		store.EXPECT().
			FindMediaFileWithOwners(mock.Anything, uint32(99)).
			Return(&ent.MediaFile{
				ID: 99, Path: path, MissingSince: &seen,
				Edges: ent.MediaFileEdges{
					Movie: &ent.Movie{ID: 88, Title: "Gone", TmdbID: 1234},
				},
			}, nil).
			Once()
		store.EXPECT().
			MarkMediaFileMissing(mock.Anything, uint32(99)).
			Return(false, nil).
			Once()
		removed(99)
		store.EXPECT().
			DeleteMediaFileAndRevertMovie(mock.Anything, uint32(99), uint32(88)).
			Return(nil).
			Once()

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	// episodeFile builds the owner shape FindMediaFileWithOwners returns for an
	// episode-owned row: the episode with its season and show attached.
	episodeFile := func(
		id, episodeID, showID uint32,
		season uint16,
		path string,
		seen *time.Time,
	) *ent.MediaFile {
		return &ent.MediaFile{
			ID: id, Path: path, MissingSince: seen,
			Edges: ent.MediaFileEdges{
				Episode: &ent.Episode{
					ID: episodeID, Number: 1,
					Edges: ent.EpisodeEdges{
						Season: &ent.Season{
							Number: season,
							Edges: ent.SeasonEdges{
								TvShow: &ent.TVShow{ID: showID, Title: "Gone Show"},
							},
						},
					},
				},
			},
		}
	}

	It("reverts the episode when grace expires and the file is missing", func() {
		seen := time.Now().Add(-2 * time.Hour)
		path := filepath.Join(tmpDir, "S01E01.mkv")
		pages(db.DriftRow{ID: 21, Path: path, MissingSince: &seen})
		store.EXPECT().
			FindMediaFileWithOwners(mock.Anything, uint32(21)).
			Return(episodeFile(21, 33, 55, 1, path, &seen), nil).
			Once()
		store.EXPECT().
			MarkMediaFileMissing(mock.Anything, uint32(21)).
			Return(false, nil).
			Once()
		removed(21)
		store.EXPECT().
			DeleteMediaFileAndRevertEpisode(mock.Anything, uint32(21), uint32(33)).
			Return(nil).
			Once()

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	It("reverts every episode of a show whose folder disappeared", func() {
		seen := time.Now().Add(-2 * time.Hour)
		rows := make([]db.DriftRow, 0, 3)
		for i, id := range []uint32{21, 22, 23} {
			path := filepath.Join(tmpDir, fmt.Sprintf("S01E0%d.mkv", i+1))
			rows = append(rows, db.DriftRow{ID: id, Path: path, MissingSince: &seen})
			store.EXPECT().
				FindMediaFileWithOwners(mock.Anything, id).
				Return(episodeFile(id, id+100, 55, 1, path, &seen), nil).
				Once()
			store.EXPECT().
				MarkMediaFileMissing(mock.Anything, id).
				Return(false, nil).
				Once()
			removed(id)
			store.EXPECT().
				DeleteMediaFileAndRevertEpisode(mock.Anything, id, id+100).
				Return(nil).
				Once()
		}
		pages(rows...)

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	It("deletes an ownerless row instead of warning about it forever", func() {
		seen := time.Now().Add(-2 * time.Hour)
		path := filepath.Join(tmpDir, "Orphan.mkv")
		pages(db.DriftRow{ID: 64, Path: path, MissingSince: &seen})
		store.EXPECT().
			FindMediaFileWithOwners(mock.Anything, uint32(64)).
			Return(&ent.MediaFile{ID: 64, Path: path, MissingSince: &seen}, nil).
			Once()
		store.EXPECT().
			MarkMediaFileMissing(mock.Anything, uint32(64)).
			Return(false, nil).
			Once()
		store.EXPECT().
			DeleteMediaFile(mock.Anything, uint32(64)).
			Return(nil).
			Once()

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	It("skips reverting when stat returns a permission error", func() {
		if os.Geteuid() == 0 {
			Skip("not relevant as root")
		}
		locked := filepath.Join(tmpDir, "locked")
		Expect(os.Mkdir(locked, 0o755)).To(Succeed())
		file := filepath.Join(locked, "Gone.mkv")
		Expect(os.WriteFile(file, []byte("x"), 0o644)).To(Succeed())
		Expect(os.Chmod(locked, 0o000)).To(Succeed())
		DeferCleanup(func() { _ = os.Chmod(locked, 0o755) })

		seen := time.Now().Add(-2 * time.Hour)
		pages(db.DriftRow{ID: 5, Path: file, MissingSince: &seen})

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})
})
