package music

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/internal/config"
)

var _ = Describe("DeleteTrackFile", Label("unit", "integration", "music"), func() {
	var (
		e      *env
		albums []*ent.Album
		tr     *ent.Track
	)

	write := func(path string) {
		GinkgoHelper()
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, []byte("x"), 0o600)).To(Succeed())
	}

	BeforeEach(func() {
		e = newEnv(false)
		_, albums = e.seedArtist("Nirvana",
			albumSeed{mbid: "rg-1", title: "Nevermind", tracks: []string{"A", "B"}})
		e.client.Album.UpdateOneID(albums[0].ID).
			SetStatus(album.StatusAvailable).
			ExecX(e.ctx)
		tr = albums[0].QueryTracks().FirstX(e.ctx)
	})

	root := func() string { return config.Get().Library.MusicPath }

	It("removes the file and its row and puts the album back to wanted", func() {
		path := filepath.Join(root(), "Nirvana", "Nevermind", "01.flac")
		write(path)
		e.addFile(tr, path, "lossless")

		Expect(e.svc.DeleteTrackFile(e.ctx, tr.ID)).To(Succeed())

		Expect(path).NotTo(BeAnExistingFile())
		Expect(e.client.MediaFile.Query().Count(e.ctx)).To(BeZero())
		Expect(
			e.client.Album.GetX(e.ctx, albums[0].ID).Status,
		).To(Equal(album.StatusWanted))
	})

	It("removes every file of the track", func() {
		first := filepath.Join(root(), "Nirvana", "Nevermind", "01.flac")
		second := filepath.Join(root(), "Nirvana", "Nevermind", "01.mp3")
		write(first)
		write(second)
		e.addFile(tr, first, "lossless")
		e.addFile(tr, second, "high")

		Expect(e.svc.DeleteTrackFile(e.ctx, tr.ID)).To(Succeed())
		Expect(first).NotTo(BeAnExistingFile())
		Expect(second).NotTo(BeAnExistingFile())
	})

	It("leaves an album that is not available alone", func() {
		e.client.Album.UpdateOneID(albums[0].ID).
			SetStatus(album.StatusDownloading).
			ExecX(e.ctx)
		path := filepath.Join(root(), "Nirvana", "01.flac")
		write(path)
		e.addFile(tr, path, "lossless")

		Expect(e.svc.DeleteTrackFile(e.ctx, tr.ID)).To(Succeed())
		Expect(e.client.Album.GetX(e.ctx, albums[0].ID).Status).
			To(Equal(album.StatusDownloading))
	})

	It("deletes nothing when a file lies outside the music root", func() {
		inside := filepath.Join(root(), "Nirvana", "01.flac")
		outside := filepath.Join(GinkgoT().TempDir(), "02.flac")
		write(inside)
		write(outside)
		e.addFile(tr, inside, "lossless")
		e.addFile(tr, outside, "lossless")

		err := e.svc.DeleteTrackFile(e.ctx, tr.ID)
		Expect(err).To(MatchError(ErrOutsideRoot))
		Expect(inside).To(BeAnExistingFile())
		Expect(outside).To(BeAnExistingFile())
		Expect(e.client.MediaFile.Query().Count(e.ctx)).To(Equal(2))
		Expect(
			e.client.Album.GetX(e.ctx, albums[0].ID).Status,
		).To(Equal(album.StatusAvailable))
	})

	It("maps a missing track to ErrTrackNotFound", func() {
		Expect(e.svc.DeleteTrackFile(e.ctx, 999)).To(MatchError(ErrTrackNotFound))
	})
})
