package restapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

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

	seededArtist := func() *ent.Artist {
		release := time.Date(1991, 9, 24, 0, 0, 0, 0, time.UTC)
		return &ent.Artist{
			ID:             7,
			Mbid:           "a-1",
			Name:           "Nirvana",
			SortName:       "Nirvana",
			Monitored:      true,
			Path:           "/music/Nirvana",
			QualityProfile: "lossless",
			Edges: ent.ArtistEdges{Albums: []*ent.Album{{
				ID:          3,
				Mbid:        "rg-1",
				Title:       "Nevermind",
				ReleaseDate: &release,
				Monitored:   true,
			}}},
		}
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

	Describe("ListMusicArtists", func() {
		It("rejects page=0", func() {
			resp := send(
				http.MethodGet,
				"/api/v1/music/artists?page=0",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("rejects a limit above 100", func() {
			resp := send(
				http.MethodGet,
				"/api/v1/music/artists?limit=101",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("returns a paginated body with album counts", func() {
			app.music.EXPECT().List(mock.Anything, uint16(2), uint16(10)).
				Return([]*ent.Artist{seededArtist()}, uint32(11), nil).Once()
			resp := send(
				http.MethodGet,
				"/api/v1/music/artists?page=2&limit=10",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var out PaginatedMusicArtists
			Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
			Expect(out.Total).To(Equal(uint32(11)))
			Expect(out.Page).To(Equal(uint32(2)))
			Expect(out.Limit).To(Equal(uint16(10)))
			Expect(out.Items).To(HaveLen(1))
			Expect(out.Items[0].AlbumCount).To(Equal(uint32(1)))
			Expect(out.Items[0].Albums).To(BeNil())
		})
	})

	Describe("AddMusicArtist", func() {
		It("adds the artist and answers 201", func() {
			app.music.EXPECT().Add(mock.Anything, music.AddParams{
				MBID: "a-1", Monitored: true, QualityProfile: "lossless",
			}).Return(seededArtist(), nil).Once()

			resp := send(
				http.MethodPost,
				"/api/v1/music/artists",
				app.adminKey,
				`{"mbid":"a-1","quality_profile":"lossless"}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))

			var out MusicArtist
			Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
			Expect(out.Name).To(Equal("Nirvana"))
		})

		It("answers 409 for an artist already in the library", func() {
			app.music.EXPECT().Add(mock.Anything, mock.Anything).
				Return(nil, music.ErrArtistExists).Once()
			resp := send(
				http.MethodPost,
				"/api/v1/music/artists",
				app.adminKey,
				`{"mbid":"a-1"}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		It("answers 403 to a request-only caller", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/music/artists",
				app.requestOnlyKey,
				`{"mbid":"a-1"}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("GetMusicArtist", func() {
		It("returns the artist with its albums", func() {
			app.music.EXPECT().Get(mock.Anything, uint32(7)).
				Return(seededArtist(), nil).Once()
			resp := send(http.MethodGet, "/api/v1/music/artists/7", app.adminKey, "")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var out MusicArtist
			Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
			Expect(out.Albums).NotTo(BeNil())
			Expect(*out.Albums).To(HaveLen(1))
			Expect((*out.Albums)[0].ReleaseDate.Format("2006-01-02")).
				To(Equal("1991-09-24"))
		})

		It("404s for an unknown id", func() {
			app.music.EXPECT().Get(mock.Anything, uint32(99)).
				Return(nil, music.ErrArtistNotFound).Once()
			resp := send(
				http.MethodGet,
				"/api/v1/music/artists/99",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("PatchMusicArtist", func() {
		It("applies monitored and quality_profile then re-reads", func() {
			app.music.EXPECT().SetArtistMonitored(mock.Anything, uint32(7), false).
				Return(nil).Once()
			app.music.EXPECT().
				SetArtistQualityProfile(mock.Anything, uint32(7), "lossless").
				Return(nil).Once()
			app.music.EXPECT().Get(mock.Anything, uint32(7)).
				Return(seededArtist(), nil).Once()

			resp := send(
				http.MethodPatch,
				"/api/v1/music/artists/7",
				app.adminKey,
				`{"monitored":false,"quality_profile":"lossless"}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("answers 422 for an unknown quality profile", func() {
			resp := send(
				http.MethodPatch,
				"/api/v1/music/artists/7",
				app.adminKey,
				`{"quality_profile":"nope"}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("404s for an unknown artist", func() {
			app.music.EXPECT().SetArtistMonitored(mock.Anything, uint32(99), true).
				Return(music.ErrArtistNotFound).Once()
			resp := send(
				http.MethodPatch,
				"/api/v1/music/artists/99",
				app.adminKey,
				`{"monitored":true}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
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

	Describe("RefreshMusicArtist", func() {
		It("returns the refreshed artist", func() {
			app.music.EXPECT().RefreshOne(mock.Anything, uint32(7)).
				Return(seededArtist(), nil).Once()
			resp := send(
				http.MethodPost,
				"/api/v1/music/artists/7/refresh",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("GetMusicAlbum", func() {
		It("returns the album with its tracks", func() {
			app.music.EXPECT().GetAlbum(mock.Anything, uint32(3)).
				Return(&ent.Album{
					ID: 3, Mbid: "rg-1", Title: "Nevermind",
					Edges: ent.AlbumEdges{Tracks: []*ent.Track{
						{
							ID:       1,
							Mbid:     "t-1",
							Title:    "Smells Like Teen Spirit",
							Disc:     1,
							Position: 1,
						},
					}},
				}, nil).Once()
			resp := send(http.MethodGet, "/api/v1/music/albums/3", app.adminKey, "")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var out MusicAlbum
			Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
			Expect(out.TrackCount).To(Equal(uint32(1)))
			Expect(out.Tracks).NotTo(BeNil())
			Expect((*out.Tracks)[0].Title).To(Equal("Smells Like Teen Spirit"))
		})

		It("404s for an unknown id", func() {
			app.music.EXPECT().GetAlbum(mock.Anything, uint32(99)).
				Return(nil, music.ErrAlbumNotFound).Once()
			resp := send(http.MethodGet, "/api/v1/music/albums/99", app.adminKey, "")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("PatchMusicAlbum", func() {
		It("toggles monitored and re-reads the album", func() {
			app.music.EXPECT().SetAlbumMonitored(mock.Anything, uint32(3), false).
				Return(nil).Once()
			app.music.EXPECT().GetAlbum(mock.Anything, uint32(3)).
				Return(&ent.Album{ID: 3, Title: "Nevermind"}, nil).Once()
			resp := send(
				http.MethodPatch,
				"/api/v1/music/albums/3",
				app.adminKey,
				`{"monitored":false}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("404s for an unknown album", func() {
			app.music.EXPECT().SetAlbumMonitored(mock.Anything, uint32(99), true).
				Return(music.ErrAlbumNotFound).Once()
			resp := send(
				http.MethodPatch,
				"/api/v1/music/albums/99",
				app.adminKey,
				`{"monitored":true}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("SearchMusicAlbumReleases", func() {
		It("returns the ranked releases with sealed handles", func() {
			app.music.EXPECT().SearchAlbumReleases(mock.Anything, uint32(3)).
				Return([]music.AlbumRelease{
					{
						Result: indexer.SearchResult{
							Title:    "Nirvana - Nevermind FLAC",
							Download: "magnet:?xt=urn:btih:aaaa",
							Size:     100,
							Seeders:  9,
						},
						Format: "flac",
						Score:  40,
					},
					{
						Result: indexer.SearchResult{
							Title:    "Nirvana - Nevermind MP3",
							Download: "magnet:?xt=urn:btih:bbbb",
							Size:     50,
							Seeders:  3,
						},
						Format: "mp3-320",
						Score:  10,
					},
				}, nil).Once()
			resp := send(
				http.MethodPost, "/api/v1/music/albums/3/search", app.memberKey, "",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body AlbumReleaseList
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(2))
			Expect(body.Items[0].Format).To(Equal("flac"))
			Expect(body.Items[0].Release.Title).To(Equal("Nirvana - Nevermind FLAC"))
			Expect(body.Items[0].Release.Score).To(HaveValue(Equal(40)))
			Expect(body.Items[0].Release.DownloadUrl).To(HavePrefix("slr1."))
			Expect(body.Items[1].Format).To(Equal("mp3-320"))
			Expect(body.Items[1].Release.Score).To(HaveValue(Equal(10)))
		})

		It("404s for an unknown album", func() {
			app.music.EXPECT().SearchAlbumReleases(mock.Anything, uint32(99)).
				Return(nil, music.ErrAlbumNotFound).Once()
			resp := send(
				http.MethodPost, "/api/v1/music/albums/99/search", app.memberKey, "",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("422s with no_quality_profile when the album has none", func() {
			app.music.EXPECT().SearchAlbumReleases(mock.Anything, uint32(3)).
				Return(nil, music.ErrNoQualityProfile).Once()
			resp := send(
				http.MethodPost, "/api/v1/music/albums/3/search", app.memberKey, "",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			var body struct {
				Code string `json:"code"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Code).To(Equal("no_quality_profile"))
		})

		It("500s on a generic error", func() {
			app.music.EXPECT().SearchAlbumReleases(mock.Anything, uint32(3)).
				Return(nil, errors.New("boom")).Once()
			resp := send(
				http.MethodPost, "/api/v1/music/albums/3/search", app.memberKey, "",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		})

		It("answers 403 to a request-only caller", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/music/albums/3/search",
				app.requestOnlyKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
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
