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
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/arr"
	arrmocks "github.com/datahearth/streamline/internal/arr/mocks"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

var _ = Describe("validateArrScanParams", Label("unit", "bulkimport"), func() {
	var (
		svc     *Service
		library string
	)

	BeforeEach(func() {
		library = GinkgoT().TempDir()
		svc = &Service{moviePath: library, seriesPath: library}
	})

	params := func(mode entimportscan.Mode, to string) StartScanParams {
		return StartScanParams{
			Source:    entimportscan.SourceRadarr,
			Kind:      entimportscan.KindMovie,
			SourceURL: "http://radarr.lan:7878",
			Mode:      mode,
			Mappings: schema.ScanMappings{
				Roots: []schema.RootMapping{{From: "/media/movies", To: to}},
			},
		}
	}

	It("accepts an in-place mapping that lands inside the library root", func() {
		inside := filepath.Join(library, "movies")
		Expect(os.MkdirAll(inside, 0o750)).To(Succeed())

		Expect(svc.validateArrScanParams(
			params(entimportscan.ModeInPlace, inside),
		)).To(Succeed())
	})

	It("refuses an in-place mapping that lands outside the library root", func() {
		Expect(svc.validateArrScanParams(
			params(entimportscan.ModeInPlace, GinkgoT().TempDir()),
		)).To(MatchError(ErrRootOutsideLibrary))
	})

	It("refuses a rename mapping that lands inside the library root", func() {
		Expect(svc.validateArrScanParams(
			params(entimportscan.ModeRename, library),
		)).To(MatchError(ErrRootOutsideLibrary))
	})

	It("refuses a mapping target that does not exist", func() {
		Expect(svc.validateArrScanParams(
			params(entimportscan.ModeInPlace, "/nope/nowhere"),
		)).To(MatchError(ErrInvalidPath))
	})

	It("requires a source url", func() {
		p := params(entimportscan.ModeInPlace, library)
		p.SourceURL = ""
		Expect(svc.validateArrScanParams(p)).To(MatchError(ErrMissingSourceURL))
	})
})

