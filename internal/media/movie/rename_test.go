package movie

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

var _ = Describe("RenameService", Label("unit", "movies"), func() {
	var (
		ctx context.Context
		tmp string
	)

	// Test-only naming template (kept tight & deterministic). Production wires
	// config.Library.MovieNaming.
	const naming = "{title} ({year})/{title} ({year}).{ext}"

	BeforeEach(func() {
		ctx = context.Background()
		tmp = GinkgoT().TempDir()
	})

	It("returns an empty plan when files already match the target", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		svc := NewRenameService(store, "/library/movies", naming)
		movie := &ent.Movie{ID: 1, Title: "The Matrix", Year: 1999, TmdbID: 603}
		files := []*ent.MediaFile{{
			ID:   10,
			Path: "/library/movies/The Matrix (1999)/The Matrix (1999).mkv",
		}}
		store.EXPECT().FindMovieByID(mock.Anything, uint32(1)).
			Return(movie, nil).Once()
		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(1)).
			Return(files, nil).Once()

		plan, err := svc.Preview(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(BeEmpty())
	})

	It("plans a move for a misnamed file", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		svc := NewRenameService(store, "/library/movies", naming)
		movie := &ent.Movie{ID: 1, Title: "Dune", Year: 2021, TmdbID: 438631}
		src := filepath.Join(tmp, "Dune.2021.1080p.WEB-DL.x264-GROUP.mkv")
		Expect(os.WriteFile(src, []byte("x"), 0o644)).To(Succeed())
		files := []*ent.MediaFile{{ID: 10, Path: src}}
		store.EXPECT().FindMovieByID(mock.Anything, uint32(1)).
			Return(movie, nil).Once()
		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(1)).
			Return(files, nil).Once()

		plan, err := svc.Preview(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(HaveLen(1))
		Expect(plan.Operations[0].MediaFileID).To(Equal(uint32(10)))
		Expect(plan.Operations[0].From).To(Equal(src))
		Expect(plan.Operations[0].To).To(
			Equal("/library/movies/Dune (2021)/Dune (2021).mkv"),
		)
	})

	It("applies the plan, moves files, and updates DB paths", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		svc := NewRenameService(store, tmp, naming)
		movie := &ent.Movie{ID: 1, Title: "Dune", Year: 2021, TmdbID: 438631}
		src := filepath.Join(tmp, "Dune.misnamed.mkv")
		Expect(os.WriteFile(src, []byte("x"), 0o644)).To(Succeed())
		files := []*ent.MediaFile{{ID: 10, Path: src}}
		store.EXPECT().FindMovieByID(mock.Anything, uint32(1)).
			Return(movie, nil).Once()
		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(1)).
			Return(files, nil).Once()
		store.EXPECT().UpdateMediaFilePath(
			mock.Anything, uint32(10), mock.AnythingOfType("string"),
		).Return(nil).Once()

		plan, err := svc.Apply(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(HaveLen(1))
		_, statErr := os.Stat(src)
		Expect(os.IsNotExist(statErr)).To(BeTrue())
		_, statErr = os.Stat(plan.Operations[0].To)
		Expect(statErr).NotTo(HaveOccurred())
	})
})
