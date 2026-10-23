package music

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/metadata"
	mockmeta "github.com/datahearth/streamline/internal/metadata/mocks"
	mockposters "github.com/datahearth/streamline/internal/posters/mocks"
)

// stubCoverPaths points the mock's Path at a per-spec directory and returns a
// function that marks an album's cover as already cached.
func stubCoverPaths(p *mockposters.MockManager) func(id uint32) {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	path := func(id uint32) string {
		return filepath.Join(dir, strconv.FormatUint(uint64(id), 10))
	}
	p.EXPECT().Path("albums", mock.Anything).
		RunAndReturn(func(_ string, id uint32) string { return path(id) }).
		Maybe()
	return func(id uint32) {
		GinkgoHelper()
		Expect(os.WriteFile(path(id), []byte("cached"), 0o600)).To(Succeed())
	}
}

const (
	withPicture = "../../library/audiotags/testdata/withpicture.mp3"
	untagged    = "../../library/audiotags/testdata/untagged.mp3"
)

var _ = Describe("Cover resolver", Label("unit", "music"), func() {
	var (
		ctx     context.Context
		posters *mockposters.MockManager
		deezer  *mockmeta.MockCoverProvider
		svc     *Service
		cache   func(id uint32)
		album   *ent.Album
	)

	albumWithFiles := func(paths ...string) *ent.Album {
		GinkgoHelper()
		files := make([]*ent.MediaFile, len(paths))
		for i, p := range paths {
			files[i] = &ent.MediaFile{Path: p}
		}
		return &ent.Album{
			ID:      7,
			Mbid:    "rg-7",
			Title:   "Nevermind",
			Barcode: "0720642442524",
			Edges: ent.AlbumEdges{
				Artist: &ent.Artist{Name: "Nirvana"},
				Tracks: []*ent.Track{{Edges: ent.TrackEdges{MediaFiles: files}}},
			},
		}
	}

	putCapture := func(into *[]byte) func(context.Context, string, uint32, io.Reader) {
		return func(_ context.Context, _ string, _ uint32, r io.Reader) {
			b, err := io.ReadAll(r)
			Expect(err).NotTo(HaveOccurred())
			*into = b
		}
	}

	BeforeEach(func() {
		ctx = context.Background()
		posters = mockposters.NewMockManager(GinkgoT())
		deezer = mockmeta.NewMockCoverProvider(GinkgoT())
		cache = stubCoverPaths(posters)
		svc = &Service{posters: posters, covers: deezer}
		album = albumWithFiles()
	})

	It(
		"stores a picture embedded in one of the files and tries nothing else",
		func() {
			album = albumWithFiles(untagged, withPicture)
			var stored []byte
			posters.EXPECT().Put(mock.Anything, "albums", uint32(7), mock.Anything).
				Run(putCapture(&stored)).Return(nil).Once()

			svc.resolveCover(ctx, album)

			Expect(stored).NotTo(BeEmpty())
			Expect(stored[:2]).To(Equal([]byte{0xff, 0xd8}))
		},
	)

	It("falls back to a cover file beside the first media file, any case", func() {
		dir := GinkgoT().TempDir()
		track := filepath.Join(dir, "01.mp3")
		copyFile(untagged, track)
		Expect(
			os.WriteFile(filepath.Join(dir, "Folder.PNG"), []byte("folder"), 0o600),
		).
			To(Succeed())
		Expect(
			os.WriteFile(filepath.Join(dir, "COVER.Jpg"), []byte("cover"), 0o600),
		).
			To(Succeed())
		album = albumWithFiles(track)
		var stored []byte
		posters.EXPECT().Put(mock.Anything, "albums", uint32(7), mock.Anything).
			Run(putCapture(&stored)).Return(nil).Once()

		svc.resolveCover(ctx, album)

		Expect(string(stored)).To(Equal("cover"))
	})

	It("replaces a cached cover with a local one", func() {
		cache(7)
		album = albumWithFiles(withPicture)
		posters.EXPECT().Put(mock.Anything, "albums", uint32(7), mock.Anything).
			Return(nil).Once()

		svc.resolveCover(ctx, album)
	})

	It("takes Deezer by barcode before searching", func() {
		deezer.EXPECT().CoverByUPC(mock.Anything, "0720642442524").
			Return("https://cdn.example/upc.jpg", nil).Once()
		posters.EXPECT().
			Fetch(mock.Anything, "albums", uint32(7), "https://cdn.example/upc.jpg").
			Return(nil).Once()

		svc.resolveCover(ctx, album)
	})

	It("skips the barcode lookup when the album has none", func() {
		album.Barcode = ""
		deezer.EXPECT().SearchCover(mock.Anything, "Nirvana", "Nevermind").
			Return(&metadata.CoverHit{
				ArtistName: "Nirvana", CoverURL: "https://cdn.example/s.jpg",
			}, nil).Once()
		posters.EXPECT().
			Fetch(mock.Anything, "albums", uint32(7), "https://cdn.example/s.jpg").
			Return(nil).Once()

		svc.resolveCover(ctx, album)
	})

	It("searches when the barcode is unknown to Deezer", func() {
		deezer.EXPECT().CoverByUPC(mock.Anything, mock.Anything).
			Return("", nil).Once()
		deezer.EXPECT().SearchCover(mock.Anything, "Nirvana", "Nevermind").
			Return(&metadata.CoverHit{
				ArtistName: "NIRVANA", CoverURL: "https://cdn.example/s.jpg",
			}, nil).Once()
		posters.EXPECT().
			Fetch(mock.Anything, "albums", uint32(7), "https://cdn.example/s.jpg").
			Return(nil).Once()

		svc.resolveCover(ctx, album)
	})

	It("rejects a search hit by another artist and uses the archive", func() {
		deezer.EXPECT().CoverByUPC(mock.Anything, mock.Anything).
			Return("", nil).Once()
		deezer.EXPECT().SearchCover(mock.Anything, "Nirvana", "Nevermind").
			Return(&metadata.CoverHit{
				ArtistName: "Nirvana Tribute Band",
				CoverURL:   "https://cdn.example/x.jpg",
			}, nil).Once()
		posters.EXPECT().
			Fetch(mock.Anything, "albums", uint32(7), metadata.CoverArtURL("rg-7")).
			Return(nil).Once()

		svc.resolveCover(ctx, album)
	})

	It("rejects a search hit whose name normalizes to nothing", func() {
		album.Edges.Artist.Name = "坂本龍一"
		deezer.EXPECT().CoverByUPC(mock.Anything, mock.Anything).
			Return("", nil).Once()
		deezer.EXPECT().SearchCover(mock.Anything, "坂本龍一", "Nevermind").
			Return(&metadata.CoverHit{
				ArtistName: "宇多田ヒカル",
				CoverURL:   "https://cdn.example/x.jpg",
			}, nil).Once()
		posters.EXPECT().
			Fetch(mock.Anything, "albums", uint32(7), metadata.CoverArtURL("rg-7")).
			Return(nil).Once()

		svc.resolveCover(ctx, album)
	})

	It("falls through a failing source to the next", func() {
		deezer.EXPECT().CoverByUPC(mock.Anything, mock.Anything).
			Return("", errors.New("deezer down")).Once()
		deezer.EXPECT().SearchCover(mock.Anything, mock.Anything, mock.Anything).
			Return(&metadata.CoverHit{
				ArtistName: "Nirvana", CoverURL: "https://cdn.example/s.jpg",
			}, nil).Once()
		posters.EXPECT().
			Fetch(mock.Anything, "albums", uint32(7), "https://cdn.example/s.jpg").
			Return(errors.New("cdn 403")).Once()
		posters.EXPECT().
			Fetch(mock.Anything, "albums", uint32(7), metadata.CoverArtURL("rg-7")).
			Return(nil).Once()

		svc.resolveCover(ctx, album)
	})

	It("ends quietly when no source has a cover", func() {
		deezer.EXPECT().CoverByUPC(mock.Anything, mock.Anything).
			Return("", nil).Once()
		deezer.EXPECT().SearchCover(mock.Anything, mock.Anything, mock.Anything).
			Return(nil, nil).Once()
		posters.EXPECT().
			Fetch(mock.Anything, "albums", uint32(7), metadata.CoverArtURL("rg-7")).
			Return(errors.New("status 404")).Once()

		svc.resolveCover(ctx, album)
	})

	It("leaves a cached cover alone when the album has no local artwork", func() {
		cache(7)

		svc.resolveCover(ctx, album)
	})

	It("skips Deezer entirely when no provider is wired", func() {
		svc.covers = nil
		posters.EXPECT().
			Fetch(mock.Anything, "albums", uint32(7), metadata.CoverArtURL("rg-7")).
			Return(nil).Once()

		svc.resolveCover(ctx, album)
	})
})

func copyFile(src, dst string) {
	GinkgoHelper()
	b, err := os.ReadFile(src)
	Expect(err).NotTo(HaveOccurred())
	//nolint:gosec // dst is under the spec's TempDir
	Expect(os.WriteFile(dst, b, 0o600)).To(Succeed())
}
