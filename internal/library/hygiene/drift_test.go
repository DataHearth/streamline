package hygiene

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
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

	It("bumps last_seen_at when the file is present on disk", func() {
		path := filepath.Join(tmpDir, "Inception.mkv")
		Expect(os.WriteFile(path, []byte("data"), 0o644)).To(Succeed())

		seen := time.Now().Add(-2 * time.Hour)
		rows := []*ent.MediaFile{{
			ID:         42,
			Path:       path,
			LastSeenAt: &seen,
		}}
		store.EXPECT().ListAllMediaFilesWithMovie(mock.Anything).
			Return(rows, nil).Once()
		store.EXPECT().BumpMediaFileLastSeen(mock.Anything, uint32(42)).
			Return(nil).Once()

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	It(
		"starts the grace clock when last_seen_at is NULL and the file is missing",
		func() {
			rows := []*ent.MediaFile{{
				ID:   7,
				Path: filepath.Join(tmpDir, "Gone.mkv"),
			}}
			store.EXPECT().
				ListAllMediaFilesWithMovie(mock.Anything).
				Return(rows, nil).
				Once()
			store.EXPECT().
				BumpMediaFileLastSeen(mock.Anything, uint32(7)).
				Return(nil).
				Once()

			Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
		},
	)

	It("no-ops while still within the grace window", func() {
		seen := time.Now().Add(-30 * time.Minute) // grace = 15m × 3 = 45m
		rows := []*ent.MediaFile{{
			ID:         11,
			Path:       filepath.Join(tmpDir, "Gone.mkv"),
			LastSeenAt: &seen,
		}}
		store.EXPECT().
			ListAllMediaFilesWithMovie(mock.Anything).
			Return(rows, nil).
			Once()

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})

	It("reverts the movie when grace expires and the file is missing", func() {
		seen := time.Now().Add(-2 * time.Hour)
		rows := []*ent.MediaFile{{
			ID:         99,
			Path:       filepath.Join(tmpDir, "Gone.mkv"),
			LastSeenAt: &seen,
			Edges: ent.MediaFileEdges{
				Movie: &ent.Movie{ID: 88, Title: "Gone", TmdbID: 1234},
			},
		}}
		store.EXPECT().
			ListAllMediaFilesWithMovie(mock.Anything).
			Return(rows, nil).
			Once()
		store.EXPECT().
			DeleteMediaFileAndRevertMovie(mock.Anything, uint32(99), uint32(88)).
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
		rows := []*ent.MediaFile{{
			ID: 5, Path: file, LastSeenAt: &seen,
		}}
		store.EXPECT().
			ListAllMediaFilesWithMovie(mock.Anything).
			Return(rows, nil).
			Once()

		Expect(svc.RunDriftCheck(ctx, 15*time.Minute)).To(Succeed())
	})
})
