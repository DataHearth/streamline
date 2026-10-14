package music

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/posters"
)

func solidImage(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, c)
		}
	}
	return img
}

func pngOf(w, h int, c color.Color) []byte {
	GinkgoHelper()
	var b bytes.Buffer
	Expect(png.Encode(&b, solidImage(w, h, c))).To(Succeed())
	return b.Bytes()
}

func jpegBytes(w, h int, c color.Color) []byte {
	GinkgoHelper()
	var b bytes.Buffer
	Expect(jpeg.Encode(&b, solidImage(w, h, c), nil)).To(Succeed())
	return b.Bytes()
}

var _ = Describe("embeddableCover", Label("unit", "music"), func() {
	It("keeps a cover already small enough as it is", func() {
		small := jpegBytes(300, 300, color.White)
		got, err := embeddableCover(small, 500)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(small))
	})

	It("scales a larger one down to a JPEG of the long side asked for", func() {
		got, err := embeddableCover(
			pngOf(1200, 600, color.RGBA{R: 200, A: 255}),
			500,
		)
		Expect(err).NotTo(HaveOccurred())
		cfg, format, err := image.DecodeConfig(bytes.NewReader(got))
		Expect(err).NotTo(HaveOccurred())
		Expect(format).To(Equal("jpeg"))
		Expect([]int{cfg.Width, cfg.Height}).To(Equal([]int{500, 250}))

		img, _, err := image.Decode(bytes.NewReader(got))
		Expect(err).NotTo(HaveOccurred())
		r, g, b, _ := img.At(250, 125).RGBA()
		Expect(r >> 8).To(BeNumerically("~", 200, 4))
		Expect(g >> 8).To(BeNumerically("<", 8))
		Expect(b >> 8).To(BeNumerically("<", 8))
	})

	It("refuses a format players do not all read from a tag", func() {
		var b bytes.Buffer
		Expect(gif.Encode(&b, solidImage(10, 10, color.White), nil)).To(Succeed())
		_, err := embeddableCover(b.Bytes(), 500)
		Expect(err).To(MatchError(errCoverUndecodable))
	})
})

