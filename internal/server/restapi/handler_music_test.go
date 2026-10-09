package restapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/media/music"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Handler: Music", Label("unit", "server", "music"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		configtest.SetupFile(map[string]any{
			"music_quality_profiles": []map[string]any{
				{
					"name":    "lossless",
					"formats": []string{"flac"},
					"cutoff":  "flac",
				},
			},
			"music_quality_default_profile": "lossless",
		})
		app = newAPIKeyApp()
	})

	send := func(method, path, key, body string) *http.Response {
		GinkgoHelper()
		var req *http.Request
		if body == "" {
			req = app.req(method, path, key, nil)
		} else {
			req = app.req(method, path, key, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		}
		return app.do(req)
	}

	Describe("SearchMusicArtists", func() {
		It("flags artists already in the library", func() {
			app.metadataMusic.EXPECT().SearchArtists(mock.Anything, "nirvana").
				Return([]metadata.ArtistResult{
					{MBID: "a-1", Name: "Nirvana", SortName: "Nirvana", Score: 100},
					{
						MBID:     "a-2",
						Name:     "Nirvana UK",
						SortName: "Nirvana UK",
						Score:    80,
					},
				}, nil).Once()
			app.store.EXPECT().FindArtistByMBID(mock.Anything, "a-1").
				Return(&ent.Artist{ID: 1}, nil).Once()
			app.store.EXPECT().FindArtistByMBID(mock.Anything, "a-2").
				Return(nil, nil).Once()

			resp := send(
				http.MethodGet,
				"/api/v1/music/search?query=nirvana",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var out MusicArtistSearchResultList
			Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
			Expect(out.Items).To(HaveLen(2))
			Expect(out.Items[0].AlreadyAdded).To(BeTrue())
			Expect(out.Items[1].AlreadyAdded).To(BeFalse())
		})

		It("returns an empty list when nothing matches", func() {
			app.metadataMusic.EXPECT().SearchArtists(mock.Anything, "zzz").
				Return(nil, nil).Once()
			resp := send(
				http.MethodGet,
				"/api/v1/music/search?query=zzz",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var raw map[string]any
			Expect(json.NewDecoder(resp.Body).Decode(&raw)).To(Succeed())
			Expect(raw["items"]).To(Equal([]any{}))
		})
	})

	Describe("DeleteMusicArtist", func() {
		It("passes delete_files through and answers 204", func() {
			app.music.EXPECT().Delete(mock.Anything, uint32(7), true).
				Return(nil).Once()
			resp := send(
				http.MethodDelete,
				"/api/v1/music/artists/7?delete_files=true",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("404s for an unknown artist", func() {
			app.music.EXPECT().Delete(mock.Anything, uint32(99), false).
				Return(music.ErrArtistNotFound).Once()
			resp := send(
				http.MethodDelete,
				"/api/v1/music/artists/99",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("GrabMusicAlbumRelease", func() {
		grabBody := func(link string) string {
			b, err := json.Marshal(map[string]any{
				"title":        "Nirvana - Nevermind FLAC",
				"download_url": link,
				"size":         100,
				"seeders":      9,
			})
			Expect(err).NotTo(HaveOccurred())
			return string(b)
		}
		grab := func(body string) *http.Response {
			GinkgoHelper()
			return send(
				http.MethodPost, "/api/v1/music/albums/3/grab", app.memberKey, body,
			)
		}
		decode := func(resp *http.Response) (string, string) {
			GinkgoHelper()
			var body struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			return body.Message, body.Code
		}

		It("opens the handle and dispatches the grab", func() {
			app.music.EXPECT().
				GrabAlbumRelease(mock.Anything, uint32(3), mock.MatchedBy(
					func(r indexer.SearchResult) bool {
						return r.Download == "magnet:?xt=urn:btih:aaaa" &&
							r.Title == "Nirvana - Nevermind FLAC"
					},
				)).
				Return(nil).Once()
			resp := grab(grabBody(sealReleaseLink("magnet:?xt=urn:btih:aaaa")))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
		})

		It("accepts a plain magnet", func() {
			app.music.EXPECT().
				GrabAlbumRelease(mock.Anything, uint32(3), mock.Anything).
				Return(nil).Once()
			resp := grab(grabBody("magnet:?xt=urn:btih:aaaa"))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
		})

		It("tells the caller to search again on a bad handle", func() {
			resp := grab(grabBody("slr1.bm90LWEtaGFuZGxl"))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			msg, code := decode(resp)
			Expect(code).To(Equal("grab_rejected"))
			Expect(msg).To(ContainSubstring("search again"))
		})

		It("422s a body missing its title or url", func() {
			resp := grab(`{"title":"","download_url":"","size":1,"seeders":1}`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			_, code := decode(resp)
			Expect(code).To(BeEmpty())
		})

		DescribeTable("maps a refused grab to grab_rejected",
			func(sentinel error) {
				app.music.EXPECT().
					GrabAlbumRelease(mock.Anything, uint32(3), mock.Anything).
					Return(fmt.Errorf("grab album: %w", sentinel)).Once()
				resp := grab(grabBody("magnet:?xt=urn:btih:aaaa"))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
				_, code := decode(resp)
				Expect(code).To(Equal("grab_rejected"))
			},
			Entry("untrusted source", download.ErrUntrustedSource),
			Entry("client full", download.ErrClientFull),
			Entry("unsafe torrent name", download.ErrUnsafeTorrentName),
		)

		It("404s for an unknown album", func() {
			app.music.EXPECT().
				GrabAlbumRelease(mock.Anything, uint32(3), mock.Anything).
				Return(fmt.Errorf("grab album: %w", music.ErrAlbumNotFound)).Once()
			resp := grab(grabBody("magnet:?xt=urn:btih:aaaa"))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("422s with no_quality_profile", func() {
			app.music.EXPECT().
				GrabAlbumRelease(mock.Anything, uint32(3), mock.Anything).
				Return(music.ErrNoQualityProfile).Once()
			resp := grab(grabBody("magnet:?xt=urn:btih:aaaa"))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			_, code := decode(resp)
			Expect(code).To(Equal("no_quality_profile"))
		})

		It("500s on a generic error", func() {
			app.music.EXPECT().
				GrabAlbumRelease(mock.Anything, uint32(3), mock.Anything).
				Return(errors.New("boom")).Once()
			resp := grab(grabBody("magnet:?xt=urn:btih:aaaa"))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		})

		It("answers 403 to a request-only caller", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/music/albums/3/grab",
				app.requestOnlyKey,
				grabBody("magnet:?xt=urn:btih:aaaa"),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})
})
