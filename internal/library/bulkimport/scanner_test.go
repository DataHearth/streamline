package bulkimport

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	metadatamocks "github.com/datahearth/streamline/internal/metadata/mocks"
)

func writeFile(path string, sizeMB int) {
	GinkgoHelper()
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	f, err := os.Create(path)
	Expect(err).ToNot(HaveOccurred())
	defer f.Close()
	Expect(f.Truncate(int64(sizeMB) * 1024 * 1024)).To(Succeed())
}

var _ = Describe(
	"Service.StartScan validation",
	Label("unit", "bulkimport"),
	func() {
		var (
			ctx      context.Context
			store    *dbmocks.MockStore
			metaProv *metadatamocks.MockProvider
			svc      *Service
			tmpDir   string
			libRoot  string
		)

		BeforeEach(func() {
			ctx = context.Background()
			store = dbmocks.NewMockStore(GinkgoT())
			metaProv = metadatamocks.NewMockProvider(GinkgoT())
			tmpDir = GinkgoT().TempDir()
			libRoot = tmpDir
			svc = NewService(
				store,
				metaProv,
				nil,
				nil,
				nil,
				nil,
				nil,
				libRoot,
				libRoot,
				nil, nil, nil, nil,
			)
		})

		It("rejects relative path", func() {
			_, err := svc.StartScan(
				ctx,
				StartScanParams{
					SourcePath: "relative/path",
					Mode:       entimportscan.ModeInPlace,
				},
			)
			Expect(err).To(MatchError(ErrInvalidPath))
		})

		It("runs BeforeCreate only once every check has passed", func() {
			called := false
			hook := func(context.Context) error { called = true; return nil }

			_, err := svc.StartScan(ctx, StartScanParams{
				SourcePath: "relative/path", Mode: entimportscan.ModeInPlace,
				BeforeCreate: hook,
			})
			Expect(err).To(MatchError(ErrInvalidPath))
			Expect(called).To(BeFalse())

			store.EXPECT().CountActiveImportScans(mock.Anything).
				Return(1, nil).Once()
			_, err = svc.StartScan(ctx, StartScanParams{
				SourcePath: libRoot, Mode: entimportscan.ModeInPlace,
				BeforeCreate: hook,
			})
			Expect(err).To(MatchError(ErrScanRunning))
			Expect(called).To(BeFalse())
		})

		It("refuses the start with BeforeCreate's error and creates no scan",
			func() {
				store.EXPECT().CountActiveImportScans(mock.Anything).
					Return(0, nil).Once()
				boom := errors.New("config is read-only")
				// No CreateImportScan expectation: the mock fails the spec if
				// the scan row is written anyway.
				_, err := svc.StartScan(ctx, StartScanParams{
					SourcePath: libRoot, Mode: entimportscan.ModeInPlace,
					BeforeCreate: func(context.Context) error { return boom },
				})
				Expect(err).To(MatchError(boom))
			})

		It("rejects nonexistent path", func() {
			_, err := svc.StartScan(
				ctx,
				StartScanParams{
					SourcePath: "/nonexistent/abs/path",
					Mode:       entimportscan.ModeInPlace,
				},
			)
			Expect(err).To(MatchError(ErrInvalidPath))
		})

		It("rejects file (not a directory)", func() {
			file := filepath.Join(tmpDir, "f.mkv")
			writeFile(file, 60)
			_, err := svc.StartScan(
				ctx,
				StartScanParams{SourcePath: file, Mode: entimportscan.ModeInPlace},
			)
			Expect(err).To(MatchError(ErrInvalidPath))
		})

		It("distinguishes a missing library root from a bad source path", func() {
			svc = NewService(
				store, metaProv, nil, nil, nil, nil, nil,
				"/nonexistent/library/root", "/nonexistent/library/root",
				nil, nil, nil, nil,
			)
			_, err := svc.StartScan(
				ctx,
				StartScanParams{SourcePath: tmpDir, Mode: entimportscan.ModeInPlace},
			)
			Expect(err).To(MatchError(ErrLibraryPathMissing))
			Expect(err).ToNot(MatchError(ErrInvalidPath))
		})

		It("validates a series scan against series_path, not movie_path", func() {
			seriesRoot := GinkgoT().TempDir()
			svc = NewService(
				store, metaProv, nil, nil, nil, nil, nil,
				"/nonexistent/movie/root", seriesRoot,
				nil, nil, nil, nil,
			)
			store.EXPECT().
				CountActiveImportScans(mock.Anything).
				Return(1, nil).
				Once()
			_, err := svc.StartScan(ctx, StartScanParams{
				SourcePath: seriesRoot,
				Kind:       entimportscan.KindSeries,
				Mode:       entimportscan.ModeInPlace,
			})
			Expect(err).To(MatchError(ErrScanRunning))
		})

		It("rejects path outside library in in_place mode", func() {
			outside := GinkgoT().TempDir()
			_, err := svc.StartScan(
				ctx,
				StartScanParams{
					SourcePath: outside,
					Mode:       entimportscan.ModeInPlace,
				},
			)
			Expect(err).To(MatchError(ErrPathOutsideLibrary))
		})

		It("rejects path inside library in rename mode", func() {
			inside := filepath.Join(libRoot, "subdir")
			Expect(os.MkdirAll(inside, 0o755)).To(Succeed())
			_, err := svc.StartScan(
				ctx,
				StartScanParams{SourcePath: inside, Mode: entimportscan.ModeRename},
			)
			Expect(err).To(MatchError(ErrPathOutsideLibrary))
		})

		It("rejects when another scan is already active", func() {
			store.EXPECT().
				CountActiveImportScans(mock.Anything).
				Return(1, nil).
				Once()
			_, err := svc.StartScan(
				ctx,
				StartScanParams{
					SourcePath: libRoot,
					Mode:       entimportscan.ModeInPlace,
				},
			)
			Expect(err).To(MatchError(ErrScanRunning))
		})

		It("creates the scan row when validation passes", func() {
			store.EXPECT().
				CountActiveImportScans(mock.Anything).
				Return(0, nil).
				Once()
			store.EXPECT().CreateImportScan(mock.Anything, mock.Anything).
				Return(&ent.ImportScan{ID: 1, SourcePath: libRoot, Mode: entimportscan.ModeInPlace, Status: entimportscan.StatusRunning}, nil).
				Once()
			// runScan fires async; allow any subsequent store/metadata calls.
			store.EXPECT().
				UpdateImportScanStatus(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Return(nil).
				Maybe()
			store.EXPECT().
				MovieTMDBIndex(mock.Anything).
				Return(nil, nil).
				Maybe()
			store.EXPECT().
				FindImportScan(mock.Anything, mock.Anything).
				Return(nil, nil).
				Maybe()
			store.EXPECT().
				BulkCreateImportScanFiles(mock.Anything, mock.Anything, mock.Anything).
				Return(nil).
				Maybe()
			store.EXPECT().
				IncrementImportScanProgress(mock.Anything, mock.Anything, mock.Anything).
				Return(nil).
				Maybe()
			metaProv.EXPECT().
				SearchMovie(mock.Anything, mock.Anything, mock.Anything).
				Return(nil, nil).
				Maybe()

			scan, err := svc.StartScan(
				ctx,
				StartScanParams{
					SourcePath: libRoot,
					Mode:       entimportscan.ModeInPlace,
				},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(scan.ID).To(Equal(uint32(1)))
		})
	},
)

var _ = Describe("storedSourceURL", Label("unit", "bulkimport"), func() {
	It("keeps the address and drops what can carry a credential", func() {
		Expect(storedSourceURL(
			"https://admin:s3cret@radarr.example.com/radarr?apikey=K#top",
		)).To(Equal("https://radarr.example.com/radarr"))
		Expect(storedSourceURL("http://radarr.lan:7878")).
			To(Equal("http://radarr.lan:7878"))
	})
})
