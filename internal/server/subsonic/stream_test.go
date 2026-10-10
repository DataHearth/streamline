package subsonic

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

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

	g.It("answers 70 for cover art of a track id", func() {
		rec := serve("getCoverArt", fmt.Sprintf("tr-%d", f.withFile.ID), nil)
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
