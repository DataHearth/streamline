package restapi

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/config"
)

var _ = Describe("convert", Label("unit", "server"), func() {
	Describe("movieToAPI", func() {
		It("omits Overview when ent field is empty", func() {
			m := &ent.Movie{
				ID:     1,
				Title:  "Interstellar",
				Year:   2014,
				Status: movie.StatusWanted,
				TmdbID: 157336,
			}
			api := movieToAPI(m)
			Expect(api.Id).To(Equal(uint32(1)))
			Expect(api.Title).To(Equal("Interstellar"))
			Expect(api.Overview).To(BeNil())
		})

		It("sets Overview when ent field is populated", func() {
			m := &ent.Movie{
				ID:       2,
				Title:    "Inception",
				Overview: "dream heist",
				Status:   movie.StatusDownloading,
				TmdbID:   27205,
			}
			api := movieToAPI(m)
			Expect(api.Overview).NotTo(BeNil())
			Expect(*api.Overview).To(Equal("dream heist"))
		})

		It("carries the digital release date when TMDB published one", func() {
			digital := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
			m := &ent.Movie{
				ID:                 3,
				Title:              "La bataille de Gaulle",
				Status:             movie.StatusWanted,
				TmdbID:             1165369,
				DigitalReleaseDate: &digital,
			}
			api := movieToAPI(m)
			Expect(api.DigitalReleaseDate).NotTo(BeNil())
			Expect(api.DigitalReleaseDate.String()).To(Equal("2026-10-29"))

			m.DigitalReleaseDate = nil
			Expect(movieToAPI(m).DigitalReleaseDate).To(BeNil())
		})
	})

	Describe("toAPIUser", func() {
		It("omits DisplayName when ent field is empty", func() {
			u := &ent.User{
				ID:         1,
				Email:      "a@x.com",
				Role:       user.RoleMember,
				AuthMethod: user.AuthMethodLocal,
				CreateTime: time.Now(),
			}
			api := toAPIUser(u)
			Expect(api.Id).To(Equal(uint32(1)))
			Expect(string(api.Email)).To(Equal("a@x.com"))
			Expect(api.DisplayName).To(BeNil())
			Expect(string(api.Role)).To(Equal("member"))
			Expect(string(api.AuthMethod)).To(Equal("local"))
		})

		It("sets DisplayName when ent field is populated", func() {
			u := &ent.User{
				ID:          2,
				Email:       "b@x.com",
				DisplayName: "Beatrice",
				Role:        user.RoleAdmin,
				AuthMethod:  user.AuthMethodBoth,
				CreateTime:  time.Now(),
			}
			api := toAPIUser(u)
			Expect(api.DisplayName).NotTo(BeNil())
			Expect(*api.DisplayName).To(Equal("Beatrice"))
			Expect(string(api.Role)).To(Equal("admin"))
			Expect(string(api.AuthMethod)).To(Equal("both"))
		})
	})

	Describe("toAPIInvite", func() {
		It("leaves Email nil when the invite is not bound to an email", func() {
			now := time.Now()
			inv := &ent.Invite{
				ID:         3,
				Role:       "member",
				ExpiresAt:  now.Add(time.Hour),
				CreateTime: now,
			}
			api := toAPIInvite(inv)
			Expect(api.Id).To(Equal(uint32(3)))
			Expect(api.Email).To(BeNil())
			Expect(api.UsedAt).To(BeNil())
		})

		It("populates Email + UsedAt when the invite has them", func() {
			now := time.Now()
			used := now.Add(-time.Minute)
			inv := &ent.Invite{
				ID:         4,
				Email:      "c@x.com",
				Role:       "admin",
				ExpiresAt:  now.Add(time.Hour),
				UsedAt:     &used,
				CreateTime: now,
			}
			api := toAPIInvite(inv)
			Expect(api.Email).NotTo(BeNil())
			Expect(string(*api.Email)).To(Equal("c@x.com"))
			Expect(api.UsedAt).NotTo(BeNil())
		})
	})

	Describe("downloadClientToAPI", func() {
		It("omits Username and reports secrets unset when entry is bare", func() {
			e := config.DownloadClientEntry{
				Name:       "qbit",
				ClientType: "qbittorrent",
				Host:       "127.0.0.1",
				Port:       8080,
				AuthMethod: "password",
				Enabled:    true,
			}
			api := downloadClientToAPI(e)
			Expect(api.Username).To(BeNil())
			Expect(api.Enabled).To(BeTrue())
			Expect(api.ApiKeySet).To(BeFalse())
			Expect(api.PasswordSet).To(BeFalse())
		})

		It("populates optional fields and secret-set flags when present", func() {
			e := config.DownloadClientEntry{
				Name:       "tx",
				ClientType: "qbittorrent",
				Host:       "tx.local",
				Port:       9091,
				AuthMethod: "password",
				Enabled:    false,
				Username:   "admin",
				Password:   "secret",
				UseSSL:     true,
				Priority:   5,
			}
			api := downloadClientToAPI(e)
			Expect(api.Username).NotTo(BeNil())
			Expect(*api.Username).To(Equal("admin"))
			Expect(api.UseSsl).NotTo(BeNil())
			Expect(*api.UseSsl).To(BeTrue())
			Expect(api.Priority).NotTo(BeNil())
			Expect(*api.Priority).To(Equal(uint8(5)))
			Expect(api.PasswordSet).To(BeTrue())
		})
	})

	Describe("indexerToAPI", func() {
		It("reports ApiKeySet false when no key is set", func() {
			e := config.IndexerEntry{
				Name:     "torznab",
				Host:     "idx.example",
				Port:     443,
				Path:     "/api",
				UseSSL:   true,
				Protocol: "torznab",
				Enabled:  true,
			}
			api := indexerToAPI(e)
			Expect(api.ApiKeySet).To(BeFalse())
			Expect(api.Path).NotTo(BeNil())
			Expect(*api.Path).To(Equal("/api"))
		})

		It("populates Priority + ApiKeySet when entry carries them", func() {
			e := config.IndexerEntry{
				Name:     "torznab",
				Host:     "idx.example",
				Port:     443,
				Path:     "/api",
				UseSSL:   true,
				Protocol: "torznab",
				Enabled:  true,
				Priority: 10,
				APIKey:   "secret",
			}
			api := indexerToAPI(e)
			Expect(api.Priority).NotTo(BeNil())
			Expect(*api.Priority).To(Equal(uint8(10)))
			Expect(api.ApiKeySet).To(BeTrue())
		})
	})

	Describe("mediaFileToAPI", func() {
		It("maps probed fields into media_info", func() {
			now := time.Now()
			f := &ent.MediaFile{
				ID: 1, Path: "/m/Movie (2020)/Movie.mkv", Size: 100,
				Container: "matroska", VideoCodec: "hevc", Width: 3840, Height: 1608,
				DurationSeconds: 8130, AudioCodec: "eac3", AudioChannels: 6,
				Bitrate: 24500000, ProbedAt: &now,
			}
			out := mediaFileToAPI(f)
			Expect(out.MediaInfo).NotTo(BeNil())
			Expect(out.MediaInfo.VideoCodec).To(Equal("hevc"))
			Expect(out.MediaInfo.Width).To(Equal(3840))
		})

		It("marks a confirmed row only, not one in grace or on disk", func() {
			at := time.Now()
			confirmed := &ent.MediaFile{MissingSince: &at, MissingConfirmedAt: &at}
			inGrace := &ent.MediaFile{MissingSince: &at}

			Expect(mediaFileToAPI(confirmed).MissingFromDisk).To(HaveValue(BeTrue()))
			Expect(mediaFileToAPI(inGrace).MissingFromDisk).To(BeNil())
			Expect(mediaFileToAPI(&ent.MediaFile{}).MissingFromDisk).To(BeNil())
		})

		It("omits media_info when never probed", func() {
			f := &ent.MediaFile{ID: 1, Path: "/m/x.mkv", Size: 1}
			Expect(mediaFileToAPI(f).MediaInfo).To(BeNil())
		})
	})

	Describe("episodeToAPI", func() {
		It("maps media_info from the eager-loaded media file", func() {
			now := time.Now()
			e := &ent.Episode{ID: 1, Number: 1, Status: "available", Monitored: true}
			e.Edges.MediaFiles = []*ent.MediaFile{{
				ID: 1, Path: "/m/Show/S01E01.mkv", Size: 100,
				Container: "matroska", VideoCodec: "hevc", Width: 1920, Height: 1080,
				DurationSeconds: 1200, ProbedAt: &now,
			}}
			out := episodeToAPI(e, now, "")
			Expect(out.MediaInfo).NotTo(BeNil())
			Expect(out.MediaInfo.VideoCodec).To(Equal("hevc"))
		})

		It("marks an episode whose only file is confirmed missing", func() {
			at := time.Now()
			e := &ent.Episode{ID: 1, Number: 1}
			e.Edges.MediaFiles = []*ent.MediaFile{
				{ID: 1, MissingSince: &at, MissingConfirmedAt: &at},
			}
			Expect(episodeToAPI(e, at, "").MissingFromDisk).To(HaveValue(BeTrue()))

			e.Edges.MediaFiles = []*ent.MediaFile{{ID: 1, MissingSince: &at}}
			Expect(episodeToAPI(e, at, "").MissingFromDisk).To(BeNil())

			e.Edges.MediaFiles = []*ent.MediaFile{
				{ID: 1, MissingSince: &at, MissingConfirmedAt: &at},
				{ID: 2},
			}
			Expect(episodeToAPI(e, at, "").MissingFromDisk).To(BeNil())
		})

		It("omits media_info when the episode's file was never probed", func() {
			e := &ent.Episode{ID: 1, Number: 1, Status: "available", Monitored: true}
			e.Edges.MediaFiles = []*ent.MediaFile{
				{ID: 1, Path: "/m/Show/S01E01.mkv", Size: 100},
			}
			Expect(episodeToAPI(e, time.Now(), "").MediaInfo).To(BeNil())
		})

		It("omits media_info when the episode has no file", func() {
			e := &ent.Episode{ID: 1, Number: 1, Status: "wanted", Monitored: true}
			Expect(episodeToAPI(e, time.Now(), "").MediaInfo).To(BeNil())
		})

		It("reports grab_failures, omitting it at zero", func() {
			clean := &ent.Episode{
				ID:        1,
				Number:    1,
				Status:    "wanted",
				Monitored: true,
			}
			Expect(episodeToAPI(clean, time.Now(), "").GrabFailures).To(BeNil())

			failing := &ent.Episode{
				ID: 1, Number: 1, Status: "wanted", Monitored: true, GrabFailures: 2,
			}
			out := episodeToAPI(failing, time.Now(), "")
			Expect(out.GrabFailures).NotTo(BeNil())
			Expect(*out.GrabFailures).To(Equal(uint8(2)))
		})
	})

	Describe("toHistoryEntry", func() {
		It("names the track a single-track upload is for", func() {
			rec := &ent.DownloadRecord{ID: 3, Title: "Upload", Uploaded: true}
			rec.Edges.Album = &ent.Album{ID: 7, Title: "Proof 2"}
			rec.Edges.Track = &ent.Track{
				ID:       11,
				Title:    "Intro",
				Disc:     1,
				Position: 4,
			}
			out := toHistoryEntry(rec)
			Expect(out.Album).NotTo(BeNil())
			Expect(out.Track).To(Equal(&ActivityTrackRef{
				Id: 11, Title: "Intro", Disc: 1, Position: 4,
			}))
		})

		It("links an episode record to its series", func() {
			rec := &ent.DownloadRecord{ID: 5, Title: "Show.S01E02"}
			show := &ent.TVShow{ID: 9, Title: "Show"}
			season := &ent.Season{Number: 1}
			season.Edges.TvShow = show
			ep := &ent.Episode{Number: 2}
			ep.Edges.Season = season
			rec.Edges.AnchorEpisode = ep
			out := toHistoryEntry(rec)
			Expect(out.Episode).NotTo(BeNil())
			Expect(out.Episode.SeriesId).To(Equal(&show.ID))
		})

		It("names a discography pack's albums in id order", func() {
			rec := &ent.DownloadRecord{ID: 6, Title: "Upload", Uploaded: true}
			rec.Edges.Artist = &ent.Artist{ID: 1, Name: "Dr. Peacock"}
			rec.Edges.Albums = []*ent.Album{
				{ID: 12, Title: "Trip to Lapland"},
				{ID: 3, Title: "Proof 1"},
			}
			out := toHistoryEntry(rec)
			Expect(out.Artist).NotTo(BeNil())
			Expect(out.Artist.AlbumCount).To(Equal(2))
			Expect(out.Artist.AlbumTitles).
				To(Equal(&[]string{"Proof 1", "Trip to Lapland"}))
		})

		It("leaves track absent on any other record", func() {
			rec := &ent.DownloadRecord{ID: 4, Title: "Artist - Album"}
			rec.Edges.Album = &ent.Album{ID: 7, Title: "Proof 2"}
			Expect(toHistoryEntry(rec).Track).To(BeNil())
		})
	})
})