var _ = Describe("Music tag artwork", Label("unit", "integration", "music"), func() {
	var (
		e        *env
		musicDir string
		cache    posters.Manager
		artist   *ent.Artist
		alb      *ent.Album
		tr       *ent.Track
		albumDir string
	)

	tagConfig := func(tags map[string]any) {
		GinkgoHelper()
		e.setConfig(map[string]any{"library": map[string]any{
			"music_path":        musicDir,
			"download_path":     GinkgoT().TempDir(),
			"no_match_cooldown": "6h",
			"max_grab_failures": 3,
			"music_tags":        tags,
		}})
	}

	cacheCover := func(kind string, id uint32, data []byte) {
		GinkgoHelper()
		Expect(cache.Put(e.ctx, kind, id, bytes.NewReader(data))).To(Succeed())
	}

	placeFile := func(name string) *ent.MediaFile {
		GinkgoHelper()
		data, err := os.ReadFile("../../library/audiotags/testdata/tagged.flac")
		Expect(err).NotTo(HaveOccurred())
		path := filepath.Join(albumDir, name)
		Expect(os.MkdirAll(albumDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(path, data, 0o644)).To(Succeed())
		return e.client.MediaFile.Create().
			SetPath(path).SetSize(int64(len(data))).SetFormat("flac").
			SetTrackID(tr.ID).SetTagsState(entmediafile.TagsStatePending).
			SaveX(e.ctx)
	}

	embedded := func(mf *ent.MediaFile) []byte {
		GinkgoHelper()
		data, _, err := audiotags.Picture(mf.Path)
		Expect(err).NotTo(HaveOccurred())
		return data
	}

	sync := func() {
		GinkgoHelper()
		Expect(e.svc.SyncTags(manualCtx(e.ctx))).To(Succeed())
	}

	requeue := func() {
		GinkgoHelper()
		_, err := e.store.MarkAllMusicTags(e.ctx, db.MarkForce)
		Expect(err).NotTo(HaveOccurred())
	}

	reloadAlbum := func() *ent.Album { return e.client.Album.GetX(e.ctx, alb.ID) }

	findAlbum := func() *ent.Album {
		GinkgoHelper()
		a, err := e.store.FindAlbumByID(e.ctx, alb.ID)
		Expect(err).NotTo(HaveOccurred())
		return a
	}

	BeforeEach(func() {
		e = newEnv(false)
		var err error
		cache, err = posters.New(GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		e.svc.posters = cache
		e.dl.EXPECT().SeedingTorrents(mock.Anything).
			Return(download.NewSeedingSet(true, nil, nil)).Maybe()
		musicDir = GinkgoT().TempDir()
		tagConfig(map[string]any{"mode": config.MusicTagsImport, "art_max_px": 500})
		var albums []*ent.Album
		artist, albums = e.seedArtist("Nirvana",
			albumSeed{mbid: "rg-1", title: "Nevermind", tracks: []string{"Lithium"}})
		alb = albums[0]
		tr = alb.QueryTracks().OnlyX(e.ctx)
		albumDir = filepath.Join(musicDir, "Nirvana", "Nevermind (1991)")
	})

	It(
		"embeds the cached cover scaled down, and leaves it and the artist's photo beside the files",
		func() {
			cover := pngOf(800, 800, color.RGBA{G: 255, A: 255})
			photo := jpegBytes(400, 400, color.RGBA{B: 255, A: 255})
			cacheCover(coverKind, alb.ID, cover)
			cacheCover(artistKind, artist.ID, photo)
			mf := placeFile("01.flac")

			sync()

			got := e.client.MediaFile.GetX(e.ctx, mf.ID)
			Expect(*got.TagsState).To(Equal(entmediafile.TagsStateWritten))
			art := embedded(mf)
			cfg, format, err := image.DecodeConfig(bytes.NewReader(art))
			Expect(err).NotTo(HaveOccurred())
			Expect(format).To(Equal("jpeg"))
			Expect(cfg.Width).To(Equal(500))
			Expect(got.TagsArtHash).To(Equal(audiotags.ArtHash(art)))

			side, err := os.ReadFile(filepath.Join(albumDir, "cover.png"))
			Expect(err).NotTo(HaveOccurred())
			Expect(side).To(Equal(cover))
			Expect(reloadAlbum().ArtSidecarHash).To(Equal(audiotags.ArtHash(cover)))
			folder, err := os.ReadFile(
				filepath.Join(musicDir, "Nirvana", "folder.jpg"),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(folder).To(Equal(photo))
			Expect(e.client.Artist.GetX(e.ctx, artist.ID).ArtSidecarHash).
				To(Equal(audiotags.ArtHash(photo)))
		},
	)

	It(
		"replaces its own cover in the file and beside it when the cover changes",
		func() {
			cacheCover(coverKind, alb.ID, jpegBytes(300, 300, color.White))
			mf := placeFile("01.flac")
			sync()

			next := jpegBytes(300, 300, color.Black)
			cacheCover(coverKind, alb.ID, next)
			requeue()
			sync()

			Expect(embedded(mf)).To(Equal(next))
			side, err := os.ReadFile(filepath.Join(albumDir, "cover.jpg"))
			Expect(err).NotTo(HaveOccurred())
			Expect(side).To(Equal(next))
		},
	)

	It("never replaces a cover file or a front cover it did not write", func() {
		own := jpegBytes(64, 64, color.RGBA{R: 255, A: 255})
		mf := placeFile("01.flac")
		_, err := audiotags.WriteWithArt(
			mf.Path,
			audiotags.Tags{},
			audiotags.Policy{},
			audiotags.Art{Manage: true, Front: own},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(
			os.WriteFile(filepath.Join(albumDir, "Folder.JPG"), own, 0o644),
		).To(Succeed())
		cacheCover(coverKind, alb.ID, jpegBytes(300, 300, color.White))

		sync()

		Expect(embedded(mf)).To(Equal(own))
		Expect(e.client.MediaFile.GetX(e.ctx, mf.ID).TagsArtHash).To(BeEmpty())
		Expect(filepath.Join(albumDir, "cover.jpg")).NotTo(BeAnExistingFile())
		Expect(reloadAlbum().ArtSidecarHash).To(BeEmpty())
	})

	It("removes only its own art when the settings turn it off", func() {
		cacheCover(coverKind, alb.ID, jpegBytes(300, 300, color.White))
		mf := placeFile("01.flac")
		sync()
		Expect(filepath.Join(albumDir, "cover.jpg")).To(BeAnExistingFile())

		tagConfig(map[string]any{
			"mode": config.MusicTagsImport, "embed_art": false, "sidecar_art": false,
		})
		requeue()
		sync()

		Expect(embedded(mf)).To(BeNil())
		Expect(e.client.MediaFile.GetX(e.ctx, mf.ID).TagsArtHash).To(BeEmpty())
		Expect(filepath.Join(albumDir, "cover.jpg")).NotTo(BeAnExistingFile())
		Expect(reloadAlbum().ArtSidecarHash).To(BeEmpty())
	})

	It("writes no artist image outside a folder named after the artist", func() {
		albumDir = filepath.Join(musicDir, "Nirvana - Nevermind")
		cacheCover(coverKind, alb.ID, jpegBytes(300, 300, color.White))
		cacheCover(artistKind, artist.ID, jpegBytes(100, 100, color.White))
		placeFile("01.flac")

		sync()

		Expect(filepath.Join(albumDir, "cover.jpg")).To(BeAnExistingFile())
		Expect(filepath.Join(musicDir, "folder.jpg")).NotTo(BeAnExistingFile())
	})

	It("never takes its own copies back as the album's art", func() {
		cover := pngOf(800, 800, color.RGBA{G: 255, A: 255})
		cacheCover(coverKind, alb.ID, cover)
		placeFile("01.flac")
		sync()

		e.svc.resolveCover(e.ctx, findAlbum())

		data, err := os.ReadFile(cache.Path(coverKind, alb.ID))
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(Equal(cover))
	})

	It("queues a managed album when its cover changes", func() {
		tagConfig(map[string]any{"mode": config.MusicTagsManaged})
		cacheCover(coverKind, alb.ID, jpegBytes(300, 300, color.White))
		mf := placeFile("01.flac")
		sync()
		Expect(os.WriteFile(filepath.Join(albumDir, "front.jpg"),
			jpegBytes(64, 64, color.Black), 0o644)).To(Succeed())

		e.svc.resolveCover(e.ctx, findAlbum())

		Expect(*e.client.MediaFile.GetX(e.ctx, mf.ID).TagsState).
			To(Equal(entmediafile.TagsStatePending))
	})
})
