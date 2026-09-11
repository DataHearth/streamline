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
	"github.com/datahearth/streamline/ent/mediaevent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/transcodejob"
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
	"Series commit (adopt show)",
	Label("integration", "bulkimport"),
	func() {
		var (
			ctx    context.Context
			tmpDir string
			client *ent.Client
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
			client = dbtest.SetupTestDB(ctx)
			events.Register(client)
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
			path := filepath.Join(tmpDir, showFolder, "Season 01", file)
			Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
			Expect(
				os.WriteFile(
					path,
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
							{
								SeasonNumber: 1,
								Number:       4,
								Title:        "Cancer Man",
							},
						},
					}, nil).
					Once()
				tvmeta.EXPECT().GetSeriesCast(mock.Anything, tvdbID).
					Return(nil, nil).Once()

				placeEpisode("Breaking Bad", "Breaking Bad S01E01.mkv")
				placeEpisode("Breaking Bad", "Breaking Bad S01E02.mkv")
				// The layout unsanitized slashes in the episode title used to
				// produce: one folder per slash, the number on the top one, the
				// file says nothing.
				placeEpisode(
					"Breaking Bad",
					filepath.Join(
						"Breaking Bad - S01E04 - Cancer", "Man", "Again [].mkv",
					),
				)

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
								FileCount:      3,
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
				Expect(statuses[4]).To(Equal("available"))
				Expect(fileCounts[1]).To(Equal(1))
				Expect(fileCounts[2]).To(Equal(1))
				Expect(fileCounts[3]).To(Equal(0))
				Expect(fileCounts[4]).To(Equal(1))

				// Media file points at the on-disk path (adopted in place, not moved).
				mf, err := store.FindMediaFileByEpisodeID(ctx, episodeID(full))
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

				// One series-scoped row for the whole commit, not one per file.
				imported := client.MediaEvent.Query().
					Where(mediaevent.TypeEQ(
						mediaevent.Type(events.TypeImported),
					)).
					WithTvShow().
					WithEpisode().
					AllX(ctx)
				Expect(imported).To(HaveLen(1))
				Expect(imported[0].Edges.Episode).To(BeNil())
				Expect(imported[0].Edges.TvShow).NotTo(BeNil())
				Expect(imported[0].Edges.TvShow.ID).To(Equal(show.ID))
				Expect(imported[0].Payload).To(
					HaveKeyWithValue("episodes", BeNumerically("==", 3)),
				)
				Expect(imported[0].Payload).To(
					HaveKeyWithValue("source", "bulk_import"),
				)
				Expect(imported[0].Payload).To(HaveKeyWithValue(
					"seasons", ConsistOf(BeNumerically("==", 1)),
				))

				// Re-adopting the show with a different E01 file replaces the
				// episode's tracked file instead of double-linking it.
				oldPath := mf.Path
				placeEpisode("Breaking Bad", "Breaking Bad S01E01 REPACK.mkv")
				scan2, err := store.CreateImportScan(ctx, db.CreateImportScanParams{
					SourcePath: tmpDir,
					Kind:       entimportscan.KindSeries,
					Mode:       entimportscan.ModeInPlace,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(store.UpdateImportScanStatus(
					ctx,
					scan2.ID,
					entimportscan.StatusAwaitingReview,
					db.UpdateScanStatusOpts{},
				)).To(Succeed())
				Expect(store.BulkCreateImportScanShows(
					ctx,
					scan2.ID,
					[]db.CreateImportScanShowParams{{
						FolderPath:       filepath.Join(tmpDir, "Breaking Bad"),
						ParsedTitle:      "Breaking Bad",
						Classification:   entimportscanshow.ClassificationExisting,
						ExistingTvshowID: &show.ID,
						FileCount:        3,
					}},
				)).To(Succeed())

				svc.runCommitSeries(ctx, scan2)

				// E01 now tracks the repack; the old file is gone from disk.
				// E02's file is re-scanned at its same path, so it is untouched.
				mf2, err := store.FindMediaFileByEpisodeID(ctx, episodeID(full))
				Expect(err).NotTo(HaveOccurred())
				Expect(mf2.Path).To(Equal(filepath.Join(
					tmpDir,
					"Breaking Bad",
					"Season 01",
					"Breaking Bad S01E01 REPACK.mkv",
				)))
				Expect(oldPath).NotTo(BeAnExistingFile())
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
				root := GinkgoT().TempDir()
				srcDir := filepath.Join(root, "downloads")
				libDir := filepath.Join(root, "tv")
				showFolder := filepath.Join(srcDir, "Breaking Bad")
				Expect(os.MkdirAll(showFolder, 0o755)).To(Succeed())
				srcFile := filepath.Join(showFolder, "Breaking Bad S01E01.mkv")
				Expect(
					os.WriteFile(srcFile, make([]byte, 60*1024*1024), 0o644),
				).To(Succeed())
				configtest.Setup(map[string]any{
					"library": map[string]any{
						"series_path": libDir,
						"series_naming": "{title}/Season {season:02}/" +
							"{title} - S{season:02}E{episode:02}.{ext}",
						"import_mode": "hardlink",
					},
				})

				client := dbtest.SetupTestDB(ctx)
				events.Register(client)
				DeferCleanup(client.Close)
				store := db.New(client)
				tvmeta := metamocks.NewMockTVProvider(GinkgoT())
				importSvc := library.NewImportService()
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
				tvmeta.EXPECT().GetSeriesCast(mock.Anything, tvdbID).
					Return(nil, nil).Once()

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

				mf, err := store.FindMediaFileByEpisodeID(ctx, episodeID(full))
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

var _ = Describe(
	"Series commit queues transcode jobs",
	Label("integration", "bulkimport"),
	func() {
		It(
			"queues a transcode job for the linked episode file when the show's profile is transcode-eligible",
			func() {
				ctx := context.Background()
				configtest.Setup(map[string]any{
					"transcoding": map[string]any{"enabled": true},
					"quality_profiles": []map[string]any{{
						"name": "hd", "preferred_resolution": "1080p",
						"min_resolution": "720p",
						"transcode": map[string]any{
							"to": map[string]any{
								"container": "mkv", "video_codec": "hevc",
								"preset": "medium", "audio_codec": "aac",
							},
						},
					}},
					"quality_default_profile": "hd",
				})
				tmpDir := GinkgoT().TempDir()
				client := dbtest.SetupTestDB(ctx)
				events.Register(client)
				DeferCleanup(client.Close)
				store := db.New(client)
				tvmeta := metamocks.NewMockTVProvider(GinkgoT())
				tvSvc := tvshow.NewService(store, tvmeta, nil, nil)
				svc := NewService(
					store,
					nil,
					tvmeta,
					nil,
					nil,
					tvSvc,
					tmpDir,
					tmpDir,
				)

				const tvdbID = uint32(99123)
				tvmeta.EXPECT().
					GetSeries(mock.Anything, tvdbID).
					Return(&metadata.TVDetails{
						TVResult: metadata.TVResult{
							TVDBID: tvdbID, Title: "Better Call Saul", Year: 2015,
						},
						Status: "ended",
						Type:   metadata.SeriesStandard,
						Seasons: []metadata.SeasonInfo{
							{Number: 1, Name: "Season 1"},
						},
						Episodes: []metadata.EpisodeInfo{
							{SeasonNumber: 1, Number: 1, Title: "Uno"},
						},
					}, nil).
					Once()
				tvmeta.EXPECT().GetSeriesCast(mock.Anything, tvdbID).
					Return(nil, nil).Once()

				dir := filepath.Join(tmpDir, "Better Call Saul", "Season 01")
				Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
				Expect(os.WriteFile(
					filepath.Join(dir, "Better Call Saul S01E01.mkv"),
					make([]byte, 60*1024*1024),
					0o644,
				)).To(Succeed())

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
				Expect(store.BulkCreateImportScanShows(
					ctx,
					scan.ID,
					[]db.CreateImportScanShowParams{
						{
							FolderPath: filepath.Join(
								tmpDir,
								"Better Call Saul",
							),
							ParsedTitle:    "Better Call Saul",
							Classification: entimportscanshow.ClassificationConfirmed,
							TVDBID:         &id,
							FileCount:      1,
						},
					},
				)).To(Succeed())

				svc.runCommitSeries(ctx, scan)

				show, err := store.FindTVShowByTVDBID(ctx, tvdbID)
				Expect(err).NotTo(HaveOccurred())
				full, err := store.FindTVShowByID(ctx, show.ID)
				Expect(err).NotTo(HaveOccurred())

				mf, err := store.FindMediaFileByEpisodeID(ctx, episodeID(full))
				Expect(err).NotTo(HaveOccurred())

				n, err := client.TranscodeJob.Query().
					Where(transcodejob.HasMediaFileWith(mediafile.ID(mf.ID))).
					Count(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(n).To(Equal(1))
			},
		)
	},
)

// episodeID returns the id of episode 1 in show — every caller in this file
// only ever needs the first episode.
func episodeID(show *ent.TVShow) uint32 {
	for _, se := range show.Edges.Seasons {
		for _, ep := range se.Edges.Episodes {
			if ep.Number == 1 {
				return ep.ID
			}
		}
	}
	return 0
}
