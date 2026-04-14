package hygiene

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/library"
	libmocks "github.com/datahearth/streamline/internal/library/mocks"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
)

var _ = Describe("Service.RunOrphanScan", Label("unit", "hygiene"), func() {
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
				MoviePath:  tmpDir,
				ImportMode: "hardlink",
			},
		)
	})

	placeOrphan := func(rel string) string {
		path := filepath.Join(tmpDir, rel)
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		// 60 MiB → above library.MinMediaSize (50 MiB).
		Expect(os.WriteFile(path, make([]byte, 60*1024*1024), 0o644)).To(Succeed())
		return path
	}

	expectQueueWriteOnce := func(scanID uint32) {
		store.EXPECT().
			FindOpenImportScanForSource(mock.Anything, mock.Anything).
			Return(nil, &ent.NotFoundError{}).Once()
		store.EXPECT().CreateImportScan(mock.Anything, mock.Anything).
			Return(&ent.ImportScan{ID: scanID}, nil).Once()
		store.EXPECT().UpdateImportScanStatus(
			mock.Anything, scanID, mock.Anything, mock.Anything,
		).Return(nil).Once()
		store.EXPECT().
			BulkCreateImportScanFiles(mock.Anything, scanID, mock.Anything).
			Return(nil).
			Once()
	}

	It("auto-imports a high-confidence orphan into an existing movie row", func() {
		path := placeOrphan("Inception.2010.1080p.BluRay.x264-GROUP.mkv")

		store.EXPECT().ListAllMediaFilesWithMovie(mock.Anything).
			Return(nil, nil).Once()
		store.EXPECT().ListPendingImportScanFilePaths(mock.Anything).
			Return(nil, nil).Once()
		meta.EXPECT().SearchMovie(mock.Anything, "Inception", uint16(2010)).
			Return([]metadata.MovieResult{
				{TMDBID: 27205, Title: "Inception", Year: 2010},
			}, nil).Once()
		store.EXPECT().MovieHasMediaFile(mock.Anything, uint32(27205)).
			Return(false, nil).Once()
		store.EXPECT().FindMovieByTMDBID(mock.Anything, uint32(27205)).
			Return(&ent.Movie{ID: 1, Title: "Inception", Year: 2010, TmdbID: 27205}, nil).
			Once()
		imp.EXPECT().ImportMovieWithMode(
			mock.Anything,
			filepath.Dir(path),
			mock.MatchedBy(func(m *ent.Movie) bool { return m.TmdbID == 27205 }),
			"", "",
		).Return(library.ImportedFile{Path: path, Size: 60 * 1024 * 1024}, nil).Once()
		store.EXPECT().CreateMediaFile(mock.Anything, mock.MatchedBy(
			func(p db.CreateMediaFileParams) bool {
				return p.MovieID == 1 && p.Path == path
			},
		)).Return(&ent.MediaFile{ID: 10, Path: path}, nil).Once()
		store.EXPECT().UpdateMovieStatus(mock.Anything, uint32(1), mock.Anything).
			Return(nil).Once()

		Expect(svc.RunOrphanScan(ctx)).To(Succeed())
	})

	It("queues a confirmed orphan for review when no movie row exists yet", func() {
		placeOrphan("Inception.2010.1080p.BluRay.x264-GROUP.mkv")

		store.EXPECT().ListAllMediaFilesWithMovie(mock.Anything).
			Return(nil, nil).Once()
		store.EXPECT().ListPendingImportScanFilePaths(mock.Anything).
			Return(nil, nil).Once()
		meta.EXPECT().SearchMovie(mock.Anything, "Inception", uint16(2010)).
			Return([]metadata.MovieResult{
				{TMDBID: 27205, Title: "Inception", Year: 2010},
			}, nil).Once()
		store.EXPECT().MovieHasMediaFile(mock.Anything, uint32(27205)).
			Return(false, nil).Once()
		store.EXPECT().FindMovieByTMDBID(mock.Anything, uint32(27205)).
			Return(nil, &ent.NotFoundError{}).Once()
		expectQueueWriteOnce(98)

		Expect(svc.RunOrphanScan(ctx)).To(Succeed())
	})

	It("queues an ambiguous orphan into ImportScan instead of importing", func() {
		placeOrphan("Some Movie 2010.1080p.x264.mkv")
		store.EXPECT().
			ListAllMediaFilesWithMovie(mock.Anything).
			Return(nil, nil).
			Once()
		store.EXPECT().
			ListPendingImportScanFilePaths(mock.Anything).
			Return(nil, nil).
			Once()
		meta.EXPECT().SearchMovie(mock.Anything, "Some Movie", uint16(2010)).
			Return([]metadata.MovieResult{
				{TMDBID: 1, Title: "Some Other Movie", Year: 2010},
				{TMDBID: 2, Title: "Some Movie 2", Year: 2010},
			}, nil).Once()
		expectQueueWriteOnce(99)

		Expect(svc.RunOrphanScan(ctx)).To(Succeed())
	})

	It("queues an unmatched orphan", func() {
		placeOrphan("garbled.unknown.file.mkv")
		store.EXPECT().
			ListAllMediaFilesWithMovie(mock.Anything).
			Return(nil, nil).
			Once()
		store.EXPECT().
			ListPendingImportScanFilePaths(mock.Anything).
			Return(nil, nil).
			Once()
		meta.EXPECT().SearchMovie(mock.Anything, mock.Anything, mock.Anything).
			Return(nil, nil).Once()
		expectQueueWriteOnce(100)

		Expect(svc.RunOrphanScan(ctx)).To(Succeed())
	})

	It("skips orphans whose path is already in a pending ImportScanFile", func() {
		p := placeOrphan("Some Movie 2010.1080p.x264.mkv")
		store.EXPECT().
			ListAllMediaFilesWithMovie(mock.Anything).
			Return(nil, nil).
			Once()
		store.EXPECT().ListPendingImportScanFilePaths(mock.Anything).
			Return([]string{p}, nil).Once()
		// No SearchMovie, no queue writes, no Importer.

		Expect(svc.RunOrphanScan(ctx)).To(Succeed())
	})

	It("skips cleanly when the configured library path does not exist", func() {
		svc = New(
			store,
			meta,
			metamocks.NewMockTVProvider(GinkgoT()),
			imp,
			&config.LibraryConfig{
				MoviePath:  filepath.Join(tmpDir, "does-not-exist"),
				ImportMode: "hardlink",
			},
		)
		// No store/meta/importer calls expected — early return before any DB I/O.

		Expect(svc.RunOrphanScan(ctx)).To(Succeed())
	})

	It("falls through to queue when auto-import fails on a confirmed match", func() {
		placeOrphan("Inception.2010.1080p.BluRay.x264-GROUP.mkv")
		store.EXPECT().
			ListAllMediaFilesWithMovie(mock.Anything).
			Return(nil, nil).
			Once()
		store.EXPECT().
			ListPendingImportScanFilePaths(mock.Anything).
			Return(nil, nil).
			Once()
		meta.EXPECT().SearchMovie(mock.Anything, "Inception", uint16(2010)).
			Return([]metadata.MovieResult{{TMDBID: 27205, Title: "Inception", Year: 2010}}, nil).
			Once()
		store.EXPECT().
			MovieHasMediaFile(mock.Anything, uint32(27205)).
			Return(false, nil).
			Once()
		store.EXPECT().FindMovieByTMDBID(mock.Anything, uint32(27205)).
			Return(&ent.Movie{ID: 1, TmdbID: 27205}, nil).Once()
		imp.EXPECT().ImportMovieWithMode(
			mock.Anything, mock.Anything, mock.Anything, "", "",
		).Return(library.ImportedFile{}, errors.New("disk full")).Once()
		expectQueueWriteOnce(101)

		Expect(svc.RunOrphanScan(ctx)).To(Succeed())
	})
})
