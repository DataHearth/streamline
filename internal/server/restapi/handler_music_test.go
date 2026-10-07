package restapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
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
})
