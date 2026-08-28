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
	libmocks "github.com/datahearth/streamline/internal/library/mocks"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
)

var _ = Describe("Service.RunDriftCheck", Label("unit", "hygiene"), func() {
	var (
		ctx    context.Context
		tmpDir string
		store  *dbmocks.MockStore
		meta   *metamocks.MockProvider
		imp    *libmocks.MockImporter
		svc    *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		tmpDir = GinkgoT().TempDir()
		store = dbmocks.NewMockStore(GinkgoT())
		meta = metamocks.NewMockProvider(GinkgoT())
		imp = libmocks.NewMockImporter(GinkgoT())
		svc = New(
			store,
			meta,
			metamocks.NewMockTVProvider(GinkgoT()),
			imp,
			&config.LibraryConfig{
				MoviePath:       tmpDir,
				DriftGraceTicks: 3,
			},
		)
	})

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

	It("bumps last_seen_at for present files in one batch", func() {
		seen := time.Now().Add(-2 * time.Hour)
		rows := make([]db.DriftRow, 0, 2)
		for _, id := range []uint32{42, 43} {
			path := filepath.Join(tmpDir, fmt.Sprintf("Inception-%d.mkv", id))
			Expect(os.WriteFile(path, []byte("data"), 0o644)).To(Succeed())
			rows = append(rows, db.DriftRow{ID: id, Path: path, LastSeenAt: &seen})
		}
		pages(rows...)
		store.EXPECT().
			BumpMediaFilesLastSeen(mock.Anything, []uint32{42, 43}).
			Return(nil).Once()

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	It("never loads owner edges for a file that is present", func() {
		// The walk is a three-column projection; the owners cost an extra
		// query and are only worth it once a file has actually gone. No
		// FindMediaFileWithOwners expectation — the mock fails on any call.
		path := filepath.Join(tmpDir, "Present.mkv")
		Expect(os.WriteFile(path, []byte("data"), 0o644)).To(Succeed())
		seen := time.Now().Add(-2 * time.Hour)
		pages(db.DriftRow{ID: 5, Path: path, LastSeenAt: &seen})
		store.EXPECT().
			BumpMediaFilesLastSeen(mock.Anything, []uint32{5}).
			Return(nil).Once()

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	It(
		"starts the grace clock when last_seen_at is NULL and the file is missing",
		func() {
			path := filepath.Join(tmpDir, "Gone.mkv")
			pages(db.DriftRow{ID: 7, Path: path})
			store.EXPECT().
				FindMediaFileWithOwners(mock.Anything, uint32(7)).
				Return(&ent.MediaFile{ID: 7, Path: path}, nil).
				Once()
			store.EXPECT().
				MarkMediaFileMissing(mock.Anything, uint32(7)).
				Return(true, nil).
				Once()
			store.EXPECT().
				StartMediaFileGraceClock(mock.Anything, uint32(7)).
				Return(nil).
				Once()

			Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
		},
	)

	It("no-ops while still within the grace window", func() {
		seen := time.Now().Add(-30 * time.Minute) // grace = 15m × 3 = 45m
		path := filepath.Join(tmpDir, "Gone.mkv")
		pages(db.DriftRow{ID: 11, Path: path, LastSeenAt: &seen})
		store.EXPECT().
			FindMediaFileWithOwners(mock.Anything, uint32(11)).
			Return(&ent.MediaFile{ID: 11, Path: path, LastSeenAt: &seen}, nil).
			Once()
		store.EXPECT().
			MarkMediaFileMissing(mock.Anything, uint32(11)).
			Return(true, nil).
			Once()

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	It("reverts the movie when grace expires and the file is missing", func() {
		seen := time.Now().Add(-2 * time.Hour)
		path := filepath.Join(tmpDir, "Gone.mkv")
		pages(db.DriftRow{ID: 99, Path: path, LastSeenAt: &seen})
		store.EXPECT().
			FindMediaFileWithOwners(mock.Anything, uint32(99)).
			Return(&ent.MediaFile{
				ID: 99, Path: path, LastSeenAt: &seen,
				Edges: ent.MediaFileEdges{
					Movie: &ent.Movie{ID: 88, Title: "Gone", TmdbID: 1234},
				},
			}, nil).
			Once()
		store.EXPECT().
			MarkMediaFileMissing(mock.Anything, uint32(99)).
			Return(true, nil).
			Once()
		store.EXPECT().
			DeleteMediaFileAndRevertMovie(mock.Anything, uint32(99), uint32(88)).
			Return(nil).
			Once()

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	It("reverts the episode when grace expires and the file is missing", func() {
		seen := time.Now().Add(-2 * time.Hour)
		path := filepath.Join(tmpDir, "S01E01.mkv")
		pages(db.DriftRow{ID: 21, Path: path, LastSeenAt: &seen})
		store.EXPECT().
			FindMediaFileWithOwners(mock.Anything, uint32(21)).
			Return(&ent.MediaFile{
				ID: 21, Path: path, LastSeenAt: &seen,
				Edges: ent.MediaFileEdges{
					Episode: &ent.Episode{ID: 33, Number: 1},
				},
			}, nil).
			Once()
		store.EXPECT().
			MarkMediaFileMissing(mock.Anything, uint32(21)).
			Return(true, nil).
			Once()
		store.EXPECT().
			DeleteMediaFileAndRevertEpisode(mock.Anything, uint32(21), uint32(33)).
			Return(nil).
			Once()

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	It("deletes an ownerless row instead of warning about it forever", func() {
		seen := time.Now().Add(-2 * time.Hour)
		path := filepath.Join(tmpDir, "Orphan.mkv")
		pages(db.DriftRow{ID: 64, Path: path, LastSeenAt: &seen})
		store.EXPECT().
			FindMediaFileWithOwners(mock.Anything, uint32(64)).
			Return(&ent.MediaFile{ID: 64, Path: path, LastSeenAt: &seen}, nil).
			Once()
		store.EXPECT().
			MarkMediaFileMissing(mock.Anything, uint32(64)).
			Return(true, nil).
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
		pages(db.DriftRow{ID: 5, Path: file, LastSeenAt: &seen})

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})
})
