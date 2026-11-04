package music

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/upload"
)

var _ = Describe("uploads", Label("unit", "integration", "music"), func() {
	var e *env

	BeforeEach(func() {
		e = newEnv(false)
	})

	// stage writes the named files under a fresh dir and returns the receive
	// func handing them over, plus whether it was ever called.
	stage := func(names ...string) (ReceiveFunc, *bool, string) {
		GinkgoHelper()
		dir := GinkgoT().TempDir()
		called := false
		recv := func(context.Context, upload.Kind) (*upload.Staged, error) {
			called = true
			s := &upload.Staged{Dir: dir}
			for _, n := range names {
				full := filepath.Join(dir, filepath.FromSlash(n))
				Expect(os.MkdirAll(filepath.Dir(full), 0o700)).To(Succeed())
				Expect(os.WriteFile(full, []byte("x"), 0o600)).To(Succeed())
				s.Files = append(s.Files, upload.File{Path: n, Size: 1})
			}
			return s, nil
		}
		return recv, &called, dir
	}

	exists := func(dir string, name string) bool {
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
		return err == nil
	}

	var al *ent.Album

	seedAlbum := func() {
		GinkgoHelper()
		_, albums := e.seedArtist("Avatar", albumSeed{
			mbid:   "rg-ff",
			title:  "Feathers & Flesh",
			tracks: []string{"Regret", "Intro"},
		})
		al = albums[0]
	}

	Describe("UploadAlbum", func() {
		It("refuses a busy album before reading a byte", func() {
			seedAlbum()
			_, err := e.store.CreateDownloadRecord(
				e.ctx,
				db.CreateDownloadRecordParams{
					Title:   "t",
					Status:  downloadrecord.StatusDownloading,
					AlbumID: al.ID,
				},
			)
			Expect(err).NotTo(HaveOccurred())

			recv, called, _ := stage("01.flac")
			_, err = e.svc.UploadAlbum(e.ctx, al.ID, UploadOptions{}, recv)
			Expect(err).To(MatchError(upload.ErrConflict))
			Expect(*called).To(BeFalse())
		})

		It("files an uploaded record and marks the album downloading", func() {
			seedAlbum()
			e.client.Album.UpdateOneID(al.ID).
				SetStatus(album.StatusWanted).
				ExecX(e.ctx)
			recv, _, dir := stage("01 - Regret.flac")

			rec, err := e.svc.UploadAlbum(e.ctx, al.ID, UploadOptions{}, recv)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Uploaded).To(BeTrue())
			Expect(rec.SavePath).To(Equal(dir))
			Expect(rec.Title).To(Equal("Upload"))
			Expect(rec.Status).To(Equal(downloadrecord.StatusImporting))
			Expect(rec.ReplaceMode).NotTo(Equal(downloadrecord.ReplaceModeAll))
			linked := e.client.DownloadRecord.GetX(e.ctx, rec.ID).
				QueryAlbum().OnlyX(e.ctx)
			Expect(linked.ID).To(Equal(al.ID))
			Expect(e.client.Album.GetX(e.ctx, al.ID).Status).
				To(Equal(album.StatusDownloading))
		})

		It("carries the title and replace mode", func() {
			seedAlbum()
			recv, _, _ := stage("01.flac")
			rec, err := e.svc.UploadAlbum(
				e.ctx, al.ID, UploadOptions{Replace: true, Title: "Feathers"}, recv,
			)
			Expect(err).NotTo(HaveOccurred())
			got := e.client.DownloadRecord.GetX(e.ctx, rec.ID)
			Expect(got.Title).To(Equal("Feathers"))
			Expect(got.ReplaceMode).To(Equal(downloadrecord.ReplaceModeAll))
		})

		It("lets one of several simultaneous uploads through", func() {
			seedAlbum()
			results := make(chan error, 8)
			var wg sync.WaitGroup
			for range cap(results) {
				recv, _, _ := stage("01.flac")
				wg.Go(func() {
					_, err := e.svc.UploadAlbum(e.ctx, al.ID, UploadOptions{}, recv)
					results <- err
				})
			}
			wg.Wait()
			close(results)

			var ok, busy int
			for err := range results {
				switch {
				case err == nil:
					ok++
				case errors.Is(err, upload.ErrConflict):
					busy++
				}
			}
			Expect(ok).To(Equal(1))
			Expect(busy).To(Equal(cap(results) - 1))
			Expect(e.client.DownloadRecord.Query().CountX(e.ctx)).To(Equal(1))
		})

		It("leaves no record and the album untouched when the body fails", func() {
			seedAlbum()
			e.client.Album.UpdateOneID(al.ID).
				SetStatus(album.StatusWanted).
				ExecX(e.ctx)
			lost := errors.New("connection reset")

			_, err := e.svc.UploadAlbum(e.ctx, al.ID, UploadOptions{},
				func(context.Context, upload.Kind) (*upload.Staged, error) {
					return nil, lost
				})

			Expect(err).To(MatchError(lost))
			Expect(e.client.DownloadRecord.Query().CountX(e.ctx)).To(BeZero())
			Expect(
				e.client.Album.GetX(e.ctx, al.ID).Status,
			).To(Equal(album.StatusWanted))
		})

		It("maps a missing album to ErrAlbumNotFound", func() {
			recv, called, _ := stage("01.flac")
			_, err := e.svc.UploadAlbum(e.ctx, 999, UploadOptions{}, recv)
			Expect(err).To(MatchError(ErrAlbumNotFound))
			Expect(*called).To(BeFalse())
		})
	})

	Describe("UploadTrack", func() {
		var tr *ent.Track

		BeforeEach(func() {
			seedAlbum()
			tr = e.client.Track.Query().
				Where(track.HasAlbumWith(album.IDEQ(al.ID)), track.Position(1)).
				OnlyX(e.ctx)
		})

		It("pins the record to the track", func() {
			recv, _, _ := stage("anything.flac")
			rec, err := e.svc.UploadTrack(e.ctx, tr.ID, UploadOptions{}, recv)
			Expect(err).NotTo(HaveOccurred())
			got := e.client.DownloadRecord.GetX(e.ctx, rec.ID)
			Expect(got.QueryTrack().OnlyX(e.ctx).ID).To(Equal(tr.ID))
			Expect(got.QueryAlbum().OnlyX(e.ctx).ID).To(Equal(al.ID))
		})

		It("refuses a track holding a file without replace", func() {
			e.addFile(tr, "/music/x/01.flac", "lossless")
			recv, _, dir := stage("01.flac")
			_, err := e.svc.UploadTrack(e.ctx, tr.ID, UploadOptions{}, recv)
			Expect(err).To(MatchError(upload.ErrRejected))
			Expect(exists(dir, "01.flac")).To(BeFalse())
		})

		It("takes a replacement when asked", func() {
			e.addFile(tr, "/music/x/01.flac", "lossless")
			recv, _, _ := stage("01.flac")
			_, err := e.svc.UploadTrack(
				e.ctx,
				tr.ID,
				UploadOptions{Replace: true},
				recv,
			)
			Expect(err).NotTo(HaveOccurred())
		})

		It("refuses two files", func() {
			recv, _, dir := stage("01.flac", "02.flac")
			_, err := e.svc.UploadTrack(e.ctx, tr.ID, UploadOptions{}, recv)
			Expect(err).To(MatchError(upload.ErrRejected))
			Expect(exists(dir, "01.flac")).To(BeFalse())
		})

		It("maps a missing track to ErrTrackNotFound", func() {
			recv, _, _ := stage("01.flac")
			_, err := e.svc.UploadTrack(e.ctx, 9999, UploadOptions{}, recv)
			Expect(err).To(MatchError(ErrTrackNotFound))
		})
	})

	Describe("UploadArtist", func() {
		var (
			artist *ent.Artist
			a, b   *ent.Album
		)

		BeforeEach(func() {
			date := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
			var albums []*ent.Album
			artist, albums = e.seedArtist("Avatar",
				albumSeed{
					mbid: "rg-a", title: "Album A", date: &date,
					tracks: []string{"One"},
				},
				albumSeed{mbid: "rg-b", title: "Album B", monitored: true},
			)
			a, b = albums[0], albums[1]
		})

		It("links the matched albums and drops the other folders", func() {
			recv, _, dir := stage(
				"Album A (2001)/01.flac",
				"Album B/CD1/01.flac",
				"Bootleg/01.flac",
			)
			rec, skipped, err := e.svc.UploadArtist(
				e.ctx,
				artist.ID,
				UploadOptions{},
				recv,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(skipped).To(Equal([]string{"Bootleg"}))
			Expect(exists(dir, "Bootleg/01.flac")).To(BeFalse())
			Expect(exists(dir, "Album A (2001)/01.flac")).To(BeTrue())

			got := e.client.DownloadRecord.GetX(e.ctx, rec.ID)
			Expect(got.QueryArtist().OnlyX(e.ctx).ID).To(Equal(artist.ID))
			ids := got.QueryAlbums().IDsX(e.ctx)
			Expect(ids).To(ConsistOf(a.ID, b.ID))
			Expect(got.Uploaded).To(BeTrue())
		})

		It("matches folders under a wrapping discography folder", func() {
			recv, _, _ := stage("Discography/Album A/01.flac")
			rec, skipped, err := e.svc.UploadArtist(
				e.ctx,
				artist.ID,
				UploadOptions{},
				recv,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(skipped).To(BeEmpty())
			Expect(e.client.DownloadRecord.GetX(e.ctx, rec.ID).
				QueryAlbums().IDsX(e.ctx)).To(ConsistOf(a.ID))
		})

		It("rejects an upload matching nothing and removes the staging", func() {
			recv, _, dir := stage("Bootleg/01.flac")
			_, _, err := e.svc.UploadArtist(e.ctx, artist.ID, UploadOptions{}, recv)
			Expect(err).To(MatchError(upload.ErrRejected))
			_, statErr := os.Stat(dir)
			Expect(os.IsNotExist(statErr)).To(BeTrue())
			Expect(e.client.DownloadRecord.Query().CountX(e.ctx)).To(BeZero())
		})

		It("refuses only when an album it names has a download in flight", func() {
			_, err := e.store.CreateDownloadRecord(
				e.ctx,
				db.CreateDownloadRecordParams{
					Title:   "t",
					Status:  downloadrecord.StatusImporting,
					AlbumID: b.ID,
				},
			)
			Expect(err).NotTo(HaveOccurred())

			recv, _, _ := stage("Album A/01.flac")
			_, _, err = e.svc.UploadArtist(e.ctx, artist.ID, UploadOptions{}, recv)
			Expect(err).NotTo(HaveOccurred())

			recv, _, dir := stage("Album B/01.flac")
			_, _, err = e.svc.UploadArtist(e.ctx, artist.ID, UploadOptions{}, recv)
			Expect(err).To(MatchError(upload.ErrConflict))
			Expect(exists(dir, "Album B/01.flac")).To(BeFalse())
		})
	})

	Describe("previews", func() {
		It("reads an album's files like a release's", func() {
			seedAlbum()
			files := []download.ReleaseFile{
				{Path: "01 - Intro.flac", Size: 10},
				{Path: "cover.jpg", Size: 1},
			}
			release := indexer.SearchResult{Title: "x", Download: "https://i/dl"}
			e.dl.EXPECT().
				ReleaseFiles(mock.Anything, release).
				Return(files, nil).
				Once()

			want, err := e.svc.AlbumReleaseFiles(e.ctx, al.ID, release)
			Expect(err).NotTo(HaveOccurred())
			got, err := e.svc.AlbumUploadPreview(e.ctx, al.ID, files)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
			Expect(got.Files[0].TrackID).NotTo(BeZero())
		})

		It("reads a discography against every album of the artist", func() {
			artist, albums := e.seedArtist("Avatar",
				albumSeed{mbid: "rg-a", title: "Album A", tracks: []string{"One"}},
				albumSeed{mbid: "rg-b", title: "Album B"},
			)
			v, err := e.svc.ArtistUploadPreview(
				e.ctx,
				artist.ID,
				[]download.ReleaseFile{
					{Path: "Album A/01 - One.flac", Size: 1},
					{Path: "Album B/01.flac", Size: 1},
					{Path: "Bootleg/01.flac", Size: 1},
				},
			)
			Expect(err).NotTo(HaveOccurred())
			got := map[string]uint32{}
			for _, f := range v.Folders {
				got[f.Path] = f.AlbumID
			}
			Expect(got).To(Equal(map[string]uint32{
				"Album A": albums[0].ID,
				"Album B": albums[1].ID,
				"Bootleg": 0,
			}))
			Expect(v.Files[0].TrackID).NotTo(BeZero())
		})

		It("answers a busy album before anything is sent", func() {
			seedAlbum()
			_, err := e.store.CreateDownloadRecord(
				e.ctx,
				db.CreateDownloadRecordParams{
					Title: "t", Status: downloadrecord.StatusHeld, AlbumID: al.ID,
				},
			)
			Expect(err).NotTo(HaveOccurred())

			_, err = e.svc.AlbumUploadPreview(e.ctx, al.ID, []download.ReleaseFile{
				{Path: "01.flac", Size: 1},
			})
			Expect(err).To(MatchError(upload.ErrConflict))
		})

		It("refuses media of the wrong kind and passes a booklet", func() {
			seedAlbum()
			_, err := e.svc.AlbumUploadPreview(e.ctx, al.ID, []download.ReleaseFile{
				{Path: "01.flac", Size: 1}, {Path: "Booklet.pdf", Size: 1},
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = e.svc.AlbumUploadPreview(e.ctx, al.ID, []download.ReleaseFile{
				{Path: "01.flac", Size: 1}, {Path: "bonus.mkv", Size: 1},
			})
			Expect(err).To(MatchError(upload.ErrRejected))
		})

		It("says when an album's tracks are not known yet", func() {
			seedAlbum()
			e.client.Album.UpdateOneID(al.ID).ClearMetadataFetchedAt().ExecX(e.ctx)

			v, err := e.svc.AlbumUploadPreview(e.ctx, al.ID, []download.ReleaseFile{
				{Path: "01.flac", Size: 1},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(v.TracksPending).To(BeTrue())
		})

		It("answers a busy album only for a discography naming it", func() {
			artist, albums := e.seedArtist("Avatar",
				albumSeed{mbid: "rg-a", title: "Album A"},
				albumSeed{mbid: "rg-b", title: "Album B"},
			)
			_, err := e.store.CreateDownloadRecord(
				e.ctx,
				db.CreateDownloadRecordParams{
					Title:   "t",
					Status:  downloadrecord.StatusImporting,
					AlbumID: albums[1].ID,
				},
			)
			Expect(err).NotTo(HaveOccurred())

			_, err = e.svc.ArtistUploadPreview(
				e.ctx,
				artist.ID,
				[]download.ReleaseFile{
					{Path: "Album A/01.flac", Size: 1},
				},
			)
			Expect(err).NotTo(HaveOccurred())
			_, err = e.svc.ArtistUploadPreview(
				e.ctx,
				artist.ID,
				[]download.ReleaseFile{
					{Path: "Album B/01.flac", Size: 1},
				},
			)
			Expect(err).To(MatchError(upload.ErrConflict))
		})

		It("maps a missing artist to ErrArtistNotFound", func() {
			_, err := e.svc.ArtistUploadPreview(e.ctx, 999, nil)
			Expect(err).To(MatchError(ErrArtistNotFound))
		})
	})
})
