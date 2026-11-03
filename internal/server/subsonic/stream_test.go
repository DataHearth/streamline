package subsonic

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/internal/appaccess"
	postersmocks "github.com/datahearth/streamline/internal/posters/mocks"
)

var _ = g.Describe("stream", g.Label("integration"), func() {
	var (
		f       *fixture
		posters *postersmocks.MockManager
		audio   = []byte("0123456789abcdef")
	)

	g.BeforeEach(func() {
		ctx := context.Background()
		f = newFixture(ctx)
		path := filepath.Join(g.GinkgoT().TempDir(), "01.flac")
		Expect(os.WriteFile(path, audio, 0o600)).To(Succeed())
		Expect(f.client.MediaFile.Update().SetPath(path).Exec(ctx)).To(Succeed())

		posters = postersmocks.NewMockManager(g.GinkgoT())
		f.handler = New(Deps{
			Ent:     f.client,
			Posters: posters,
			Tracker: appaccess.NewTracker(f.client),
		}).Routes()
	})

	serve := func(endpoint string, id string, hdr http.Header) *httptest.ResponseRecorder {
		g.GinkgoHelper()
		q := signedQuery("sesame", url.Values{"id": {id}})
		req := httptest.NewRequest(http.MethodGet, "/"+endpoint+"?"+q.Encode(), nil)
		maps.Copy(req.Header, hdr)
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		return rec
	}

	g.It("streams the whole file with range support advertised", func() {
		rec := serve("stream", fmt.Sprintf("tr-%d", f.withFile.ID), nil)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.Bytes()).To(Equal(audio))
		Expect(rec.Header().Get("Content-Type")).To(Equal("audio/flac"))
		Expect(rec.Header().Get("Accept-Ranges")).To(Equal("bytes"))
	})

	g.It("answers a range request with 206", func() {
		rec := serve(
			"stream",
			fmt.Sprintf("tr-%d", f.withFile.ID),
			http.Header{"Range": {"bytes=0-3"}},
		)
		Expect(rec.Code).To(Equal(http.StatusPartialContent))
		Expect(rec.Body.Bytes()).To(Equal(audio[:4]))
	})

	g.It("answers 70 for a track with no file", func() {
		rec := serve("stream", fmt.Sprintf("tr-%d", f.noFile.ID), nil)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(ContainSubstring(`"code":70`))
	})

	g.It("answers 70 when the file is gone from disk", func() {
		Expect(
			os.Remove(f.client.MediaFile.Query().OnlyX(context.Background()).Path),
		).To(Succeed())
		rec := serve("stream", fmt.Sprintf("tr-%d", f.withFile.ID), nil)
		Expect(rec.Body.String()).To(ContainSubstring(`"code":70`))
	})

	g.It("delegates album cover art to the posters manager", func() {
		posters.EXPECT().
			Serve(mock.Anything, mock.Anything, "albums", f.album.ID).
			Once()
		serve("getCoverArt", fmt.Sprintf("al-%d", f.album.ID), nil)
	})

	g.It("delegates artist cover art to the posters manager", func() {
		posters.EXPECT().
			Serve(mock.Anything, mock.Anything, "artists", f.artist.ID).
			Once()
		serve("getCoverArt", fmt.Sprintf("ar-%d", f.artist.ID), nil)
	})

	g.It("serves a track's cover art from its album", func() {
		posters.EXPECT().
			Serve(mock.Anything, mock.Anything, "albums", f.album.ID).
			Once()
		serve("getCoverArt", fmt.Sprintf("tr-%d", f.withFile.ID), nil)
	})

	g.Context("for a playlist", func() {
		solid := func(c color.Color, w, h int) string {
			g.GinkgoHelper()
			img := image.NewRGBA(image.Rect(0, 0, w, h))
			for y := range h {
				for x := range w {
					img.Set(x, y, c)
				}
			}
			path := filepath.Join(g.GinkgoT().TempDir(), "poster.png")
			out, err := os.Create(path)
			Expect(err).NotTo(HaveOccurred())
			defer out.Close()
			Expect(png.Encode(out, img)).To(Succeed())
			return path
		}
		playlist := func(songs ...string) string {
			g.GinkgoHelper()
			p := obj(okResp(f.call("createPlaylist", url.Values{
				"name": {"Mix"}, "songId": songs,
			})), "playlist")
			Expect(p["coverArt"]).To(Equal(p["id"]))
			return p["id"].(string)
		}

		g.It("draws two albums' covers on the diagonals of a grid", func() {
			other := f.addAlbum(
				f.artist,
				"rg-other",
				"Other",
				"/music/other/01.flac",
			)
			otherTrack := f.client.Track.Query().
				Where(track.HasAlbumWith(album.IDEQ(other.ID))).
				OnlyX(context.Background())
			red := color.RGBA{R: 255, A: 255}
			blue := color.RGBA{B: 255, A: 255}
			posters.EXPECT().Path("albums", f.album.ID).
				Return(solid(red, 400, 300)).Once()
			posters.EXPECT().Path("albums", other.ID).
				Return(solid(blue, 100, 100)).Once()
			id := playlist(tr(f.withFile), tr(otherTrack), tr(f.withFile))

			rec := serve("getCoverArt", id, nil)
			Expect(rec.Header().Get("Content-Type")).To(Equal("image/jpeg"))
			img, err := jpeg.Decode(bytes.NewReader(rec.Body.Bytes()))
			Expect(err).NotTo(HaveOccurred())
			Expect(img.Bounds().Size()).To(Equal(image.Pt(600, 600)))
			isRed := func(x, y int) bool {
				r, _, b, _ := img.At(x, y).RGBA()
				return r > b
			}
			Expect([]bool{
				isRed(150, 150), isRed(450, 150), isRed(150, 450), isRed(450, 450),
			}).To(Equal([]bool{true, false, false, true}))
		})

		g.It("serves the one album's cover as it is", func() {
			posters.EXPECT().Path("albums", f.album.ID).
				Return(solid(color.Black, 10, 10)).Once()
			posters.EXPECT().
				Serve(mock.Anything, mock.Anything, "albums", f.album.ID).
				Once()
			serve("getCoverArt", playlist(tr(f.withFile)), nil)
		})
	})

	g.It("answers 70 for cover art of a track that does not exist", func() {
		rec := serve("getCoverArt", "tr-999999", nil)
		Expect(rec.Body.String()).To(ContainSubstring(`"code":70`))
	})

	g.It("answers 10 for cover art without an id", func() {
		rec := serve("getCoverArt", "", nil)
		Expect(rec.Body.String()).To(ContainSubstring(`"code":10`))
	})

	g.It("answers 70 for a malformed cover art id", func() {
		rec := serve("getCoverArt", "nonsense", nil)
		Expect(rec.Body.String()).To(ContainSubstring(`"code":70`))
	})
})
