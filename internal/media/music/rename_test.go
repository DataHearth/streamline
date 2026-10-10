package music

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/library"
	msmocks "github.com/datahearth/streamline/internal/mediaserver/mocks"
)

var _ = Describe("Renamer", Label("unit", "integration", "music"), func() {
	var (
		e      *env
		r      *Renamer
		artist *ent.Artist
		albums []*ent.Album
	)

	root := func() string { return config.Get().Library.MusicPath }

	write := func(path string) {
		GinkgoHelper()
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, []byte("x"), 0o600)).To(Succeed())
	}

	BeforeEach(func() {
		e = newEnv(false)
		r = NewRenamer(e.store, nil)
		artist, albums = e.seedArtist("Nirvana", albumSeed{
			mbid: "rg-1", title: "Nevermind", monitored: true,
			date:   new(time.Date(1991, 9, 24, 0, 0, 0, 0, time.UTC)),
			tracks: []string{"Drain You", "Lithium"},
		})
	})

	target := func(file string) string {
		return filepath.Join(root(), "Nirvana", "Nevermind (1991)", file)
	}

	It("plans a move for every file whose path differs from the template", func() {
		tracks := albums[0].QueryTracks().AllX(e.ctx)
		stray := filepath.Join(root(), "dump", "a.flac")
		right := target("02 - Lithium.flac")
		e.addFile(tracks[0], stray, "lossless")
		e.addFile(tracks[1], right, "lossless")

		plan, err := r.Preview(e.ctx, artist.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(HaveLen(1))
		Expect(plan.Operations[0].From).To(Equal(stray))
		Expect(plan.Operations[0].To).To(Equal(target("01 - Drain You.flac")))
	})

	It("plans nothing when every file is where the template puts it", func() {
		tracks := albums[0].QueryTracks().AllX(e.ctx)
		e.addFile(tracks[0], target("01 - Drain You.flac"), "lossless")

		plan, err := r.Preview(e.ctx, artist.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(BeEmpty())
	})

	It("prefixes the disc number on a multi-disc album", func() {
		e.client.Track.Update().Where().SetDisc(1).ExecX(e.ctx)
		tracks := albums[0].QueryTracks().AllX(e.ctx)
		e.client.Track.UpdateOneID(tracks[1].ID).
			SetDisc(2).
			SetPosition(1).
			ExecX(e.ctx)
		e.addFile(tracks[1], filepath.Join(root(), "x.flac"), "lossless")

		plan, err := r.Preview(e.ctx, artist.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations[0].To).To(Equal(target("2-01 - Lithium.flac")))
	})

	It("keeps each file's own extension", func() {
		tracks := albums[0].QueryTracks().AllX(e.ctx)
		e.addFile(tracks[0], filepath.Join(root(), "x.MP3"), "high")
		plan, err := r.Preview(e.ctx, artist.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations[0].To).To(Equal(target("01 - Drain You.mp3")))
	})

	It("asks the media servers to rescan the music root after a move", func() {
		ms := msmocks.NewMockRefresher(GinkgoT())
		r = NewRenamer(e.store, ms)
		done := make(chan struct{})
		ms.EXPECT().RefreshAll(mock.Anything, "music", root()).
			RunAndReturn(func(context.Context, string, string) error {
				close(done)
				return nil
			}).Once()
		tracks := albums[0].QueryTracks().AllX(e.ctx)
		from := filepath.Join(root(), "dump", "a.flac")
		write(from)
		e.addFile(tracks[0], from, "lossless")

		_, err := r.Apply(e.ctx, artist.ID)
		Expect(err).NotTo(HaveOccurred())
		Eventually(done).Should(BeClosed())
	})

	It("moves the files, updates their rows and prunes what it empties", func() {
		tracks := albums[0].QueryTracks().AllX(e.ctx)
		from := filepath.Join(root(), "dump", "deep", "a.flac")
		write(from)
		mf := e.addFile(tracks[0], from, "lossless")

		plan, err := r.Apply(e.ctx, artist.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(HaveLen(1))

		to := target("01 - Drain You.flac")
		Expect(to).To(BeAnExistingFile())
		Expect(from).NotTo(BeAnExistingFile())
		Expect(filepath.Join(root(), "dump")).NotTo(BeADirectory())
		Expect(e.client.MediaFile.GetX(e.ctx, mf.ID).Path).To(Equal(to))

		again, err := r.Preview(e.ctx, artist.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(again.Operations).To(BeEmpty())
	})

	It("stops at the first collision and reports it", func() {
		tracks := albums[0].QueryTracks().AllX(e.ctx)
		first := filepath.Join(root(), "dump", "a.flac")
		second := filepath.Join(root(), "dump", "b.flac")
		write(first)
		write(second)
		blocker := target("01 - Drain You.flac")
		write(blocker)
		e.addFile(tracks[0], first, "lossless")
		e.addFile(tracks[1], second, "lossless")

		_, err := r.Apply(e.ctx, artist.ID)
		Expect(err).To(MatchError(library.ErrDestExists))
		Expect(first).To(BeAnExistingFile())
		Expect(string(mustRead(blocker))).To(Equal("x"))
	})

	It("treats a file already at its target as no collision", func() {
		tracks := albums[0].QueryTracks().AllX(e.ctx)
		to := target("01 - Drain You.flac")
		write(to)
		e.addFile(tracks[0], to, "lossless")
		plan, err := r.Apply(e.ctx, artist.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(BeEmpty())
	})

	It("maps a missing artist to ErrArtistNotFound", func() {
		_, err := r.Preview(e.ctx, 999)
		Expect(err).To(MatchError(ErrArtistNotFound))
		_, err = r.Apply(e.ctx, 999)
		Expect(err).To(MatchError(ErrArtistNotFound))
	})
})

func mustRead(path string) []byte {
	GinkgoHelper()
	b, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	return b
}
