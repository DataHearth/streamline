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
	entalbum "github.com/datahearth/streamline/ent/album"
	entartist "github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
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
					"name":      "lossless",
					"tiers":     []string{"lossless"},
					"preferred": "lossless",
				},
			},
			"music_quality_default_profile": "lossless",
		})
		app = newAPIKeyApp()
		app.addMember("")
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

	get := func(path string) *http.Response {
		GinkgoHelper()
		return send(http.MethodGet, path, app.memberKey, "")
	}

	decodeInto := func(resp *http.Response, out any) {
		GinkgoHelper()
		defer resp.Body.Close()
		Expect(json.NewDecoder(resp.Body).Decode(out)).To(Succeed())
	}

	errCode := func(resp *http.Response) string {
		GinkgoHelper()
		var body struct {
			Code string `json:"code"`
		}
		decodeInto(resp, &body)
		return body.Code
	}

	released := time.Date(1991, 9, 24, 0, 0, 0, 0, time.UTC)

	// detail is a hydrated artist with one album, one file, credits and a
	// member that is also a library artist.
	detail := func() *music.ArtistView {
		tr := &ent.Track{
			ID: 11, Mbid: "rec-1", Title: "Smells Like Teen Spirit",
			Disc: 1, Position: 1, Duration: 301, Bonus: true,
		}
		alb := &ent.Album{
			ID:            5,
			Mbid:          "rg-1",
			Title:         "Nevermind",
			Type:          entalbum.TypeAlbum,
			Monitored:     true,
			ReleaseDate:   &released,
			Label:         "DGC",
			CatalogNumber: "DGCD-24425",
			Country:       "US",
			Media:         "cd,vinyl",
			Studio:        "Sound City",
			Edges:         ent.AlbumEdges{Artist: &ent.Artist{ID: 3}},
		}
		return &music.ArtistView{
			Artist: &ent.Artist{
				ID:             3,
				Mbid:           "a-1",
				Name:           "Nirvana",
				SortName:       "Nirvana",
				Monitor:        entartist.MonitorFuture,
				Type:           entartist.TypeGroup,
				Genre:          "Grunge",
				Origin:         "Aberdeen, United States",
				Since:          1987,
				Path:           "/music/Nirvana",
				QualityProfile: "lossless",
				CreateTime:     released,
			},
			Status:         "wanted",
			AlbumCount:     1,
			TracksHave:     1,
			Size:           1000,
			Hydrating:      true,
			Overview:       "A band.",
			OverviewSource: "https://en.wikipedia.org/wiki/Nirvana",
			Members: []music.MemberView{
				{
					Name:        "Dave Grohl",
					MBID:        "m-1",
					ArtistID:    9,
					Instruments: []string{"drums"},
					From:        1990,
					To:          1994,
				},
				{
					Name: "Kurt Cobain", MBID: "m-2",
				},
			},
			Albums: []music.AlbumView{{
				Album: alb, Status: "upcoming", TrackCount: 1, TracksHave: 1,
				Size: 1000, Duration: 301, Quality: "lossless", Format: "FLAC",
				Tracks: []music.TrackView{
					{
						Track:   tr,
						HasFile: true,
						Featuring: []music.PersonView{
							{Name: "A Guest", MBID: "g-1", ArtistID: 4},
						},
						Writers: []music.PersonView{{Name: "Kurt Cobain"}},
					},
				},
				Credits: []music.CreditView{
					{
						Name: "Butch Vig",
						MBID: "p-1",
						Role: "producer",
					},
				},
				Personnel: []music.PerformerView{{
					Name:        "Pat Smear",
					Instruments: []string{"guitar"}, Guest: true,
				}},
			}},
		}
	}

	Describe("SearchMusicArtists", func() {
		It("maps the hits, flagging the ones in the library", func() {
			app.music.EXPECT().SearchArtists(mock.Anything, "nirvana").
				Return([]music.LookupHit{
					{
						MBID:           "a-1",
						Name:           "Nirvana",
						SortName:       "Nirvana",
						Score:          100,
						Type:           "group",
						Genre:          "Grunge",
						Area:           "Aberdeen",
						Since:          1987,
						Disambiguation: "90s band",
						AlreadyAdded:   true, LibraryID: 3,
					},
					{
						MBID:  "a-2",
						Name:  "Nirvana UK",
						Score: 80,
					},
				}, nil).Once()

			resp := get("/api/v1/music/search?query=nirvana")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var out MusicArtistSearchResultList
			decodeInto(resp, &out)
			Expect(out.Items).To(HaveLen(2))
			first := out.Items[0]
			Expect(first.AlreadyAdded).To(BeTrue())
			Expect(first.LibraryId).To(HaveValue(Equal(uint32(3))))
			Expect(first.Type).To(HaveValue(Equal(MusicArtistKindGroup)))
			Expect(first.Genre).To(HaveValue(Equal("Grunge")))
			Expect(first.Area).To(HaveValue(Equal("Aberdeen")))
			Expect(first.Since).To(HaveValue(Equal(uint16(1987))))
			Expect(first.Disambiguation).To(HaveValue(Equal("90s band")))
			Expect(out.Items[1].AlreadyAdded).To(BeFalse())
			Expect(out.Items[1].LibraryId).To(BeNil())
		})

		It("returns an empty list when nothing matches", func() {
			app.music.EXPECT().
				SearchArtists(mock.Anything, "zzz").
				Return(nil, nil).
				Once()
			resp := get("/api/v1/music/search?query=zzz")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var raw map[string]any
			decodeInto(resp, &raw)
			Expect(raw["items"]).To(Equal([]any{}))
		})

		It("answers 429 with Retry-After when MusicBrainz limits", func() {
			app.music.EXPECT().SearchArtists(mock.Anything, "x").
				Return(nil, &metadata.RateLimitedError{RetryAfter: 1500 * time.Millisecond}).
				Once()
			resp := get("/api/v1/music/search?query=x")
			Expect(resp.StatusCode).To(Equal(http.StatusTooManyRequests))
			Expect(resp.Header.Get("Retry-After")).To(Equal("2"))
			Expect(errCode(resp)).To(Equal("rate_limited"))
		})

		It("is open to a request-only caller", func() {
			app.music.EXPECT().
				SearchArtists(mock.Anything, "x").
				Return(nil, nil).
				Once()
			resp := send(
				http.MethodGet,
				"/api/v1/music/search?query=x",
				app.requestOnlyKey,
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			resp.Body.Close()
		})
	})

	Describe("GetMusicArtistLookup", func() {
		It("maps the detail and passes the language", func() {
			app.music.EXPECT().LookupArtist(mock.Anything, "a-1", "fr").
				Return(&music.LookupDetail{
					MBID:     "a-1",
					Name:     "Nirvana",
					SortName: "Nirvana",
					Overview: "Un groupe.",
					Genres:   []string{"Grunge"},
					Members:  []string{"Dave Grohl"},
					Releases: []music.LookupRelease{{
						MBID: "rg-1", Title: "Nevermind", Year: 1991, Type: "album",
					}},
				}, nil).Once()

			resp := get("/api/v1/music/search/a-1?lang=fr")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var out MusicArtistLookupDetail
			decodeInto(resp, &out)
			Expect(out.Mbid).To(Equal("a-1"))
			Expect(out.Overview).To(HaveValue(Equal("Un groupe.")))
			Expect(out.Genres).To(HaveValue(Equal([]string{"Grunge"})))
			Expect(out.Members).To(HaveValue(Equal([]string{"Dave Grohl"})))
			Expect(out.Releases).To(HaveValue(HaveLen(1)))
			Expect((*out.Releases)[0].Year).To(HaveValue(Equal(uint16(1991))))
		})

		It("defaults the language to English", func() {
			app.music.EXPECT().LookupArtist(mock.Anything, "a-1", "en").
				Return(&music.LookupDetail{}, nil).Once()
			resp := get("/api/v1/music/search/a-1")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			resp.Body.Close()
		})

		It("answers 404 for an artist MusicBrainz does not have", func() {
			app.music.EXPECT().LookupArtist(mock.Anything, "gone", "en").
				Return(nil, fmt.Errorf("x: %w", metadata.ErrNotFound)).Once()
			resp := get("/api/v1/music/search/gone")
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			resp.Body.Close()
		})

		It("answers 429 when MusicBrainz limits", func() {
			app.music.EXPECT().LookupArtist(mock.Anything, "busy", "en").
				Return(nil, &metadata.RateLimitedError{RetryAfter: time.Second}).
				Once()
			resp := get("/api/v1/music/search/busy")
			Expect(resp.StatusCode).To(Equal(http.StatusTooManyRequests))
			resp.Body.Close()
		})
	})

	Describe("ListMusicArtists", func() {
		It("passes the filters and pages to the service", func() {
			app.music.EXPECT().List(mock.Anything, db.ListArtistsParams{
				Status: "wanted", Monitored: "monitored", Query: "nirv",
				Sort: "name", Order: "desc", Offset: 40, Limit: 20,
			}).Return(music.ArtistPage{Total: 0}, nil).Once()
			resp := get(
				"/api/v1/music/artists?status=wanted&monitored=monitored&query=nirv&sort=name&order=desc&page=3",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var out PaginatedMusicArtists
			decodeInto(resp, &out)
			Expect(out.Page).To(Equal(uint32(3)))
			Expect(out.Limit).To(Equal(uint16(20)))
			Expect(out.Items).To(BeEmpty())
		})

		It("maps each artist with its albums as tiles", func() {
			v := detail()
			progress := 42.0
			v.Albums[0].Progress = &progress
			v.Albums[0].Tracks, v.Albums[0].Credits, v.Albums[0].Personnel = nil, nil, nil
			app.music.EXPECT().List(mock.Anything, mock.Anything).
				Return(music.ArtistPage{Items: []music.ArtistView{*v}, Total: 7}, nil).
				Once()

			resp := get("/api/v1/music/artists?limit=1")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var out PaginatedMusicArtists
			decodeInto(resp, &out)
			Expect(out.Total).To(Equal(uint32(7)))
			Expect(out.Items).To(HaveLen(1))
			a := out.Items[0]
			Expect(a.Id).To(Equal(uint32(3)))
			Expect(a.Monitor).To(Equal(MusicMonitorFuture))
			Expect(a.Status).To(Equal(MusicArtistStatusWanted))
			Expect(a.Hydrating).To(BeTrue())
			Expect(a.AlbumCount).To(Equal(uint32(1)))
			Expect(a.TracksHave).To(Equal(uint32(1)))
			Expect(a.Since).To(HaveValue(Equal(uint16(1987))))
			Expect(a.Overview).To(HaveValue(Equal("A band.")))
			Expect(a.Albums).To(HaveLen(1))
			Expect(a.Albums[0].Status).To(Equal(MusicAlbumStatusUpcoming))
			Expect(a.Albums[0].ArtistId).To(Equal(uint32(3)))
			Expect(a.Albums[0].Progress).To(HaveValue(BeNumerically("~", 42, 0.01)))
			Expect(a.Albums[0].ReleaseDate).NotTo(BeNil())
		})

		DescribeTable("rejects a bad page or limit with 400",
			func(query string) {
				resp := get("/api/v1/music/artists?" + query)
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				resp.Body.Close()
			},
			Entry("page 0", "page=0"),
			Entry("limit 0", "limit=0"),
			Entry("limit 101", "limit=101"),
			Entry("unknown status", "status=sleeping"),
			Entry("unknown sort", "sort=popular"),
		)

		It("is open to a request-only caller", func() {
			app.music.EXPECT().List(mock.Anything, mock.Anything).
				Return(music.ArtistPage{}, nil).Once()
			resp := send(
				http.MethodGet,
				"/api/v1/music/artists?query=x&limit=5",
				app.requestOnlyKey,
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			resp.Body.Close()
		})
	})

	Describe("GetMusicArtistCounts", func() {
		It("maps the facets", func() {
			app.music.EXPECT().Counts(mock.Anything, db.ListArtistsParams{
				Status: "wanted", Query: "x",
			}).Return(db.ArtistCounts{
				Total: 10, StatusTotal: 9, Wanted: 3, Downloading: 2, Available: 4,
				MonitoredTotal: 8, Monitored: 7, Unmonitored: 1, Albums: 55,
			}, nil).Once()
			resp := get("/api/v1/music/artists/counts?status=wanted&query=x")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var out MusicArtistCounts
			decodeInto(resp, &out)
			Expect(out).To(Equal(MusicArtistCounts{
				Total: 10, StatusTotal: 9, Wanted: 3, Downloading: 2, Available: 4,
				MonitoredTotal: 8, Monitored: 7, Unmonitored: 1, Albums: 55,
			}))
		})
	})

	Describe("AddMusicArtist", func() {
		add := func(body string) *http.Response {
			GinkgoHelper()
			return send(
				http.MethodPost,
				"/api/v1/music/artists",
				app.memberKey,
				body,
			)
		}

		It("adds the artist and answers its detail", func() {
			app.music.EXPECT().Add(mock.Anything, music.AddParams{
				MBID: "a-1", Monitor: "future", QualityProfile: "lossless",
			}).Return(&ent.Artist{ID: 3}, nil).Once()
			app.music.EXPECT().
				Detail(mock.Anything, uint32(3), "en").
				Return(detail(), nil).
				Once()

			resp := add(
				`{"mbid":"a-1","monitor":"future","quality_profile":"lossless"}`,
			)
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			var out MusicArtistDetail
			decodeInto(resp, &out)
			Expect(out.Id).To(Equal(uint32(3)))
			Expect(out.Hydrating).To(BeTrue())
		})

		It("defaults the monitor policy in the service", func() {
			app.music.EXPECT().Add(mock.Anything, music.AddParams{MBID: "a-1"}).
				Return(&ent.Artist{ID: 3}, nil).Once()
			app.music.EXPECT().
				Detail(mock.Anything, uint32(3), "en").
				Return(detail(), nil).
				Once()
			resp := add(`{"mbid":"a-1"}`)
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			resp.Body.Close()
		})

		DescribeTable(
			"maps a refusal",
			func(err error, status int) {
				app.music.EXPECT().
					Add(mock.Anything, mock.Anything).
					Return(nil, err).
					Once()
				resp := add(`{"mbid":"a-1"}`)
				Expect(resp.StatusCode).To(Equal(status))
				resp.Body.Close()
			},
			Entry(
				"duplicate",
				fmt.Errorf("x: %w", music.ErrArtistExists),
				http.StatusConflict,
			),
			Entry("unknown profile", fmt.Errorf("x: %w", music.ErrUnknownProfile),
				http.StatusUnprocessableEntity),
			Entry("unknown monitor", fmt.Errorf("x: %w", music.ErrInvalidMonitor),
				http.StatusUnprocessableEntity),
			Entry("rate limit", &metadata.RateLimitedError{RetryAfter: time.Second},
				http.StatusTooManyRequests),
			Entry(
				"anything else",
				errors.New("boom"),
				http.StatusInternalServerError,
			),
		)

		It("422s a body with no mbid", func() {
			resp := add(`{"monitor":"all"}`)
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			resp.Body.Close()
		})

		It("answers 403 to a request-only caller", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/music/artists",
				app.requestOnlyKey,
				`{"mbid":"a-1"}`,
			)
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			resp.Body.Close()
		})
	})

	Describe("GetMusicArtist", func() {
		It("maps the full tree", func() {
			app.music.EXPECT().
				Detail(mock.Anything, uint32(3), "fr").
				Return(detail(), nil).
				Once()
			resp := get("/api/v1/music/artists/3?lang=fr")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var out MusicArtistDetail
			decodeInto(resp, &out)

			Expect(out.Name).To(Equal("Nirvana"))
			Expect(out.Type).To(HaveValue(Equal(MusicArtistKindGroup)))
			Expect(
				out.OverviewSource,
			).To(HaveValue(Equal("https://en.wikipedia.org/wiki/Nirvana")))
			Expect(out.Members).To(HaveLen(2))
			Expect(out.Members[0].ArtistId).To(HaveValue(Equal(uint32(9))))
			Expect(out.Members[0].From).To(HaveValue(Equal(uint16(1990))))
			Expect(out.Members[0].To).To(HaveValue(Equal(uint16(1994))))
			Expect(out.Members[1].Instruments).To(BeEmpty())
			Expect(out.Members[1].ArtistId).To(BeNil())

			Expect(out.Albums).To(HaveLen(1))
			al := out.Albums[0]
			Expect(al.ArtistId).To(Equal(uint32(3)))
			Expect(al.Label).To(HaveValue(Equal("DGC")))
			Expect(al.Country).To(HaveValue(Equal("US")))
			Expect(
				al.Media,
			).To(HaveValue(Equal([]MusicMedium{MusicMediumCd, MusicMediumVinyl})))
			Expect(al.Quality).To(HaveValue(Equal(MusicTierLossless)))
			Expect(al.Format).To(HaveValue(Equal("FLAC")))
			Expect(al.Size).To(HaveValue(Equal(int64(1000))))
			Expect(al.Duration).To(HaveValue(Equal(uint32(301))))
			Expect(al.Credits).To(HaveLen(1))
			Expect(al.Credits[0].Role).To(Equal(MusicCreditRoleProducer))
			Expect(al.Personnel[0].Guest).To(BeTrue())

			Expect(al.Tracks).To(HaveLen(1))
			tr := al.Tracks[0]
			Expect(tr.Number).To(Equal(uint16(1)))
			Expect(tr.HasFile).To(BeTrue())
			Expect(tr.Bonus).To(HaveValue(BeTrue()))
			Expect((*tr.Featuring)[0].ArtistId).To(HaveValue(Equal(uint32(4))))
			Expect((*tr.Writers)[0].Name).To(Equal("Kurt Cobain"))
		})

		It("omits what has no value instead of sending empty strings", func() {
			v := &music.ArtistView{
				Artist: &ent.Artist{
					ID:         3,
					Mbid:       "a-1",
					Name:       "X",
					CreateTime: released,
				},
				Status: "available",
				Albums: []music.AlbumView{
					{
						Album: &ent.Album{
							ID:    5,
							Mbid:  "rg",
							Title: "T",
							Edges: ent.AlbumEdges{Artist: &ent.Artist{ID: 3}},
						},
						Status:        "wanted",
						TracksPending: true,
					},
				},
			}
			app.music.EXPECT().
				Detail(mock.Anything, uint32(3), "en").
				Return(v, nil).
				Once()
			resp := get("/api/v1/music/artists/3")
			var raw map[string]any
			decodeInto(resp, &raw)
			for _, k := range []string{"genre", "origin", "overview", "overview_source", "since", "type"} {
				Expect(raw).NotTo(HaveKey(k))
			}
			al := raw["albums"].([]any)[0].(map[string]any)
			for _, k := range []string{"label", "country", "media", "quality", "format", "size", "duration", "release_date"} {
				Expect(al).NotTo(HaveKey(k))
			}
			Expect(al["tracks_pending"]).To(BeTrue())
			Expect(al["tracks"]).To(Equal([]any{}))
			Expect(raw["members"]).To(Equal([]any{}))
		})

		It("404s for an unknown artist", func() {
			app.music.EXPECT().Detail(mock.Anything, uint32(99), "en").
				Return(nil, music.ErrArtistNotFound).Once()
			resp := get("/api/v1/music/artists/99")
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			resp.Body.Close()
		})
	})

	Describe("PatchMusicArtist", func() {
		patch := func(body string) *http.Response {
			GinkgoHelper()
			return send(
				http.MethodPatch,
				"/api/v1/music/artists/3",
				app.memberKey,
				body,
			)
		}

		It(
			"applies the monitor policy and the profile, then answers the detail",
			func() {
				app.music.EXPECT().
					SetArtistMonitor(mock.Anything, uint32(3), "none").
					Return(nil).
					Once()
				app.music.EXPECT().
					SetArtistQualityProfile(mock.Anything, uint32(3), "").
					Return(nil).
					Once()
				app.music.EXPECT().
					Detail(mock.Anything, uint32(3), "en").
					Return(detail(), nil).
					Once()
				resp := patch(`{"monitor":"none","quality_profile":""}`)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				resp.Body.Close()
			},
		)

		It("touches only what the body carries", func() {
			app.music.EXPECT().
				SetArtistQualityProfile(mock.Anything, uint32(3), "lossless").
				Return(nil).
				Once()
			app.music.EXPECT().
				Detail(mock.Anything, uint32(3), "en").
				Return(detail(), nil).
				Once()
			resp := patch(`{"quality_profile":"lossless"}`)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			resp.Body.Close()
		})

		DescribeTable(
			"maps a refusal",
			func(err error, status int) {
				app.music.EXPECT().
					SetArtistMonitor(mock.Anything, uint32(3), "all").
					Return(err).
					Once()
				resp := patch(`{"monitor":"all"}`)
				Expect(resp.StatusCode).To(Equal(status))
				resp.Body.Close()
			},
			Entry("missing", music.ErrArtistNotFound, http.StatusNotFound),
			Entry(
				"bad monitor",
				music.ErrInvalidMonitor,
				http.StatusUnprocessableEntity,
			),
			Entry(
				"bad profile",
				music.ErrUnknownProfile,
				http.StatusUnprocessableEntity,
			),
			Entry("other", errors.New("boom"), http.StatusInternalServerError),
		)

		It("answers 403 to a request-only caller", func() {
			resp := send(
				http.MethodPatch,
				"/api/v1/music/artists/3",
				app.requestOnlyKey,
				`{"monitor":"all"}`,
			)
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			resp.Body.Close()
		})
	})

	Describe("DeleteMusicArtist", func() {
		It("passes delete_files through and answers 204", func() {
			app.music.EXPECT().
				Delete(mock.Anything, uint32(7), true).
				Return(nil).
				Once()
			resp := send(
				http.MethodDelete,
				"/api/v1/music/artists/7?delete_files=true",
				app.adminKey,
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			resp.Body.Close()
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
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			resp.Body.Close()
		})
	})

	Describe("RefreshMusicArtist", func() {
		refresh := func() *http.Response {
			GinkgoHelper()
			return send(
				http.MethodPost,
				"/api/v1/music/artists/3/refresh-metadata?lang=fr",
				app.memberKey,
				"",
			)
		}

		It("refreshes, then answers the detail in the requested language", func() {
			app.music.EXPECT().
				RefreshOne(mock.Anything, uint32(3)).
				Return(&ent.Artist{ID: 3}, nil).
				Once()
			app.music.EXPECT().
				Detail(mock.Anything, uint32(3), "fr").
				Return(detail(), nil).
				Once()
			resp := refresh()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			resp.Body.Close()
		})

		DescribeTable(
			"maps a failure",
			func(err error, status int) {
				app.music.EXPECT().
					RefreshOne(mock.Anything, uint32(3)).
					Return(nil, err).
					Once()
				resp := refresh()
				Expect(resp.StatusCode).To(Equal(status))
				resp.Body.Close()
			},
			Entry("missing", music.ErrArtistNotFound, http.StatusNotFound),
			Entry(
				"rate limited",
				&metadata.RateLimitedError{RetryAfter: time.Second},
				http.StatusTooManyRequests,
			),
			Entry("other", errors.New("boom"), http.StatusInternalServerError),
		)

		It("is gone from its old path", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/music/artists/3/refresh",
				app.memberKey,
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			resp.Body.Close()
		})
	})

	Describe("RenameMusicArtistFiles", func() {
		plan := library.RenamePlan{Operations: []library.RenameOperation{
			{MediaFileID: 4, From: "/m/a.flac", To: "/m/b.flac"},
		}}

		It("previews without applying", func() {
			app.musicRenamer.EXPECT().
				Preview(mock.Anything, uint32(3)).
				Return(plan, nil).
				Once()
			resp := send(
				http.MethodPost,
				"/api/v1/music/artists/3/rename?preview=true",
				app.memberKey,
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var out MusicRenamePlan
			decodeInto(resp, &out)
			Expect(out.ArtistId).To(Equal(uint32(3)))
			Expect(out.Operations).To(Equal([]RenameOperation{
				{MediaFileId: 4, From: "/m/a.flac", To: "/m/b.flac"},
			}))
		})

		It("applies otherwise", func() {
			app.musicRenamer.EXPECT().Apply(mock.Anything, uint32(3)).
				Return(library.RenamePlan{}, nil).Once()
			resp := send(
				http.MethodPost,
				"/api/v1/music/artists/3/rename",
				app.memberKey,
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var raw map[string]any
			decodeInto(resp, &raw)
			Expect(raw["operations"]).To(Equal([]any{}))
		})

		It("404s for an unknown artist and 500s on a collision", func() {
			app.musicRenamer.EXPECT().Apply(mock.Anything, uint32(3)).
				Return(library.RenamePlan{}, music.ErrArtistNotFound).Once()
			resp := send(
				http.MethodPost,
				"/api/v1/music/artists/3/rename",
				app.memberKey,
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			resp.Body.Close()

			app.musicRenamer.EXPECT().Apply(mock.Anything, uint32(3)).
				Return(library.RenamePlan{}, library.ErrDestExists).Once()
			resp = send(
				http.MethodPost,
				"/api/v1/music/artists/3/rename",
				app.memberKey,
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			resp.Body.Close()
		})

		It("answers 403 to a request-only caller", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/music/artists/3/rename",
				app.requestOnlyKey,
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			resp.Body.Close()
		})
	})

	Describe("search-now", func() {
		DescribeTable("answers 202, 404 and 403",
			func(path string, expect func(err error)) {
				expect(nil)
				resp := send(http.MethodPost, path, app.memberKey, "")
				Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
				resp.Body.Close()

				expect(music.ErrArtistNotFound)
				resp = send(http.MethodPost, path, app.memberKey, "")
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				resp.Body.Close()

				resp = send(http.MethodPost, path, app.requestOnlyKey, "")
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
				resp.Body.Close()
			},
			Entry("artist", "/api/v1/music/artists/3/search-now", func(err error) {
				app.music.EXPECT().
					SearchArtistNow(mock.Anything, uint32(3)).
					Return(err).
					Once()
			}),
			Entry("album", "/api/v1/music/albums/3/search-now", func(err error) {
				if err != nil {
					err = music.ErrAlbumNotFound
				}
				app.music.EXPECT().
					SearchAlbumNow(mock.Anything, uint32(3)).
					Return(err).
					Once()
			}),
			Entry("track", "/api/v1/music/tracks/3/search-now", func(err error) {
				if err != nil {
					err = music.ErrTrackNotFound
				}
				app.music.EXPECT().
					SearchTrackNow(mock.Anything, uint32(3)).
					Return(err).
					Once()
			}),
		)
	})

	Describe("the manual searches", func() {
		releases := func() []music.AlbumRelease {
			ok := indexer.SearchResult{
				Title:             "Nirvana - Nevermind (1991) [24bit FLAC]",
				Download:          "magnet:?xt=urn:btih:a",
				Seeders:           5,
				Size:              100,
				ConfiguredIndexer: "private-one",
			}
			bad := indexer.SearchResult{
				Title:    "Nirvana - Nevermind (1991) [MP3 128]",
				Download: "magnet:?xt=urn:btih:b",
			}
			return []music.AlbumRelease{
				{
					Result: ok,
					Parsed: library.ParseMusicRelease(ok.Title),
					Score:  450,
				},
				{
					Result: bad,
					Parsed: library.ParseMusicRelease(bad.Title),
					Score:  -1,
					Reason: "tier not in the profile",
				},
			}
		}

		check := func(resp *http.Response) {
			GinkgoHelper()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var out SearchResultList
			decodeInto(resp, &out)
			Expect(out.Items).To(HaveLen(2))
			first := out.Items[0]
			Expect(first.Source).To(HaveValue(Equal("FLAC 24-bit")))
			Expect(first.AudioTier).To(HaveValue(Equal(MusicTierHires)))
			Expect(first.Score).To(HaveValue(Equal(450)))
			Expect(first.Rejected).To(BeNil())
			Expect(first.MatchedFormats).To(HaveValue(BeEmpty()))
			Expect(first.Resolution).To(BeNil())
			Expect(first.DownloadUrl).To(HavePrefix("slr1."))
			rejected := out.Items[1]
			Expect(rejected.Rejected).To(HaveValue(BeTrue()))
			Expect(
				rejected.RejectReason,
			).To(HaveValue(Equal("tier not in the profile")))
			Expect(rejected.AudioTier).To(HaveValue(Equal(MusicTierLow)))
		}

		It("lists an album's releases as plain search results", func() {
			app.music.EXPECT().
				SearchAlbumReleases(mock.Anything, uint32(3)).
				Return(releases(), nil).
				Once()
			check(
				send(
					http.MethodPost,
					"/api/v1/music/albums/3/search",
					app.memberKey,
					"",
				),
			)
		})

		It("lists an artist's discography packs the same way", func() {
			app.music.EXPECT().
				BrowseArtistReleases(mock.Anything, uint32(3)).
				Return(releases(), nil).
				Once()
			check(
				send(
					http.MethodPost,
					"/api/v1/music/artists/3/browse",
					app.memberKey,
					"",
				),
			)
		})

		DescribeTable("maps a failure",
			func(path string, stub func(err error), err error, status int) {
				stub(err)
				resp := send(http.MethodPost, path, app.memberKey, "")
				Expect(resp.StatusCode).To(Equal(status))
				if status == http.StatusUnprocessableEntity {
					Expect(errCode(resp)).To(Equal("no_quality_profile"))
					return
				}
				resp.Body.Close()
			},
			Entry("album: no profile", "/api/v1/music/albums/3/search",
				func(err error) {
					app.music.EXPECT().
						SearchAlbumReleases(mock.Anything, uint32(3)).
						Return(nil, err).
						Once()
				}, music.ErrNoQualityProfile, http.StatusUnprocessableEntity),
			Entry("album: missing", "/api/v1/music/albums/3/search",
				func(err error) {
					app.music.EXPECT().
						SearchAlbumReleases(mock.Anything, uint32(3)).
						Return(nil, err).
						Once()
				}, music.ErrAlbumNotFound, http.StatusNotFound),
			Entry("artist: no profile", "/api/v1/music/artists/3/browse",
				func(err error) {
					app.music.EXPECT().
						BrowseArtistReleases(mock.Anything, uint32(3)).
						Return(nil, err).
						Once()
				}, music.ErrNoQualityProfile, http.StatusUnprocessableEntity),
			Entry("artist: missing", "/api/v1/music/artists/3/browse",
				func(err error) {
					app.music.EXPECT().
						BrowseArtistReleases(mock.Anything, uint32(3)).
						Return(nil, err).
						Once()
				}, music.ErrArtistNotFound, http.StatusNotFound),
		)

		It("answers 403 to a request-only caller", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/music/albums/3/search",
				app.requestOnlyKey,
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			resp.Body.Close()
		})
	})

	Describe("the grabs", func() {
		grabBody := func(link string, extra ...string) string {
			b, err := json.Marshal(map[string]any{
				"title":        "Nirvana - Nevermind FLAC",
				"download_url": link,
				"size":         100,
				"seeders":      9,
			})
			Expect(err).NotTo(HaveOccurred())
			s := string(b)
			if len(extra) > 0 {
				s = strings.TrimSuffix(s, "}") + "," + extra[0] + "}"
			}
			return s
		}
		decode := func(resp *http.Response) (string, string) {
			GinkgoHelper()
			var body struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			}
			decodeInto(resp, &body)
			return body.Message, body.Code
		}
		matchesRelease := mock.MatchedBy(func(r indexer.SearchResult) bool {
			return r.Download == "magnet:?xt=urn:btih:aaaa" &&
				r.Title == "Nirvana - Nevermind FLAC"
		})

		Describe("GrabMusicAlbumRelease", func() {
			grab := func(body string) *http.Response {
				GinkgoHelper()
				return send(
					http.MethodPost,
					"/api/v1/music/albums/3/grab",
					app.memberKey,
					body,
				)
			}

			It("opens the handle and dispatches the grab", func() {
				app.music.EXPECT().
					GrabAlbum(mock.Anything, uint32(3), matchesRelease, false).
					Return(nil).
					Once()
				resp := grab(grabBody(sealReleaseLink("magnet:?xt=urn:btih:aaaa")))
				Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
				resp.Body.Close()
			})

			It("accepts a plain magnet", func() {
				app.music.EXPECT().
					GrabAlbum(mock.Anything, uint32(3), mock.Anything, false).
					Return(nil).
					Once()
				resp := grab(grabBody("magnet:?xt=urn:btih:aaaa"))
				Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
				resp.Body.Close()
			})

			It("passes replace_existing through", func() {
				app.music.EXPECT().
					GrabAlbum(mock.Anything, uint32(3), mock.Anything, true).
					Return(nil).
					Once()
				resp := grab(
					grabBody("magnet:?xt=urn:btih:aaaa", `"replace_existing":true`),
				)
				Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
				resp.Body.Close()
			})

			It("tells the caller to search again on a bad handle", func() {
				resp := grab(grabBody("slr1.bm90LWEtaGFuZGxl"))
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
				msg, code := decode(resp)
				Expect(code).To(Equal("grab_rejected"))
				Expect(msg).To(ContainSubstring("search again"))
			})

			It("422s a body missing its title or url", func() {
				resp := grab(`{"title":"","download_url":"","size":1,"seeders":1}`)
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
				_, code := decode(resp)
				Expect(code).To(BeEmpty())
			})

			DescribeTable("maps a refused grab to grab_rejected",
				func(sentinel error) {
					app.music.EXPECT().
						GrabAlbum(mock.Anything, uint32(3), mock.Anything, false).
						Return(fmt.Errorf("grab album: %w", sentinel)).
						Once()
					resp := grab(grabBody("magnet:?xt=urn:btih:aaaa"))
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
					GrabAlbum(mock.Anything, uint32(3), mock.Anything, false).
					Return(fmt.Errorf("grab album: %w", music.ErrAlbumNotFound)).
					Once()
				resp := grab(grabBody("magnet:?xt=urn:btih:aaaa"))
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				resp.Body.Close()
			})

			It("422s with no_quality_profile", func() {
				app.music.EXPECT().
					GrabAlbum(mock.Anything, uint32(3), mock.Anything, false).
					Return(music.ErrNoQualityProfile).
					Once()
				resp := grab(grabBody("magnet:?xt=urn:btih:aaaa"))
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
				_, code := decode(resp)
				Expect(code).To(Equal("no_quality_profile"))
			})

			It("500s on a generic error", func() {
				app.music.EXPECT().
					GrabAlbum(mock.Anything, uint32(3), mock.Anything, false).
					Return(errors.New("boom")).
					Once()
				resp := grab(grabBody("magnet:?xt=urn:btih:aaaa"))
				Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
				resp.Body.Close()
			})

			It("answers 403 to a request-only caller", func() {
				resp := send(
					http.MethodPost,
					"/api/v1/music/albums/3/grab",
					app.requestOnlyKey,
					grabBody("magnet:?xt=urn:btih:aaaa"),
				)
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
				resp.Body.Close()
			})
		})

		Describe("GrabMusicArtistRelease", func() {
			grab := func(body string) *http.Response {
				GinkgoHelper()
				return send(
					http.MethodPost,
					"/api/v1/music/artists/3/grab",
					app.memberKey,
					body,
				)
			}

			It("dispatches the pack", func() {
				app.music.EXPECT().
					GrabArtistRelease(mock.Anything, uint32(3), matchesRelease, false).
					Return(nil).
					Once()
				resp := grab(grabBody(sealReleaseLink("magnet:?xt=urn:btih:aaaa")))
				Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
				resp.Body.Close()
			})

			It("passes replace_existing through", func() {
				app.music.EXPECT().
					GrabArtistRelease(mock.Anything, uint32(3), mock.Anything, true).
					Return(nil).
					Once()
				resp := grab(
					grabBody("magnet:?xt=urn:btih:aaaa", `"replace_existing":true`),
				)
				Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
				resp.Body.Close()
			})

			DescribeTable(
				"maps a refusal",
				func(err error, status int, code string) {
					app.music.EXPECT().
						GrabArtistRelease(mock.Anything, uint32(3), mock.Anything, false).
						Return(fmt.Errorf("grab artist pack: %w", err)).
						Once()
					resp := grab(grabBody("magnet:?xt=urn:btih:aaaa"))
					Expect(resp.StatusCode).To(Equal(status))
					if code == "" {
						resp.Body.Close()
						return
					}
					_, got := decode(resp)
					Expect(got).To(Equal(code))
				},
				Entry("no linkable album", download.ErrNoWantedFiles,
					http.StatusUnprocessableEntity, "grab_rejected"),
				Entry("untrusted source", download.ErrUntrustedSource,
					http.StatusUnprocessableEntity, "grab_rejected"),
				Entry("no profile", music.ErrNoQualityProfile,
					http.StatusUnprocessableEntity, "no_quality_profile"),
				Entry(
					"missing artist",
					music.ErrArtistNotFound,
					http.StatusNotFound,
					"",
				),
				Entry(
					"other",
					errors.New("boom"),
					http.StatusInternalServerError,
					"",
				),
			)

			It("answers 403 to a request-only caller", func() {
				resp := send(
					http.MethodPost,
					"/api/v1/music/artists/3/grab",
					app.requestOnlyKey,
					grabBody("magnet:?xt=urn:btih:aaaa"),
				)
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
				resp.Body.Close()
			})
		})
	})

	Describe("the album", func() {
		It("GetMusicAlbum answers the same object an artist detail embeds", func() {
			v := detail().Albums[0]
			app.music.EXPECT().
				AlbumDetail(mock.Anything, uint32(5)).
				Return(&v, nil).
				Once()
			resp := get("/api/v1/music/albums/5")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var out MusicAlbum
			decodeInto(resp, &out)
			Expect(out.Id).To(Equal(uint32(5)))
			Expect(out.ArtistId).To(Equal(uint32(3)))
			Expect(out.Tracks).To(HaveLen(1))
		})

		It("GetMusicAlbum 404s for an unknown album", func() {
			app.music.EXPECT().AlbumDetail(mock.Anything, uint32(99)).
				Return(nil, music.ErrAlbumNotFound).Once()
			resp := get("/api/v1/music/albums/99")
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			resp.Body.Close()
		})

		It("PatchMusicAlbum sets the flag and answers the album", func() {
			v := detail().Albums[0]
			app.music.EXPECT().
				SetAlbumMonitored(mock.Anything, uint32(5), false).
				Return(nil).
				Once()
			app.music.EXPECT().
				AlbumDetail(mock.Anything, uint32(5)).
				Return(&v, nil).
				Once()
			resp := send(
				http.MethodPatch,
				"/api/v1/music/albums/5",
				app.memberKey,
				`{"monitored":false}`,
			)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			resp.Body.Close()
		})

		It(
			"PatchMusicAlbum 404s for an unknown album and 403s a request-only caller",
			func() {
				app.music.EXPECT().SetAlbumMonitored(mock.Anything, uint32(5), true).
					Return(music.ErrAlbumNotFound).Once()
				resp := send(
					http.MethodPatch,
					"/api/v1/music/albums/5",
					app.memberKey,
					`{"monitored":true}`,
				)
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				resp.Body.Close()

				resp = send(
					http.MethodPatch,
					"/api/v1/music/albums/5",
					app.requestOnlyKey,
					`{"monitored":true}`,
				)
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
				resp.Body.Close()
			},
		)
	})

	Describe("DeleteMusicTrackFile", func() {
		del := func(key string) *http.Response {
			return send(http.MethodDelete, "/api/v1/music/tracks/8/file", key, "")
		}

		It("answers 204", func() {
			app.music.EXPECT().
				DeleteTrackFile(mock.Anything, uint32(8)).
				Return(nil).
				Once()
			resp := del(app.memberKey)
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			resp.Body.Close()
		})

		DescribeTable(
			"maps a failure",
			func(err error, status int) {
				app.music.EXPECT().
					DeleteTrackFile(mock.Anything, uint32(8)).
					Return(err).
					Once()
				resp := del(app.memberKey)
				Expect(resp.StatusCode).To(Equal(status))
				resp.Body.Close()
			},
			Entry("missing", music.ErrTrackNotFound, http.StatusNotFound),
			Entry(
				"outside the root",
				fmt.Errorf("x: %w", music.ErrOutsideRoot),
				http.StatusConflict,
			),
			Entry("other", errors.New("boom"), http.StatusInternalServerError),
		)

		It("answers 403 to a request-only caller", func() {
			resp := del(app.requestOnlyKey)
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			resp.Body.Close()
		})
	})
})
