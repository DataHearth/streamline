package bulkimport

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/media/movie"
	moviemocks "github.com/datahearth/streamline/internal/media/movie/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("commitOne — migrated rows", Label("unit", "bulkimport"), func() {
	var (
		ctx    context.Context
		movies *moviemocks.MockManager
		store  *dbmocks.MockStore
		svc    *Service
		scan   *ent.ImportScan
		lib    string
	)

	profiles := func(names ...string) map[string]any {
		ps := make([]map[string]any, 0, len(names))
		for _, n := range names {
			ps = append(ps, map[string]any{
				"name":                 n,
				"min_resolution":       "720p",
				"preferred_resolution": "1080p",
			})
		}
		return map[string]any{
			"quality_profiles":        ps,
			"quality_default_profile": names[0],
		}
	}

	flags := func(monitored bool, profile *string) any {
		return mock.MatchedBy(func(p movie.UpdateParams) bool {
			if p.Monitored == nil || *p.Monitored != monitored {
				return false
			}
			if profile == nil {
				return p.QualityProfile == nil
			}
			return p.QualityProfile != nil && *p.QualityProfile == *profile
		})
	}

	BeforeEach(func() {
		ctx = context.Background()
		lib = GinkgoT().TempDir()
		movies = moviemocks.NewMockManager(GinkgoT())
		// No CreateMediaFile expectation is registered on a title-only path:
		// the mock fails the spec if the commit links a file anyway.
		store = dbmocks.NewMockStore(GinkgoT())
		svc = &Service{store: store, movieSvc: movies, moviePath: lib}
		scan = &ent.ImportScan{
			ID:     1,
			Kind:   entimportscan.KindMovie,
			Mode:   entimportscan.ModeInPlace,
			Source: entimportscan.SourceRadarr,
		}
	})

	It(
		"adds a title with no file with its profile and flag, and links nothing",
		func() {
			configtest.Setup(profiles("Default", "HD"))
			hd := "HD"
			movies.EXPECT().Add(mock.Anything, uint32(27205), "HD").
				Return(&ent.Movie{ID: 3}, "", nil).Once()
			movies.EXPECT().Update(mock.Anything, uint32(3), flags(false, &hd)).
				Return(&ent.Movie{ID: 3}, nil).Once()

			outcome, msg, id := svc.commitOne(ctx, scan, &ent.ImportScanFile{
				TmdbID:         27205,
				QualityProfile: "HD",
				Monitored:      false,
				Classification: entimportscanfile.ClassificationConfirmed,
			})

			Expect(outcome).To(Equal(entimportscanfile.OutcomeCreated))
			Expect(msg).To(BeEmpty())
			Expect(id).To(Equal(uint32(3)))
		},
	)

	It(
		"falls back to the default profile and says so when the profile is gone",
		func() {
			configtest.Setup(profiles("Default"))
			movies.EXPECT().Add(mock.Anything, uint32(27205), "").
				Return(&ent.Movie{ID: 4}, "", nil).Once()
			movies.EXPECT().Update(mock.Anything, uint32(4), flags(true, nil)).
				Return(&ent.Movie{ID: 4}, nil).Once()

			outcome, msg, _ := svc.commitOne(ctx, scan, &ent.ImportScanFile{
				TmdbID: 27205, QualityProfile: "HD", Monitored: true,
				Classification: entimportscanfile.ClassificationConfirmed,
			})

			Expect(outcome).To(Equal(entimportscanfile.OutcomeCreated))
			Expect(msg).To(ContainSubstring(`"HD"`))
			Expect(msg).To(ContainSubstring("default"))
		},
	)

	It("reports attached for a title already in the library", func() {
		configtest.Setup(profiles("HD"))
		hd := "HD"
		movies.EXPECT().Add(mock.Anything, uint32(27205), "HD").
			Return(nil, "", movie.ErrMovieExists).Once()
		store.EXPECT().FindMovieByTMDBID(mock.Anything, uint32(27205)).
			Return(&ent.Movie{ID: 8}, nil).Once()
		movies.EXPECT().Update(mock.Anything, uint32(8), flags(true, &hd)).
			Return(&ent.Movie{ID: 8}, nil).Once()

		outcome, _, id := svc.commitOne(ctx, scan, &ent.ImportScanFile{
			TmdbID: 27205, ExistingMovieID: 8, QualityProfile: "HD",
			Monitored:      true,
			Classification: entimportscanfile.ClassificationExisting,
		})

		Expect(outcome).To(Equal(entimportscanfile.OutcomeAttached))
		Expect(id).To(Equal(uint32(8)))
	})

	It("adds the title without a file it cannot open, and says why", func() {
		configtest.Setup(profiles("HD"))
		movies.EXPECT().Add(mock.Anything, uint32(1), "").
			Return(&ent.Movie{ID: 5}, "", nil).Once()
		movies.EXPECT().Update(mock.Anything, uint32(5), flags(true, nil)).
			Return(&ent.Movie{ID: 5}, nil).Once()

		missing := filepath.Join(lib, "Gone", "gone.mkv")
		outcome, msg, id := svc.commitOne(ctx, scan, &ent.ImportScanFile{
			TmdbID: 1, SourcePath: missing, Monitored: true,
			Classification: entimportscanfile.ClassificationAmbiguous,
			Decision:       entimportscanfile.DecisionAccept,
		})

		Expect(outcome).To(Equal(entimportscanfile.OutcomeCreated))
		Expect(id).To(Equal(uint32(5)))
		Expect(msg).To(ContainSubstring(missing))
	})

	It("links a usable file in place and then applies the flags", func() {
		configtest.Setup(profiles("HD"))
		path := filepath.Join(lib, "Inception (2010)", "Inception.mkv")
		touch(path)

		movies.EXPECT().Add(mock.Anything, uint32(27205), "").
			Return(&ent.Movie{ID: 6}, "", nil).Once()
		store.EXPECT().CreateMediaFile(mock.Anything, mock.Anything).
			Return(&ent.MediaFile{ID: 1}, nil).Once()
		store.EXPECT().UpdateMovieStatus(mock.Anything, uint32(6), mock.Anything).
			Return(nil).Once()
		movies.EXPECT().Update(mock.Anything, uint32(6), flags(true, nil)).
			Return(&ent.Movie{ID: 6}, nil).Once()

		outcome, msg, id := svc.commitOne(ctx, scan, &ent.ImportScanFile{
			TmdbID: 27205, SourcePath: path, Size: 1, Monitored: true,
			Classification: entimportscanfile.ClassificationConfirmed,
		})

		Expect(outcome).To(Equal(entimportscanfile.OutcomeCreated))
		Expect(msg).To(BeEmpty())
		Expect(id).To(Equal(uint32(6)))
	})
})