var _ = Describe("fetchRadarr", Label("unit", "bulkimport"), func() {
	It("classifies untracked confirmed, tracked existing, a missing file "+
		"ambiguous, and keeps a file-less title with an empty path", func() {
		library := GinkgoT().TempDir()
		films := filepath.Join(library, "films")
		touch(filepath.Join(films, "Inception (2010)", "Inception.2010.1080p.mkv"))

		store := dbmocks.NewMockStore(GinkgoT())
		lib := arrmocks.NewMockLibrary(GinkgoT())

		lib.EXPECT().Movies(mock.Anything).Return([]arr.Movie{
			{
				TMDBID: 27205, Title: "Inception", Year: 2010,
				Monitored: true, QualityProfileID: 4, HasFile: true,
				MovieFile: &arr.MovieFile{
					Path:         "/media/movies/Inception (2010)/Inception.2010.1080p.mkv",
					Size:         100,
					ReleaseGroup: "GRP",
				},
			},
			{
				TMDBID: 157336, Title: "Interstellar", Year: 2014,
				Monitored: false, QualityProfileID: 9,
			},
			{
				TMDBID: 1, Title: "Gone", Year: 2001, Monitored: true,
				HasFile:   true,
				MovieFile: &arr.MovieFile{Path: "/media/movies/Gone/gone.mkv"},
			},
		}, nil).Once()

		// Polled once the list is in and once the last row is written: three
		// movies never fill a batch, so the in-loop poll does not run.
		store.EXPECT().FindImportScan(mock.Anything, uint32(1)).
			Return(&ent.ImportScan{ID: 1, Status: entimportscan.StatusRunning}, nil).
			Twice()
		store.EXPECT().MovieTMDBIndex(mock.Anything).
			Return(map[uint32]uint32{157336: 9}, nil).Once()
		store.EXPECT().
			UpdateImportScanStatus(mock.Anything, uint32(1),
				entimportscan.StatusRunning, mock.Anything).
			Return(nil).Once()
		store.EXPECT().
			IncrementImportScanProgress(mock.Anything, uint32(1), 3).
			Return(nil).Once()

		var got []db.CreateImportScanFileParams
		store.EXPECT().
			BulkCreateImportScanFiles(mock.Anything, uint32(1), mock.Anything).
			Run(func(_ context.Context, _ uint32, rows []db.CreateImportScanFileParams) {
				got = append(got, rows...)
			}).
			Return(nil).Once()

		svc := &Service{store: store, moviePath: library}
		scan := &ent.ImportScan{
			ID:   1,
			Kind: entimportscan.KindMovie,
			Mode: entimportscan.ModeInPlace,
			Mappings: schema.ScanMappings{
				Roots: []schema.RootMapping{
					{From: "/media/movies", To: films},
				},
				Profiles: []schema.ProfileMapping{
					{SourceID: 4, Target: "HD"},
				},
			},
		}

		tally, active, err := svc.fetchRadarr(context.Background(), scan, lib)
		Expect(err).NotTo(HaveOccurred())
		Expect(active).To(BeTrue())
		Expect(tally).To(Equal(map[string]int{
			"confirmed": 1, "existing": 1, "ambiguous": 1,
		}))
		Expect(got).To(HaveLen(3))

		Expect(got[0].TMDBID).To(Equal(uint32(27205)))
		Expect(got[0].Classification).
			To(Equal(entimportscanfile.ClassificationConfirmed))
		Expect(got[0].SourcePath).
			To(Equal(filepath.Join(films, "Inception (2010)", "Inception.2010.1080p.mkv")))
		Expect(got[0].ParsedQuality).To(Equal("1080p"))
		Expect(got[0].ParsedReleaseGroup).To(Equal("GRP"))
		Expect(got[0].QualityProfile).To(Equal("HD"))
		Expect(*got[0].Monitored).To(BeTrue())

		Expect(got[1].Classification).
			To(Equal(entimportscanfile.ClassificationExisting))
		Expect(got[1].ExistingMovieID).To(Equal(uint32(9)))
		Expect(got[1].SourcePath).To(BeEmpty())
		Expect(got[1].QualityProfile).To(BeEmpty())
		Expect(*got[1].Monitored).To(BeFalse())

		Expect(got[2].Classification).
			To(Equal(entimportscanfile.ClassificationAmbiguous))
		Expect(got[2].Candidates).To(ConsistOf(schema.ScannedCandidate{
			TMDBID: 1, Title: "Gone", Year: 2001,
		}))
	})

	It("stops without touching the status when a cancel landed during the list",
		func() {
			store := dbmocks.NewMockStore(GinkgoT())
			lib := arrmocks.NewMockLibrary(GinkgoT())
			lib.EXPECT().Movies(mock.Anything).
				Return([]arr.Movie{{TMDBID: 1, Title: "One"}}, nil).Once()
			// No UpdateImportScanStatus expectation: writing running back over
			// the cancel is what the mock would fail on.
			store.EXPECT().FindImportScan(mock.Anything, uint32(1)).
				Return(&ent.ImportScan{
					ID: 1, Status: entimportscan.StatusCancelled,
				}, nil).Once()

			svc := &Service{store: store}
			scan := &ent.ImportScan{
				ID:   1,
				Kind: entimportscan.KindMovie,
				Mode: entimportscan.ModeInPlace,
			}
			_, active, err := svc.fetchRadarr(context.Background(), scan, lib)
			Expect(err).NotTo(HaveOccurred())
			Expect(active).To(BeFalse())
		})
})

var _ = Describe("buildMonitoring", Label("unit", "bulkimport"), func() {
	It("records every season and only the episodes that differ", func() {
		got := buildMonitoring(
			[]arr.Season{
				{SeasonNumber: 1, Monitored: true},
				{SeasonNumber: 2, Monitored: false},
			},
			[]arr.Episode{
				{SeasonNumber: 1, EpisodeNumber: 1, Monitored: true},
				{SeasonNumber: 1, EpisodeNumber: 2, Monitored: false},
				{SeasonNumber: 2, EpisodeNumber: 1, Monitored: false},
				{SeasonNumber: 2, EpisodeNumber: 2, Monitored: true},
			},
		)

		Expect(got.Seasons).To(Equal(map[uint16]bool{1: true, 2: false}))
		Expect(got.Episodes).To(ConsistOf(
			schema.EpisodeFlag{Season: 1, Episode: 2, Monitored: false},
			schema.EpisodeFlag{Season: 2, Episode: 2, Monitored: true},
		))
	})

	It("stores no episode exceptions for a uniformly monitored show", func() {
		got := buildMonitoring(
			[]arr.Season{{SeasonNumber: 1, Monitored: true}},
			[]arr.Episode{
				{SeasonNumber: 1, EpisodeNumber: 1, Monitored: true},
				{SeasonNumber: 1, EpisodeNumber: 2, Monitored: true},
			},
		)
		Expect(got.Episodes).To(BeEmpty())
	})
})

