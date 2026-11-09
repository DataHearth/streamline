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
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/tvshow"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe(
	"Series commit replacing a tracked episode file",
	Label("integration", "bulkimport"),
	func() {
		const tvdbID = uint32(81189)

		var (
			ctx    context.Context
			base   string
			libDir string
			store  db.Store
			svc    *Service
		)

		naming := func(template string) {
			configtest.Setup(map[string]any{
				"library": map[string]any{
					"series_path":   libDir,
					"series_naming": template,
					"import_mode":   "copy",
				},
			})
		}

		BeforeEach(func() {
			ctx = context.Background()
			base = GinkgoT().TempDir()
			libDir = filepath.Join(base, "lib")
			Expect(os.MkdirAll(libDir, 0o755)).To(Succeed())
			naming("{title}/Season {season:02}/" +
				"{title} - S{season:02}E{episode:02}.{ext}")

			client := dbtest.SetupTestDB(ctx)
			events.Register(client)
			DeferCleanup(client.Close)
			store = db.New(client)
			tvmeta := metamocks.NewMockTVProvider(GinkgoT())
			tvmeta.EXPECT().GetSeries(mock.Anything, tvdbID).
				Return(&metadata.TVDetails{
					TVDBID: tvdbID, Title: "Breaking Bad", Year: 2008,
					Status: "ended",
					Type:   metadata.SeriesStandard,
					Seasons: []metadata.SeasonInfo{
						{Number: 1, Name: "Season 1"},
					},
					Episodes: []metadata.EpisodeInfo{
						{SeasonNumber: 1, Number: 1, Title: "Pilot"},
					},
				}, nil).Once()
			tvmeta.EXPECT().GetSeriesCast(mock.Anything, tvdbID).
				Return(nil, nil).Once()
			svc = NewService(
				store, nil, tvmeta, library.NewImportService(), nil,
				tvshow.NewService(store, tvmeta, nil, nil, nil), nil,
				libDir, libDir,
				nil, nil, nil, nil,
			)
		})

		video := func(path, content string) {
			GinkgoHelper()
			Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
			body := make([]byte, 60*1024*1024)
			copy(body, content)
			Expect(os.WriteFile(path, body, 0o644)).To(Succeed())
		}

		// commit runs one show folder through a scan of the given mode: the
		// first call adds the show, later ones attach to it.
		commit := func(folder string, mode entimportscan.Mode, show *ent.TVShow) {
			GinkgoHelper()
			scan, err := store.CreateImportScan(ctx, db.CreateImportScanParams{
				SourcePath: filepath.Dir(folder),
				Kind:       entimportscan.KindSeries,
				Mode:       mode,
				ImportMode: entimportscan.ImportModeCopy,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(store.UpdateImportScanStatus(
				ctx, scan.ID, entimportscan.StatusAwaitingReview,
				db.UpdateScanStatusOpts{},
			)).To(Succeed())
			row := db.CreateImportScanShowParams{
				FolderPath:  folder,
				ParsedTitle: "Breaking Bad",
				FileCount:   1,
			}
			if show == nil {
				id := tvdbID
				row.Classification = entimportscanshow.ClassificationConfirmed
				row.TVDBID = &id
			} else {
				row.Classification = entimportscanshow.ClassificationExisting
				row.ExistingTvshowID = &show.ID
			}
			Expect(store.BulkCreateImportScanShows(
				ctx, scan.ID, []db.CreateImportScanShowParams{row},
			)).To(Succeed())
			svc.runCommitSeries(ctx, scan)
		}

		tracked := func() (*ent.TVShow, *ent.MediaFile) {
			GinkgoHelper()
			show, err := store.FindTVShowByTVDBID(ctx, tvdbID)
			Expect(err).NotTo(HaveOccurred())
			full, err := store.FindTVShowByID(ctx, show.ID)
			Expect(err).NotTo(HaveOccurred())
			mf, err := store.FindMediaFileByEpisodeID(ctx, episodeID(full))
			Expect(err).NotTo(HaveOccurred())
			return show, mf
		}

		It("keeps the tracked file and its row when the import fails", func() {
			first := filepath.Join(base, "src1", "Breaking Bad")
			video(filepath.Join(first, "Breaking Bad S01E01.mkv"), "old")
			commit(first, entimportscan.ModeRename, nil)
			show, before := tracked()

			// The template now renders elsewhere, and another file already
			// sits there: the second import is refused.
			naming("{title}/S{season:02}E{episode:02}.{ext}")
			video(filepath.Join(libDir, "Breaking Bad", "S01E01.mkv"), "foreign")
			second := filepath.Join(base, "src2", "Breaking Bad")
			video(filepath.Join(second, "Breaking Bad S01E01 REPACK.mkv"), "new")
			commit(second, entimportscan.ModeRename, show)

			_, after := tracked()
			Expect(after.ID).To(Equal(before.ID))
			Expect(after.Path).To(Equal(before.Path))
			body, err := os.ReadFile(before.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(body[:3])).To(Equal("old"))
			Expect(before.Path + replacedSuffix).NotTo(BeAnExistingFile())
		})

		It("replaces a tracked file sitting at the destination", func() {
			first := filepath.Join(base, "src1", "Breaking Bad")
			video(filepath.Join(first, "Breaking Bad S01E01.mkv"), "old")
			commit(first, entimportscan.ModeRename, nil)
			show, before := tracked()

			second := filepath.Join(base, "src2", "Breaking Bad")
			video(filepath.Join(second, "Breaking Bad S01E01 REPACK.mkv"), "new")
			commit(second, entimportscan.ModeRename, show)

			_, after := tracked()
			Expect(after.ID).NotTo(Equal(before.ID))
			Expect(after.Path).To(Equal(before.Path))
			body, err := os.ReadFile(after.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(body[:3])).To(Equal("new"))
			Expect(after.Path + replacedSuffix).NotTo(BeAnExistingFile())
		})

		It(
			"leaves a file alone when an in-place rescan reaches it through a symlink",
			func() {
				realRoot := filepath.Join(base, "real")
				video(filepath.Join(
					realRoot, "Breaking Bad", "Season 01", "Breaking Bad S01E01.mkv",
				), "old")
				link := filepath.Join(base, "library")
				Expect(os.Symlink(realRoot, link)).To(Succeed())

				// Adopted under the configured root's spelling, then re-scanned
				// under the resolved one the scanner stores.
				commit(
					filepath.Join(link, "Breaking Bad"),
					entimportscan.ModeInPlace, nil,
				)
				show, before := tracked()
				Expect(before.Path).To(HavePrefix(link))
				commit(
					filepath.Join(realRoot, "Breaking Bad"),
					entimportscan.ModeInPlace, show,
				)

				_, after := tracked()
				Expect(after.ID).To(Equal(before.ID))
				Expect(before.Path).To(BeAnExistingFile())
			},
		)
	},
)
