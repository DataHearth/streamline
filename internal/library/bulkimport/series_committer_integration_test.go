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
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/tvshow"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe(
	"Series commit (adopt show)",
	Label("integration", "bulkimport"),
	func() {
		var (
			ctx    context.Context
			tmpDir string
			store  db.Store
			tvmeta *metamocks.MockTVProvider
			svc    *Service
		)

		BeforeEach(func() {
			ctx = context.Background()
			// Adoption goes through tvshow.Add, which rejects a show when no
			// quality profile resolves.
			configtest.Setup(map[string]any{})
			tmpDir = GinkgoT().TempDir()
			client := dbtest.SetupTestDB(ctx)
			DeferCleanup(client.Close)
			store = db.New(client)
			tvmeta = metamocks.NewMockTVProvider(GinkgoT())
			// SeriesAdder = real tvshow.Service backed by the mock TVDB provider.
			tvSvc := tvshow.NewService(store, tvmeta, nil, nil)
			svc = NewService(store, nil, tvmeta, nil, nil, tvSvc, tmpDir, tmpDir)
		})

		// placeEpisode writes a >MinMediaSize file in a season subfolder, exercising
		// the recursive folder walk (Show/Season NN/episode layout).
		placeEpisode := func(showFolder, file string) {
			dir := filepath.Join(tmpDir, showFolder, "Season 01")
			Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
			Expect(
				os.WriteFile(
					filepath.Join(dir, file),
					make([]byte, 60*1024*1024),
					0o644,
				),
			).To(Succeed())
		}

		It(
			"creates the show, links on-disk episodes, leaves missing ones wanted",
			func() {
				const tvdbID = uint32(81189)
				tvmeta.EXPECT().
					GetSeries(mock.Anything, tvdbID).
					Return(&metadata.TVDetails{
						TVResult: metadata.TVResult{
							TVDBID: tvdbID,
							Title:  "Breaking Bad",
							Year:   2008,
						},
						Status: "ended",
						Type:   metadata.SeriesStandard,
						Seasons: []metadata.SeasonInfo{
							{Number: 1, Name: "Season 1"},
						},
						Episodes: []metadata.EpisodeInfo{
							{SeasonNumber: 1, Number: 1, Title: "Pilot"},
							{
								SeasonNumber: 1,
								Number:       2,
								Title:        "Cat's in the Bag...",
							},
							{
								SeasonNumber: 1,
								Number:       3,
								Title:        "...And the Bag's in the River",
							},
						},
					}, nil).
					Once()

				placeEpisode("Breaking Bad", "Breaking Bad S01E01.mkv")
				placeEpisode("Breaking Bad", "Breaking Bad S01E02.mkv")

				scan, err := store.CreateImportScan(ctx, db.CreateImportScanParams{
					SourcePath: tmpDir,
					Kind:       entimportscan.KindSeries,
					Mode:       entimportscan.ModeInPlace,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(store.UpdateImportScanStatus(
					ctx,
					scan.ID,
					entimportscan.StatusAwaitingReview,
					db.UpdateScanStatusOpts{},
				)).To(Succeed())
				id := tvdbID
				Expect(
					store.BulkCreateImportScanShows(
						ctx,
						scan.ID,
						[]db.CreateImportScanShowParams{
							{
								FolderPath: filepath.Join(
									tmpDir,
									"Breaking Bad",
								),
								ParsedTitle:    "Breaking Bad",
								Classification: entimportscanshow.ClassificationConfirmed,
								TVDBID:         &id,
								FileCount:      2,
							},
						},
					),
				).To(Succeed())

				svc.runCommitSeries(ctx, scan)

				// Show created from TVDB.
				show, err := store.FindTVShowByTVDBID(ctx, tvdbID)
				Expect(err).NotTo(HaveOccurred())

				// E01/E02 available with a linked media file; E03 stays wanted.
				full, err := store.FindTVShowByID(ctx, show.ID)
				Expect(err).NotTo(HaveOccurred())
				statuses := map[uint16]string{}
				fileCounts := map[uint16]int{}
				for _, se := range full.Edges.Seasons {
					for _, ep := range se.Edges.Episodes {
						statuses[ep.Number] = string(ep.Status)
						fileCounts[ep.Number] = len(ep.Edges.MediaFiles)
					}
				}
				Expect(statuses[1]).To(Equal("available"))
				Expect(statuses[2]).To(Equal("available"))
				Expect(statuses[3]).To(Equal("wanted"))
				Expect(fileCounts[1]).To(Equal(1))
				Expect(fileCounts[2]).To(Equal(1))
				Expect(fileCounts[3]).To(Equal(0))

				// Media file points at the on-disk path (adopted in place, not moved).
				mf, err := store.FindMediaFileByEpisodeID(ctx, episodeID(full, 1))
				Expect(err).NotTo(HaveOccurred())
				Expect(mf.Path).To(Equal(
					filepath.Join(
						tmpDir,
						"Breaking Bad",
						"Season 01",
						"Breaking Bad S01E01.mkv",
					),
				))

				// Scan flipped to completed with one success.
				refreshed, err := store.FindImportScan(ctx, scan.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(refreshed.Status)).To(Equal("completed"))
				Expect(refreshed.CommitSuccessCount).To(Equal(uint32(1)))
			},
		)
	},
)

var _ = Describe(
	"Series commit (rename mode)",
	Label("integration", "bulkimport"),
	func() {
		It(
			"transfers episodes into the series library with the scan's mode",
			func() {
				ctx := context.Background()
				configtest.Setup(map[string]any{})
				root := GinkgoT().TempDir()
				srcDir := filepath.Join(root, "downloads")
				libDir := filepath.Join(root, "tv")
				showFolder := filepath.Join(srcDir, "Breaking Bad")
				Expect(os.MkdirAll(showFolder, 0o755)).To(Succeed())
				srcFile := filepath.Join(showFolder, "Breaking Bad S01E01.mkv")
				Expect(
					os.WriteFile(srcFile, make([]byte, 60*1024*1024), 0o644),
				).To(Succeed())

				client := dbtest.SetupTestDB(ctx)
				DeferCleanup(client.Close)
				store := db.New(client)
				tvmeta := metamocks.NewMockTVProvider(GinkgoT())
				importSvc := library.NewImportService(&config.LibraryConfig{
					SeriesPath: libDir,
					SeriesNaming: "{title}/Season {season:02}/" +
						"{title} - S{season:02}E{episode:02}.{ext}",
					ImportMode: "hardlink",
				})
				svc := NewService(
					store, nil, tvmeta, importSvc, nil,
					tvshow.NewService(store, tvmeta, nil, nil), libDir, libDir,
				)

				const tvdbID = uint32(81189)
				tvmeta.EXPECT().GetSeries(mock.Anything, tvdbID).
					Return(&metadata.TVDetails{
						TVResult: metadata.TVResult{
							TVDBID: tvdbID, Title: "Breaking Bad", Year: 2008,
						},
						Status: "ended",
						Type:   metadata.SeriesStandard,
						Seasons: []metadata.SeasonInfo{
							{Number: 1, Name: "Season 1"},
						},
						Episodes: []metadata.EpisodeInfo{
							{SeasonNumber: 1, Number: 1, Title: "Pilot"},
						},
					}, nil).Once()

				scan, err := store.CreateImportScan(ctx, db.CreateImportScanParams{
					SourcePath: srcDir,
					Kind:       entimportscan.KindSeries,
					Mode:       entimportscan.ModeRename,
					ImportMode: entimportscan.ImportModeCopy,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(store.UpdateImportScanStatus(
					ctx, scan.ID, entimportscan.StatusAwaitingReview,
					db.UpdateScanStatusOpts{},
				)).To(Succeed())
				id := tvdbID
				Expect(store.BulkCreateImportScanShows(
					ctx, scan.ID, []db.CreateImportScanShowParams{{
						FolderPath:     showFolder,
						ParsedTitle:    "Breaking Bad",
						Classification: entimportscanshow.ClassificationConfirmed,
						TVDBID:         &id,
						FileCount:      1,
					}},
				)).To(Succeed())

				svc.runCommitSeries(ctx, scan)

				show, err := store.FindTVShowByTVDBID(ctx, tvdbID)
				Expect(err).NotTo(HaveOccurred())
				full, err := store.FindTVShowByID(ctx, show.ID)
				Expect(err).NotTo(HaveOccurred())

				mf, err := store.FindMediaFileByEpisodeID(ctx, episodeID(full, 1))
				Expect(err).NotTo(HaveOccurred())
				Expect(mf.Path).To(Equal(filepath.Join(
					libDir, "Breaking Bad", "Season 01",
					"Breaking Bad - S01E01.mkv",
				)))
				Expect(mf.Path).To(BeAnExistingFile())

				// scan.import_mode=copy overrides the service's hardlink default.
				srcInfo, err := os.Stat(srcFile)
				Expect(err).NotTo(HaveOccurred())
				dstInfo, err := os.Stat(mf.Path)
				Expect(err).NotTo(HaveOccurred())
				Expect(os.SameFile(srcInfo, dstInfo)).To(BeFalse())
			},
		)
	},
)

func episodeID(show *ent.TVShow, number uint16) uint32 {
	for _, se := range show.Edges.Seasons {
		for _, ep := range se.Edges.Episodes {
			if ep.Number == number {
				return ep.ID
			}
		}
	}
	return 0
}
