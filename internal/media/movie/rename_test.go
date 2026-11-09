package movie

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/mediaserver"
	msmocks "github.com/datahearth/streamline/internal/mediaserver/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("RenameService", Label("unit", "movies"), func() {
	var (
		ctx context.Context
		tmp string
	)

	// Test-only naming template (kept tight & deterministic).
	const naming = "{title} ({year})/{title} ({year}).{ext}"

	BeforeEach(func() {
		ctx = context.Background()
		tmp = GinkgoT().TempDir()
	})

	It("returns an empty plan when files already match the target", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		svc := newTestRenameService(store, nil, "/library/movies", naming)
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
		svc := newTestRenameService(store, nil, "/library/movies", naming)
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

	It("skips a missing file and still renames the others", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		svc := newTestRenameService(store, nil, "/library/movies", naming)
		movie := &ent.Movie{ID: 1, Title: "Dune", Year: 2021, TmdbID: 438631}
		since := time.Now()
		files := []*ent.MediaFile{
			{ID: 10, Path: filepath.Join(tmp, "Gone.mkv"), MissingSince: &since},
			{ID: 11, Path: filepath.Join(tmp, "Here.mkv")},
		}
		store.EXPECT().FindMovieByID(mock.Anything, uint32(1)).
			Return(movie, nil).Once()
		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(1)).
			Return(files, nil).Once()

		plan, err := svc.Preview(ctx, 1)

		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(HaveLen(1))
		Expect(plan.Operations[0].MediaFileID).To(Equal(uint32(11)))
	})

	It("leaves out a file the template would put outside the library", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		svc := newTestRenameService(
			store,
			nil,
			"/library/movies",
			"../escaped/{title}.{ext}",
		)
		movie := &ent.Movie{ID: 1, Title: "Dune", Year: 2021, TmdbID: 438631}
		files := []*ent.MediaFile{{ID: 10, Path: "/library/movies/Dune/Dune.mkv"}}
		store.EXPECT().FindMovieByID(mock.Anything, uint32(1)).
			Return(movie, nil).Once()
		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(1)).
			Return(files, nil).Once()

		plan, err := svc.Preview(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(BeEmpty())
	})

	// The shipped template, which keeps only {quality} — so a file renamed
	// once no longer carries anything else in its name and re-parsing it can
	// only lose more. The row is the authority for both specs below.
	const defaultNaming = "{title} ({year}) {tmdb-{tmdb_id}}/{title} ({year}) [{quality}].{ext}"

	It("recovers a quality the name lost, from the probe on the row", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		svc := newTestRenameService(store, nil, "/library/movies", defaultNaming)
		movie := &ent.Movie{ID: 1, Title: "13 Hours", Year: 2016, TmdbID: 300671}
		files := []*ent.MediaFile{{
			ID: 10,
			Path: "/library/movies/13 Hours (2016) {tmdb-300671}/" +
				"13 Hours (2016) [].mkv",
			Width: 3840,
		}}
		store.EXPECT().FindMovieByID(mock.Anything, uint32(1)).
			Return(movie, nil).Once()
		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(1)).
			Return(files, nil).Once()

		plan, err := svc.Preview(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(HaveLen(1))
		Expect(plan.Operations[0].To).To(Equal(
			"/library/movies/13 Hours (2016) {tmdb-300671}/" +
				"13 Hours (2016) [2160p].mkv",
		))
	})

	// Nothing on the row and nothing left in the name: the empty brackets go
	// rather than re-rendering, so the file reaches a stable name instead of
	// being re-planned on every pass.
	It("drops the empty brackets when nothing can supply a quality", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		svc := newTestRenameService(store, nil, "/library/movies", defaultNaming)
		movie := &ent.Movie{ID: 1, Title: "13 Hours", Year: 2016, TmdbID: 300671}
		files := []*ent.MediaFile{{
			ID: 10,
			Path: "/library/movies/13 Hours (2016) {tmdb-300671}/" +
				"13 Hours (2016) [].mkv",
		}}
		store.EXPECT().FindMovieByID(mock.Anything, uint32(1)).
			Return(movie, nil).Once()
		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(1)).
			Return(files, nil).Once()

		plan, err := svc.Preview(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(HaveLen(1))
		Expect(plan.Operations[0].To).To(Equal(
			"/library/movies/13 Hours (2016) {tmdb-300671}/13 Hours (2016).mkv",
		))
	})

	It("applies the plan, moves files, and updates DB paths", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		ms := msmocks.NewMockRefresher(GinkgoT())
		svc := newTestRenameService(store, ms, tmp, naming)
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
		refreshed := make(chan []string, 1)
		ms.EXPECT().RefreshAll(mock.Anything, "movie", tmp, mock.Anything).
			Run(func(_ context.Context, _, _ string, paths ...string) {
				refreshed <- paths
			}).
			Return(nil).Once()

		plan, err := svc.Apply(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(HaveLen(1))
		_, statErr := os.Stat(src)
		Expect(os.IsNotExist(statErr)).To(BeTrue())
		_, statErr = os.Stat(plan.Operations[0].To)
		Expect(statErr).NotTo(HaveOccurred())
		// Both ends: the folder the file left as well as the one it reached.
		Eventually(refreshed).Should(Receive(Equal(
			[]string{src, plan.Operations[0].To},
		)))
	})

	It("plans against the root and template the config holds now", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		svc := newTestRenameService(store, nil, "/library/movies", naming)
		movie := &ent.Movie{ID: 1, Title: "Dune", Year: 2021, TmdbID: 438631}
		files := []*ent.MediaFile{{
			ID:   10,
			Path: "/library/movies/Dune (2021)/Dune (2021).mkv",
		}}
		store.EXPECT().FindMovieByID(mock.Anything, uint32(1)).
			Return(movie, nil).Twice()
		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(1)).
			Return(files, nil).Twice()

		// What a path migration does to the root, with no restart.
		moved := "/mnt/new/movies"
		_, err := config.UpdateLibrary(ctx, config.LibraryPatch{MoviePath: &moved})
		Expect(err).NotTo(HaveOccurred())

		plan, err := svc.Preview(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(HaveLen(1))
		Expect(plan.Operations[0].To).To(
			Equal("/mnt/new/movies/Dune (2021)/Dune (2021).mkv"),
		)

		// And what Settings → Library does to the template.
		flat := "{title} [{year}].{ext}"
		_, err = config.UpdateLibrary(ctx, config.LibraryPatch{MovieNaming: &flat})
		Expect(err).NotTo(HaveOccurred())

		plan, err = svc.Preview(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(HaveLen(1))
		Expect(plan.Operations[0].To).To(Equal("/mnt/new/movies/Dune [2021].mkv"))
	})

	It("applies under a root changed after the service was built", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		ms := msmocks.NewMockRefresher(GinkgoT())
		oldRoot := filepath.Join(tmp, "old")
		newRoot := filepath.Join(tmp, "new")
		svc := newTestRenameService(store, ms, oldRoot, naming)
		_, err := config.UpdateLibrary(ctx, config.LibraryPatch{MoviePath: &newRoot})
		Expect(err).NotTo(HaveOccurred())

		movie := &ent.Movie{ID: 1, Title: "Dune", Year: 2021, TmdbID: 438631}
		Expect(os.MkdirAll(newRoot, 0o755)).To(Succeed())
		src := filepath.Join(newRoot, "Dune.misnamed.mkv")
		Expect(os.WriteFile(src, []byte("x"), 0o644)).To(Succeed())
		want := filepath.Join(newRoot, "Dune (2021)", "Dune (2021).mkv")
		store.EXPECT().FindMovieByID(mock.Anything, uint32(1)).
			Return(movie, nil).Once()
		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(1)).
			Return([]*ent.MediaFile{{ID: 10, Path: src}}, nil).Once()
		store.EXPECT().UpdateMediaFilePath(mock.Anything, uint32(10), want).
			Return(nil).Once()
		refreshed := make(chan struct{})
		ms.EXPECT().RefreshAll(mock.Anything, "movie", newRoot, mock.Anything).
			Run(func(context.Context, string, string, ...string) { close(refreshed) }).
			Return(nil).Once()

		_, err = svc.Apply(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(want).To(BeARegularFile())
		Expect(oldRoot).NotTo(BeADirectory())
		Eventually(refreshed).Should(BeClosed())
	})

	It("prunes the directory the file left behind", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		svc := newTestRenameService(store, nil, tmp, naming)
		movie := &ent.Movie{ID: 1, Title: "Dune", Year: 2021, TmdbID: 438631}
		// The old layout of the same movie: renaming out of it empties it, which
		// is what a colon-collapsing re-rename does to every affected title.
		oldDir := filepath.Join(tmp, "Dune  - Part One (2021)")
		Expect(os.MkdirAll(oldDir, 0o755)).To(Succeed())
		src := filepath.Join(oldDir, "Dune.mkv")
		Expect(os.WriteFile(src, []byte("x"), 0o644)).To(Succeed())
		store.EXPECT().FindMovieByID(mock.Anything, uint32(1)).
			Return(movie, nil).Once()
		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(1)).
			Return([]*ent.MediaFile{{ID: 10, Path: src}}, nil).Once()
		store.EXPECT().UpdateMediaFilePath(
			mock.Anything, uint32(10), mock.AnythingOfType("string"),
		).Return(nil).Once()

		_, err := svc.Apply(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(oldDir).NotTo(BeADirectory())
		Expect(tmp).To(BeADirectory())
	})

	It("maps NotFound to ErrMovieNotFound on preview", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		svc := newTestRenameService(store, nil, "/library/movies", naming)
		store.EXPECT().FindMovieByID(mock.Anything, uint32(99)).
			Return(nil, &ent.NotFoundError{}).Once()

		_, err := svc.Preview(ctx, 99)
		Expect(err).To(MatchError(ErrMovieNotFound))
		Expect(err).To(MatchError(ContainSubstring("movie 99")))
	})

	It("maps NotFound to ErrMovieNotFound on apply", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		svc := newTestRenameService(store, nil, "/library/movies", naming)
		store.EXPECT().FindMovieByID(mock.Anything, uint32(99)).
			Return(nil, &ent.NotFoundError{}).Once()

		_, err := svc.Apply(ctx, 99)
		Expect(err).To(MatchError(ErrMovieNotFound))
		Expect(err).To(MatchError(ContainSubstring("movie 99")))
	})

	It("wraps generic lookup errors without the not-found sentinel", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		svc := newTestRenameService(store, nil, "/library/movies", naming)
		storeErr := errors.New("db down")
		store.EXPECT().FindMovieByID(mock.Anything, uint32(1)).
			Return(nil, storeErr).Once()

		_, err := svc.Preview(ctx, 1)
		Expect(err).To(MatchError(storeErr))
		Expect(err).To(MatchError(ContainSubstring("find movie")))
		Expect(err).NotTo(MatchError(ErrMovieNotFound))
	})

	It("keeps a media-file listing failure off the not-found path", func() {
		store := dbmocks.NewMockStore(GinkgoT())
		svc := newTestRenameService(store, nil, "/library/movies", naming)
		storeErr := errors.New("listing blew up")
		store.EXPECT().FindMovieByID(mock.Anything, uint32(1)).
			Return(&ent.Movie{ID: 1, Title: "Dune", Year: 2021}, nil).Once()
		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(1)).
			Return(nil, storeErr).Once()

		_, err := svc.Preview(ctx, 1)
		Expect(err).To(MatchError(storeErr))
		Expect(err).To(MatchError(ContainSubstring("list media_files")))
		Expect(err).NotTo(MatchError(ErrMovieNotFound))
	})
})

// newTestRenameService loads a config whose movie root and naming template are
// the given ones, backed by a file so a spec can change them through
// config.Update, and returns a service over it.
func newTestRenameService(
	store db.Store, ms mediaserver.Refresher, root, naming string,
) *RenameService {
	GinkgoHelper()
	configtest.SetupFile(map[string]any{
		"library": map[string]any{"movie_path": root, "movie_naming": naming},
	})
	return NewRenameService(store, ms)
}
