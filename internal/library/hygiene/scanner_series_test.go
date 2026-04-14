package hygiene

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	libmocks "github.com/datahearth/streamline/internal/library/mocks"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
)

var _ = Describe("Service.RunSeriesOrphanScan", Label("unit", "hygiene"), func() {
	var (
		ctx    context.Context
		tmpDir string
		store  *dbmocks.MockStore
		meta   *metamocks.MockProvider
		tvmeta *metamocks.MockTVProvider
		imp    *libmocks.MockImporter
		svc    *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		tmpDir = GinkgoT().TempDir()
		store = dbmocks.NewMockStore(GinkgoT())
		meta = metamocks.NewMockProvider(GinkgoT())
		tvmeta = metamocks.NewMockTVProvider(GinkgoT())
		imp = libmocks.NewMockImporter(GinkgoT())
		svc = New(store, meta, tvmeta, imp, &config.LibraryConfig{
			SeriesPath: tmpDir,
			ImportMode: "hardlink",
		})
	})

	// placeShowFile writes a > MinMediaSize video file inside a show folder and
	// returns its full path.
	placeShowFile := func(show, file string) string {
		dir := filepath.Join(tmpDir, show)
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		path := filepath.Join(dir, file)
		Expect(os.WriteFile(path, make([]byte, 60*1024*1024), 0o644)).To(Succeed())
		return path
	}

	// expectScanPrelude sets the three list lookups every scan performs before
	// walking folders.
	expectScanPrelude := func(pending []string) {
		store.EXPECT().
			ListAllEpisodeMediaFilePaths(mock.Anything).
			Return(nil, nil).
			Once()
		store.EXPECT().
			ListPendingImportScanShowFolders(mock.Anything).
			Return(pending, nil).
			Once()
		store.EXPECT().ListTvShowsForAdoption(mock.Anything).Return(nil, nil).Once()
	}

	It("classifies an untracked show folder and queues it once", func() {
		placeShowFile("Breaking Bad", "Breaking Bad S01E01.mkv")
		placeShowFile("Breaking Bad", "Breaking Bad S01E02.mkv")

		expectScanPrelude(nil)
		tvmeta.EXPECT().SearchSeries(mock.Anything, "Breaking Bad").
			Return([]metadata.TVResult{
				{TVDBID: 81189, Title: "Breaking Bad", Year: 2008},
			}, nil).Once()
		store.EXPECT().FindOpenImportScanForSource(mock.Anything, tmpDir).
			Return(nil, &ent.NotFoundError{}).Once()
		store.EXPECT().CreateImportScan(mock.Anything, mock.Anything).
			Return(&ent.ImportScan{ID: 1}, nil).Once()
		store.EXPECT().
			UpdateImportScanStatus(mock.Anything, uint32(1), mock.Anything, mock.Anything).
			Return(nil).
			Once()
		store.EXPECT().
			BulkCreateImportScanShows(mock.Anything, uint32(1), mock.MatchedBy(
				func(shows []db.CreateImportScanShowParams) bool {
					return len(shows) == 1 && shows[0].FileCount == 2
				},
			),
			).
			Return(nil).
			Once()

		Expect(svc.RunSeriesOrphanScan(ctx)).To(Succeed())
	})

	It("skips a folder already queued for review", func() {
		folder := filepath.Dir(
			placeShowFile("Breaking Bad", "Breaking Bad S01E01.mkv"),
		)

		expectScanPrelude([]string{folder})
		// No SearchSeries, no scan writes.

		Expect(svc.RunSeriesOrphanScan(ctx)).To(Succeed())
	})

	It("skips a folder whose files are all already tracked", func() {
		path := placeShowFile("Breaking Bad", "Breaking Bad S01E01.mkv")

		store.EXPECT().ListAllEpisodeMediaFilePaths(mock.Anything).
			Return([]string{path}, nil).Once()
		store.EXPECT().
			ListPendingImportScanShowFolders(mock.Anything).
			Return(nil, nil).
			Once()
		store.EXPECT().ListTvShowsForAdoption(mock.Anything).Return(nil, nil).Once()
		// No SearchSeries, no scan writes.

		Expect(svc.RunSeriesOrphanScan(ctx)).To(Succeed())
	})

	It("skips cleanly when series_path does not exist", func() {
		svc = New(store, meta, tvmeta, imp, &config.LibraryConfig{
			SeriesPath: filepath.Join(tmpDir, "nope"),
			ImportMode: "hardlink",
		})
		// No store/meta calls — early return before any I/O.

		Expect(svc.RunSeriesOrphanScan(ctx)).To(Succeed())
	})
})