var _ = Describe("buildSourceFiles", Label("unit", "bulkimport"), func() {
	roots := []arr.RootMapping{{From: "/media/series", To: "/srv/tv"}}

	It("takes the embedded file and maps its path", func() {
		got := buildSourceFiles([]arr.Episode{
			{
				SeasonNumber: 1, EpisodeNumber: 1, HasFile: true, EpisodeFileID: 41,
				EpisodeFile: &arr.EpisodeFile{
					Path: "/media/series/Show/Season 01/S01E01.mkv",
					Size: 42,
				},
			},
			{SeasonNumber: 1, EpisodeNumber: 2, HasFile: false},
		}, roots)

		Expect(got).To(ConsistOf(schema.SourceEpisodeFile{
			Season: 1, Episode: 1,
			Path: "/srv/tv/Show/Season 01/S01E01.mkv", Size: 42,
		}))
	})

	It("skips an episode whose file the source did not embed", func() {
		got := buildSourceFiles([]arr.Episode{
			{SeasonNumber: 1, EpisodeNumber: 1, HasFile: true, EpisodeFileID: 7},
		}, roots)
		Expect(got).To(BeEmpty())
	})
})

var _ = Describe("fetchSonarr", Label("unit", "bulkimport"), func() {
	It("records the show with its flags, mapping, and type", func() {
		library := GinkgoT().TempDir()
		tv := filepath.Join(library, "tv")
		touch(filepath.Join(tv, "Breaking Bad", "Season 01", "S01E01.mkv"))

		store := dbmocks.NewMockStore(GinkgoT())
		lib := arrmocks.NewMockLibrary(GinkgoT())
		lib.EXPECT().Series(mock.Anything).Return([]arr.Series{{
			ID: 7, TVDBID: 81189, Title: "Breaking Bad", Year: 2008,
			Monitored: true, QualityProfileID: 6,
			Path: "/media/series/Breaking Bad", SeriesType: "standard",
			Seasons: []arr.Season{
				{SeasonNumber: 1, Monitored: true},
				{SeasonNumber: 2, Monitored: false},
			},
		}}, nil).Once()
		lib.EXPECT().Episodes(mock.Anything, uint32(7)).Return([]arr.Episode{
			{
				SeasonNumber: 1, EpisodeNumber: 1, Monitored: true, HasFile: true,
				EpisodeFile: &arr.EpisodeFile{
					Path: "/media/series/Breaking Bad/Season 01/S01E01.mkv",
					Size: 5,
				},
			},
			{SeasonNumber: 2, EpisodeNumber: 1, Monitored: true},
		}, nil).Once()

		// Polled once the list is in and once the last row is written; the
		// per-show poll only runs after cancellationPollEvery.
		store.EXPECT().FindImportScan(mock.Anything, uint32(2)).
			Return(&ent.ImportScan{ID: 2, Status: entimportscan.StatusRunning}, nil)
		store.EXPECT().TVShowTVDBIndex(mock.Anything).
			Return(map[uint32]uint32{}, nil).Once()
		store.EXPECT().
			UpdateImportScanStatus(mock.Anything, uint32(2),
				entimportscan.StatusRunning, mock.Anything).
			Return(nil).Once()
		store.EXPECT().
			IncrementImportScanProgress(mock.Anything, uint32(2), 1).
			Return(nil).Once()
		var got []db.CreateImportScanShowParams
		store.EXPECT().
			BulkCreateImportScanShows(mock.Anything, uint32(2), mock.Anything).
			Run(func(_ context.Context, _ uint32, rows []db.CreateImportScanShowParams) {
				got = append(got, rows...)
			}).
			Return(nil).Once()

		svc := &Service{store: store, seriesPath: library}
		scan := &ent.ImportScan{
			ID:   2,
			Kind: entimportscan.KindSeries,
			Mode: entimportscan.ModeInPlace,
			Mappings: schema.ScanMappings{
				Roots:    []schema.RootMapping{{From: "/media/series", To: tv}},
				Profiles: []schema.ProfileMapping{{SourceID: 6, Target: "TV"}},
			},
		}

		_, active, err := svc.fetchSonarr(context.Background(), scan, lib)
		Expect(err).NotTo(HaveOccurred())
		Expect(active).To(BeTrue())
		Expect(got).To(HaveLen(1))
		row := got[0]
		Expect(row.Classification).
			To(Equal(entimportscanshow.ClassificationConfirmed))
		Expect(row.FolderPath).To(Equal(filepath.Join(tv, "Breaking Bad")))
		Expect(*row.TVDBID).To(Equal(uint32(81189)))
		Expect(row.QualityProfile).To(Equal("TV"))
		Expect(row.SeriesType).To(Equal("standard"))
		Expect(row.FileCount).To(Equal(uint16(1)))
		Expect(row.SourceFiles).To(HaveLen(1))
		Expect(row.Monitoring.Episodes).To(ConsistOf(
			schema.EpisodeFlag{Season: 2, Episode: 1, Monitored: true},
		))
	})
})
