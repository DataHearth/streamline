package bulkimport

import (
	"context"
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
			svc = NewService(store, metaProv, nil, nil, nil, nil, libRoot)
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
				Return(uint32(1), nil).
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
				Return(uint32(0), nil).
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
				ListMovies(mock.Anything, mock.Anything, mock.Anything).
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
