package roothealth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

type countingStore struct {
	Store
	paths, counts int
}

func (s *countingStore) RootFilePathAt(
	ctx context.Context,
	root db.LibraryRoot,
	offset int,
) (string, error) {
	s.paths++
	return s.Store.RootFilePathAt(ctx, root, offset)
}

func (s *countingStore) CountRootFiles(
	ctx context.Context,
	root db.LibraryRoot,
) (int, error) {
	s.counts++
	return s.Store.CountRootFiles(ctx, root)
}

var _ = Describe("library root health", Label("integration", "roothealth"), func() {
	var (
		ctx       context.Context
		client    *ent.Client
		store     *countingStore
		root      string
		elsewhere string
		movie     *ent.Movie
	)

	touch := func(path string) {
		GinkgoHelper()
		Expect(os.WriteFile(path, []byte("data"), 0o644)).To(Succeed())
	}
	useMovieRoot := func(path string) {
		GinkgoHelper()
		configtest.Setup(
			map[string]any{"library": map[string]any{"movie_path": path}},
		)
	}
	// movieRows writes n rows in id order, the file of each named by present
	// existing on disk.
	movieRows := func(n int, present func(i int) bool) {
		GinkgoHelper()
		builders := make([]*ent.MediaFileCreate, n)
		for i := range n {
			path := filepath.Join(elsewhere, fmt.Sprintf("%04d.mkv", i))
			if present(i) {
				touch(path)
			}
			builders[i] = client.MediaFile.Create().
				SetPath(path).
				SetSize(4).
				SetMovie(movie)
		}
		client.MediaFile.CreateBulk(builders...).SaveX(ctx)
	}
	allGone := func(int) bool { return false }

	BeforeEach(func() {
		ctx = context.Background()
		root = GinkgoT().TempDir()
		elsewhere = GinkgoT().TempDir()
		useMovieRoot(root)
		client = dbtest.SetupTestDB(ctx)
		DeferCleanup(client.Close)
		store = &countingStore{Store: db.New(client)}
		Register(store)
		DeferCleanup(func() { Register(nil) })
		movie = client.Movie.Create().
			SetTmdbID(1).
			SetTitle("Movie").
			SetOriginalTitle("Movie").
			SetYear(2020).
			SaveX(ctx)
	})

	It("is online with no tracked files, even when the path is missing", func() {
		useMovieRoot(filepath.Join(root, "absent"))
		Expect(Offline(ctx, db.RootMovies)).To(BeNil())
	})

	It("is online when any sampled file exists", func() {
		movieRows(20, func(i int) bool { return i == 19 })
		Expect(Offline(ctx, db.RootMovies)).To(BeNil())
	})

	It("costs one query and one stat for a healthy root", func() {
		movieRows(100, func(int) bool { return true })
		Expect(Offline(ctx, db.RootMovies)).To(BeNil())
		Expect(store.paths).To(Equal(1))
		Expect(store.counts).To(BeZero())
	})

	It(
		"is online when the path is empty but the tracked files resolve elsewhere",
		func() {
			movieRows(5, func(int) bool { return true })
			Expect(Offline(ctx, db.RootMovies)).To(BeNil())
		},
	)

	DescribeTable(
		"names why a root is offline",
		func(prepare func() string, want Reason) {
			movieRows(3, allGone)
			path := prepare()
			useMovieRoot(path)

			status, err := Offline(ctx, db.RootMovies)

			Expect(err).NotTo(HaveOccurred())
			Expect(status).To(Equal(&Status{
				Root: db.RootMovies, Path: path, Tracked: 3, Reason: want,
			}))
		},
		Entry(
			"path_missing",
			func() string { return filepath.Join(root, "absent") },
			ReasonPathMissing,
		),
		Entry("path_empty", func() string { return root }, ReasonPathEmpty),
		Entry("files_missing", func() string {
			touch(filepath.Join(root, "unrelated"))
			return root
		}, ReasonFilesMissing),
	)

	DescribeTable("is online when a clustered batch of old rows is gone",
		func(rows, goneLowest int) {
			movieRows(rows, func(i int) bool { return i >= goneLowest })
			Expect(Offline(ctx, db.RootMovies)).To(BeNil())
		},
		Entry("100 rows, the lowest 30 gone", 100, 30),
		Entry("39 rows, the lowest 20 gone", 39, 20),
		Entry("21 rows, the lowest 20 gone", 21, 20),
	)

	It("samples at most 20 files", func() {
		movieRows(1000, allGone)
		status, err := Offline(ctx, db.RootMovies)
		Expect(err).NotTo(HaveOccurred())
		Expect(status).NotTo(BeNil())
		Expect(status.Tracked).To(Equal(1000))
		Expect(store.paths).To(BeNumerically("<=", 20))
	})

	It("splits the book roots by book_kind", func() {
		configtest.Setup(map[string]any{"library": map[string]any{
			"ebook_path": root, "audiobook_path": root,
		}})
		book := client.Book.Create().SetHardcoverID(1).SetTitle("Book").SaveX(ctx)
		client.MediaFile.Create().
			SetPath(filepath.Join(elsewhere, "gone.epub")).
			SetSize(4).
			SetBook(book).
			SetBookKind(entmediafile.BookKindEbook).
			SaveX(ctx)
		kept := filepath.Join(elsewhere, "kept.m4b")
		touch(kept)
		client.MediaFile.Create().SetPath(kept).SetSize(4).
			SetBook(book).SetBookKind(entmediafile.BookKindAudiobook).SaveX(ctx)

		Expect(Offline(ctx, db.RootAudiobooks)).To(BeNil())
		status, err := Offline(ctx, db.RootEbooks)
		Expect(err).NotTo(HaveOccurred())
		Expect(status).NotTo(BeNil())
		Expect(status.Tracked).To(Equal(1))
	})

	It("files each owner type under its own root", func() {
		show := client.TVShow.Create().
			SetTitle("Show").
			SetYear(2020).
			SetTvdbID(1).
			SaveX(ctx)
		season := client.Season.Create().SetNumber(1).SetTvShow(show).SaveX(ctx)
		episode := client.Episode.Create().SetNumber(1).SetSeason(season).SaveX(ctx)
		artist := client.Artist.Create().SetMbid("a").SetName("Artist").SaveX(ctx)
		album := client.Album.Create().
			SetMbid("b").
			SetTitle("Album").
			SetArtist(artist).
			SaveX(ctx)
		track := client.Track.Create().
			SetTitle("T").
			SetDisc(1).
			SetPosition(1).
			SetAlbum(album).
			SaveX(ctx)
		client.MediaFile.Create().
			SetPath("/gone/e.mkv").
			SetSize(4).
			SetEpisode(episode).
			SaveX(ctx)
		client.MediaFile.Create().
			SetPath("/gone/t.flac").
			SetSize(4).
			SetTrack(track).
			SaveX(ctx)

		statuses, err := AllOffline(ctx)

		Expect(err).NotTo(HaveOccurred())
		roots := make([]db.LibraryRoot, len(statuses))
		for i, s := range statuses {
			roots[i] = s.Root
		}
		Expect(roots).To(ConsistOf(db.RootSeries, db.RootMusic))
	})

	It("reports every root online when nothing is registered", func() {
		movieRows(3, allGone)
		Register(nil)
		Expect(Offline(ctx, db.RootMovies)).To(BeNil())
		Expect(AllOffline(ctx)).To(BeEmpty())
		Expect(Require(ctx, db.RootMovies)).To(Succeed())
	})

	It("refuses through Require with the status of the offline root", func() {
		movieRows(3, allGone)
		var offline *OfflineError
		Expect(
			errors.As(Require(ctx, db.RootSeries, db.RootMovies), &offline),
		).To(BeTrue())
		Expect(offline.Root).To(Equal(db.RootMovies))
		Expect(Require(ctx, db.RootSeries)).To(Succeed())
	})

	It("logs CRITICAL once on going offline and INFO once on recovery", func() {
		var logs bytes.Buffer
		GinkgoWriter.TeeTo(&logs)
		DeferCleanup(GinkgoWriter.ClearTeeWriters)
		path := filepath.Join(elsewhere, "back.mkv")
		client.MediaFile.Create().SetPath(path).SetSize(4).SetMovie(movie).SaveX(ctx)

		for range 3 {
			Expect(Offline(ctx, db.RootMovies)).NotTo(BeNil())
		}
		Expect(strings.Count(logs.String(), "level=ERROR+4")).To(Equal(1))

		touch(path)
		for range 3 {
			Expect(Offline(ctx, db.RootMovies)).To(BeNil())
		}
		Expect(
			strings.Count(logs.String(), `level=INFO msg="library root is back"`),
		).To(Equal(1))
		Expect(strings.Count(logs.String(), "level=ERROR+4")).To(Equal(1))
	})
})
