package bulkimport

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/media/tvshow"
	msmocks "github.com/datahearth/streamline/internal/mediaserver/mocks"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe(
	"Series commit (migrated show)",
	Label("integration", "bulkimport"),
	func() {
		It("adopts files by the source's episode numbers and carries its "+
			"type and monitoring", func() {
			ctx := context.Background()
			configtest.Setup(map[string]any{})
			libDir := GinkgoT().TempDir()
			client := dbtest.SetupTestDB(ctx)
			events.Register(client)
			DeferCleanup(client.Close)
			store := db.New(client)
			tvmeta := metamocks.NewMockTVProvider(GinkgoT())
			ms := msmocks.NewMockRefresher(GinkgoT())
			svc := NewService(
				store, nil, tvmeta, nil, nil,
				tvshow.NewService(store, tvmeta, nil, nil, nil), ms,
				libDir, libDir,
				nil, nil, nil, nil,
			)

			rescanned := make(chan struct{}, 1)
			ms.EXPECT().RefreshAll(mock.Anything, "series", libDir).
				Run(func(context.Context, string, string) { rescanned <- struct{}{} }).
				Return(nil).Once()

			const tvdbID = uint32(267440)
			tvmeta.EXPECT().GetSeries(mock.Anything, tvdbID).
				Return(&metadata.TVDetails{
					TVDBID: tvdbID, Title: "Attack on Titan", Year: 2013,
					Status: "ended", Type: metadata.SeriesStandard,
					Seasons: []metadata.SeasonInfo{
						{Number: 1, Name: "Season 1"},
						{Number: 2, Name: "Season 2"},
					},
					Episodes: []metadata.EpisodeInfo{
						{SeasonNumber: 1, Number: 1, Title: "One"},
						{SeasonNumber: 1, Number: 2, Title: "Two"},
						{SeasonNumber: 2, Number: 1, Title: "Three"},
					},
				}, nil).Once()
			tvmeta.EXPECT().GetSeriesCast(mock.Anything, tvdbID).
				Return(nil, nil).Once()

			// A name no parser can number: only the source's mapping places it.
			unparseable := filepath.Join(libDir, "AoT", "aot-first.mkv")
			Expect(os.MkdirAll(filepath.Dir(unparseable), 0o750)).To(Succeed())
			Expect(os.WriteFile(unparseable, []byte("x"), 0o600)).To(Succeed())

			scan, err := store.CreateImportScan(ctx, db.CreateImportScanParams{
				Kind:      entimportscan.KindSeries,
				Mode:      entimportscan.ModeInPlace,
				Source:    entimportscan.SourceSonarr,
				SourceURL: "http://sonarr.lan:8989",
				Mappings:  &schema.ScanMappings{},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(store.UpdateImportScanStatus(
				ctx, scan.ID, entimportscan.StatusAwaitingReview,
				db.UpdateScanStatusOpts{},
			)).To(Succeed())
			id, monitored := tvdbID, true
			Expect(store.BulkCreateImportScanShows(ctx, scan.ID,
				[]db.CreateImportScanShowParams{{
					FolderPath:     filepath.Join(libDir, "AoT"),
					ParsedTitle:    "Attack on Titan",
					Classification: entimportscanshow.ClassificationConfirmed,
					TVDBID:         &id,
					Monitored:      &monitored,
					SeriesType:     "anime",
					Monitoring: &schema.ShowMonitoring{
						Seasons: map[uint16]bool{1: true, 2: false},
						Episodes: []schema.EpisodeFlag{
							{Season: 1, Episode: 2, Monitored: false},
						},
					},
					SourceFiles: []schema.SourceEpisodeFile{
						{Season: 1, Episode: 1, Path: unparseable, Size: 1},
						{
							Season:  1,
							Episode: 2,
							Path:    filepath.Join(libDir, "AoT", "gone.mkv"),
						},
					},
					FileCount: 2,
				}},
			)).To(Succeed())
			scan, err = store.FindImportScan(ctx, scan.ID)
			Expect(err).NotTo(HaveOccurred())

			svc.runCommitSeries(ctx, scan)
			Eventually(rescanned).Should(Receive())

			show, err := store.FindTVShowByTVDBID(ctx, tvdbID)
			Expect(err).NotTo(HaveOccurred())
			full, err := store.FindTVShowByID(ctx, show.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(full.Type)).To(Equal("anime"))
			Expect(full.Monitored).To(BeTrue())

			type state struct {
				status    string
				monitored bool
				files     int
			}
			got := map[[2]uint16]state{}
			seasonFlags := map[uint16]bool{}
			for _, se := range full.Edges.Seasons {
				seasonFlags[se.Number] = se.Monitored
				for _, ep := range se.Edges.Episodes {
					got[[2]uint16{se.Number, ep.Number}] = state{
						string(ep.Status), ep.Monitored, len(ep.Edges.MediaFiles),
					}
				}
			}
			Expect(seasonFlags).To(Equal(map[uint16]bool{1: true, 2: false}))
			Expect(got[[2]uint16{1, 1}]).To(Equal(state{"available", true, 1}))
			Expect(got[[2]uint16{1, 2}]).To(Equal(state{"wanted", false, 0}))
			Expect(got[[2]uint16{2, 1}]).To(Equal(state{"wanted", false, 0}))

			rows, _, err := store.ListImportScanShows(
				ctx,
				db.ListImportScanShowsParams{
					ScanID: scan.ID, Limit: 10,
				},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1))
			Expect(rows[0].Outcome).To(Equal(entimportscanshow.OutcomeCreated))
			Expect(rows[0].OutcomeMessage).To(ContainSubstring("1 of 2 files"))
		})
	},
)
