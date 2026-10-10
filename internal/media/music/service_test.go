package music

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("Music service", Label("unit", "integration", "music"), func() {
	var e *env

	BeforeEach(func() {
		e = newEnv(false)
	})

	day := func(d int) *time.Time {
		t := time.Now().Add(time.Duration(d) * 24 * time.Hour)
		return &t
	}

	details := func(groups ...metadata.ReleaseGroupInfo) *metadata.ArtistDetails {
		return &metadata.ArtistDetails{
			MBID: "mbid-1", Name: "Nirvana", SortName: "Nirvana", Type: "group",
			Genre:  "Grunge",
			Origin: "Aberdeen, United States", DeezerID: 415,
			WikidataID: "Q11649",
			Members: []metadata.ArtistMemberInfo{{
				Name: "Kurt Cobain", MBID: "m-1", Instruments: []string{"guitar"},
			}},
			ReleaseGroups: groups,
		}
	}

	rg := func(mbid, title string, date *time.Time) metadata.ReleaseGroupInfo {
		return metadata.ReleaseGroupInfo{
			MBID:        mbid,
			Title:       title,
			Type:        metadata.AlbumTypeAlbum,
			ReleaseDate: date,
		}
	}

	// serveAlbums answers the background hydration of every release group.
	serveAlbums := func(mbids ...string) {
		for _, m := range mbids {
			e.provider.EXPECT().GetReleaseGroup(mock.Anything, m).
				Return(hydrated(m, "Track "+m), nil).Maybe()
		}
		e.posters.EXPECT().
			Fetch(mock.Anything, "albums", mock.Anything, mock.Anything).
			Return(nil).
			Maybe()
	}

	Describe("Add", func() {
		It("creates the artist, its members and one stub per release group", func() {
			e.provider.EXPECT().GetArtist(mock.Anything, "mbid-1").
				Return(details(
					rg("rg-1", "Nevermind", day(-9000)),
					rg("rg-2", "In Utero", day(-8000)),
				), nil).Once()
			serveAlbums("rg-1", "rg-2")

			a, err := e.svc.Add(e.ctx, AddParams{MBID: "mbid-1"})
			Expect(err).NotTo(HaveOccurred())
			Expect(a.Name).To(Equal("Nirvana"))
			Expect(
				a.Path,
			).To(Equal(filepath.Join(config.Get().Library.MusicPath, "Nirvana")))
			Expect(a.Type).To(Equal(artist.TypeGroup))
			Expect(a.Origin).To(Equal("Aberdeen, United States"))
			Expect(a.Genre).To(Equal("Grunge"))
			Expect(a.DeezerID).To(Equal(uint32(415)))
			Expect(a.WikidataID).To(Equal("Q11649"))
			Expect(a.Edges.Members).To(HaveLen(1))
			Expect(a.Edges.Albums).To(HaveLen(2))
			for _, al := range a.Edges.Albums {
				Expect(
					al.MetadataFetchedAt,
				).To(BeNil(), "tracks arrive in the background")
			}
			e.settle(a.ID)
		})

		It("hydrates the albums in the background", func() {
			e.provider.EXPECT().GetArtist(mock.Anything, "mbid-1").
				Return(details(rg("rg-1", "Nevermind", day(-9000))), nil).Once()
			serveAlbums("rg-1")

			a, err := e.svc.Add(e.ctx, AddParams{MBID: "mbid-1"})
			Expect(err).NotTo(HaveOccurred())
			e.settle(a.ID)

			got := e.client.Album.Query().OnlyX(e.ctx)
			Expect(got.MetadataFetchedAt).NotTo(BeNil())
			Expect(got.Label).To(Equal("DGC"))
			Expect(got.QueryTracks().CountX(e.ctx)).To(Equal(1))
		})

		DescribeTable(
			"monitors new release groups per the policy",
			func(policy string, want map[string]bool) {
				e.provider.EXPECT().GetArtist(mock.Anything, "mbid-1").
					Return(details(
						rg("past", "Past", day(-100)),
						rg("soon", "Soon", day(100)),
						rg("undated", "Undated", nil),
					), nil).Once()
				serveAlbums("past", "soon", "undated")

				a, err := e.svc.Add(
					e.ctx,
					AddParams{MBID: "mbid-1", Monitor: policy},
				)
				Expect(err).NotTo(HaveOccurred())
				got := map[string]bool{}
				for _, al := range a.Edges.Albums {
					got[al.Mbid] = al.Monitored
				}
				Expect(got).To(Equal(want))
				e.settle(a.ID)
			},
			Entry(
				"all",
				"all",
				map[string]bool{"past": true, "soon": true, "undated": true},
			),
			Entry("empty means all", "",
				map[string]bool{"past": true, "soon": true, "undated": true}),
			Entry("future", "future",
				map[string]bool{"past": false, "soon": true, "undated": true}),
			Entry("manual", "manual",
				map[string]bool{"past": false, "soon": false, "undated": false}),
			Entry("none", "none",
				map[string]bool{"past": false, "soon": false, "undated": false}),
		)

		It("rejects an unknown quality profile before any provider call", func() {
			_, err := e.svc.Add(
				e.ctx,
				AddParams{MBID: "mbid-1", QualityProfile: "nope"},
			)
			Expect(err).To(MatchError(ErrUnknownProfile))
			Expect(e.client.Artist.Query().Count(e.ctx)).To(BeZero())
		})

		It("rejects an unknown monitor policy", func() {
			_, err := e.svc.Add(
				e.ctx,
				AddParams{MBID: "mbid-1", Monitor: "sometimes"},
			)
			Expect(err).To(MatchError(ErrInvalidMonitor))
		})

		It("accepts a known quality profile", func() {
			e.provider.EXPECT().GetArtist(mock.Anything, "mbid-1").
				Return(details(), nil).Once()
			a, err := e.svc.Add(
				e.ctx,
				AddParams{MBID: "mbid-1", QualityProfile: "any"},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(a.QualityProfile).To(Equal("any"))
			e.settle(a.ID)
		})

		It("rejects a duplicate mbid without asking MusicBrainz", func() {
			e.client.Artist.Create().
				SetMbid("mbid-1").
				SetName("Nirvana").
				SaveX(e.ctx)
			_, err := e.svc.Add(e.ctx, AddParams{MBID: "mbid-1"})
			Expect(err).To(MatchError(ErrArtistExists))
		})

		It("passes a rate limit through and creates nothing", func() {
			e.provider.EXPECT().GetArtist(mock.Anything, "mbid-1").
				Return(nil, &metadata.RateLimitedError{RetryAfter: time.Minute}).
				Once()
			_, err := e.svc.Add(e.ctx, AddParams{MBID: "mbid-1"})
			Expect(err).To(MatchError(metadata.ErrRateLimited))
			Expect(e.client.Artist.Query().Count(e.ctx)).To(BeZero())
		})

		It(
			"costs one set of MusicBrainz calls for a lookup followed by an add",
			func() {
				e.provider.EXPECT().GetArtist(mock.Anything, "mbid-1").
					Return(details(rg("rg-1", "Nevermind", day(-9000))), nil).Once()
				serveAlbums("rg-1")

				_, err := e.svc.LookupArtist(e.ctx, "mbid-1", "en")
				Expect(err).NotTo(HaveOccurred())
				a, err := e.svc.Add(e.ctx, AddParams{MBID: "mbid-1"})
				Expect(err).NotTo(HaveOccurred())
				e.settle(a.ID)
			},
		)
	})

	Describe("Get", func() {
		It("maps a missing row to ErrArtistNotFound", func() {
			_, err := e.svc.Get(e.ctx, 999)
			Expect(err).To(MatchError(ErrArtistNotFound))
		})
	})

	Describe("RefreshOne", func() {
		It(
			"rewrites the artist, adds new release groups under the policy and hydrates them",
			func() {
				a, albums := e.seedArtist(
					"Nirvana",
					albumSeed{
						mbid:      "rg-1",
						title:     "Nevermind",
						monitored: true,
						tracks:    []string{"A"},
					},
				)
				e.client.Artist.UpdateOneID(a.ID).
					SetMonitor(artist.MonitorFuture).
					ExecX(e.ctx)
				e.client.Album.UpdateOneID(albums[0].ID).
					SetMonitored(false).SetStatus(album.StatusSkipped).ExecX(e.ctx)

				d := details(
					rg("rg-1", "Nevermind (Remaster)", day(-9000)),
					rg("old", "Old", day(-500)),
					rg("new", "New", day(40)),
				)
				d.Genre = "Rock"
				e.provider.EXPECT().
					GetArtist(mock.Anything, "mbid-Nirvana").
					Return(d, nil).
					Once()
				e.provider.EXPECT().GetReleaseGroup(mock.Anything, "old").
					Return(hydrated("old", "O"), nil).Once()
				e.provider.EXPECT().GetReleaseGroup(mock.Anything, "new").
					Return(hydrated("new", "N"), nil).Once()
				e.posters.EXPECT().
					Fetch(mock.Anything, "albums", mock.Anything, mock.Anything).
					Return(nil).
					Maybe()

				got, err := e.svc.RefreshOne(e.ctx, a.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.LastRefreshedAt).NotTo(BeNil())
				Expect(got.Edges.Albums).To(HaveLen(3))
				Expect(got.Genre).To(Equal("Rock"))
				Expect(got.Edges.Members).To(HaveLen(1))

				old := e.client.Album.GetX(e.ctx, albums[0].ID)
				Expect(old.Title).To(Equal("Nevermind (Remaster)"))
				Expect(old.Monitored).To(BeFalse())
				Expect(old.Status).To(Equal(album.StatusSkipped))
				Expect(
					e.client.Album.Query().
						Where(album.MbidEQ("old")).
						OnlyX(e.ctx).
						Monitored,
				).
					To(BeFalse(), "future does not monitor a past release")
				Expect(
					e.client.Album.Query().
						Where(album.MbidEQ("new")).
						OnlyX(e.ctx).
						Monitored,
				).
					To(BeTrue())
				e.settle(a.ID)
				Expect(
					e.client.Album.Query().Where(album.MbidEQ("new")).OnlyX(e.ctx).
						MetadataFetchedAt,
				).NotTo(BeNil())
			},
		)

		It("hydrates a known album that came back without tracks long ago", func() {
			a, albums := e.seedArtist("Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind", monitored: true},
			)
			e.client.Album.UpdateOneID(albums[0].ID).
				SetMetadataFetchedAt(time.Now().Add(-10 * 24 * time.Hour)).
				ExecX(e.ctx)
			e.provider.EXPECT().GetArtist(mock.Anything, "mbid-Nirvana").
				Return(details(rg("rg-1", "Nevermind", day(-9000))), nil).Once()
			e.provider.EXPECT().GetReleaseGroup(mock.Anything, "rg-1").
				Return(hydrated("rg-1", "A"), nil).Once()
			e.posters.EXPECT().
				Fetch(mock.Anything, "albums", mock.Anything, mock.Anything).
				Return(nil).
				Maybe()

			_, err := e.svc.RefreshOne(e.ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			e.settle(a.ID)
			Expect(
				e.client.Album.GetX(e.ctx, albums[0].ID).QueryTracks().CountX(e.ctx),
			).
				To(Equal(1))
		})

		It("leaves a recently fetched trackless album for the sweep", func() {
			a, albums := e.seedArtist("Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind", monitored: true},
			)
			e.client.Album.UpdateOneID(albums[0].ID).
				SetMetadataFetchedAt(time.Now().Add(-time.Hour)).ExecX(e.ctx)
			e.provider.EXPECT().GetArtist(mock.Anything, "mbid-Nirvana").
				Return(details(rg("rg-1", "Nevermind", day(-9000))), nil).Once()
			fetched := make(chan struct{})
			e.posters.EXPECT().
				Fetch(mock.Anything, "albums", mock.Anything, mock.Anything).
				Run(func(context.Context, string, uint32, string) { close(fetched) }).
				Return(nil).
				Once()

			_, err := e.svc.RefreshOne(e.ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			e.settle(a.ID)
			Eventually(fetched).Should(BeClosed())
		})

		It("maps a missing artist to ErrArtistNotFound", func() {
			_, err := e.svc.RefreshOne(e.ctx, 999)
			Expect(err).To(MatchError(ErrArtistNotFound))
		})

		It("passes a rate limit through", func() {
			a, _ := e.seedArtist("Nirvana")
			e.provider.EXPECT().GetArtist(mock.Anything, "mbid-Nirvana").
				Return(nil, &metadata.RateLimitedError{RetryAfter: time.Second}).
				Once()
			_, err := e.svc.RefreshOne(e.ctx, a.ID)
			Expect(err).To(MatchError(metadata.ErrRateLimited))
		})
	})

	Describe("SetArtistQualityProfile", func() {
		It("stores a known profile and clears with an empty one", func() {
			a, _ := e.seedArtist("Nirvana")
			Expect(e.svc.SetArtistQualityProfile(e.ctx, a.ID, "any")).To(Succeed())
			Expect(e.client.Artist.GetX(e.ctx, a.ID).QualityProfile).To(Equal("any"))
			Expect(e.svc.SetArtistQualityProfile(e.ctx, a.ID, "")).To(Succeed())
			Expect(e.client.Artist.GetX(e.ctx, a.ID).QualityProfile).To(BeEmpty())
		})

		It("rejects an unknown profile", func() {
			a, _ := e.seedArtist("Nirvana")
			Expect(e.svc.SetArtistQualityProfile(e.ctx, a.ID, "nope")).
				To(MatchError(ErrUnknownProfile))
		})

		It("maps a missing artist to ErrArtistNotFound", func() {
			Expect(e.svc.SetArtistQualityProfile(e.ctx, 999, "any")).
				To(MatchError(ErrArtistNotFound))
		})
	})

	Describe("SetArtistMonitor", func() {
		It("stores the policy and applies it to the albums", func() {
			a, _ := e.seedArtist(
				"Nirvana",
				albumSeed{
					mbid:      "past",
					title:     "Past",
					date:      day(-30),
					monitored: true,
				},
				albumSeed{mbid: "soon", title: "Soon", date: day(30)},
			)
			Expect(e.svc.SetArtistMonitor(e.ctx, a.ID, "future")).To(Succeed())
			Expect(
				e.client.Artist.GetX(e.ctx, a.ID).Monitor,
			).To(Equal(artist.MonitorFuture))
			Expect(
				e.client.Album.Query().
					Where(album.MbidEQ("past")).
					OnlyX(e.ctx).
					Monitored,
			).
				To(BeFalse())
			Expect(
				e.client.Album.Query().
					Where(album.MbidEQ("soon")).
					OnlyX(e.ctx).
					Monitored,
			).
				To(BeTrue())
		})

		It("rejects an unknown policy", func() {
			a, _ := e.seedArtist("Nirvana")
			Expect(
				e.svc.SetArtistMonitor(e.ctx, a.ID, "x"),
			).To(MatchError(ErrInvalidMonitor))
		})

		It("maps a missing artist to ErrArtistNotFound", func() {
			Expect(
				e.svc.SetArtistMonitor(e.ctx, 999, "all"),
			).To(MatchError(ErrArtistNotFound))
		})
	})

	Describe("SetAlbumMonitored", func() {
		It("sets the flag without touching the artist's policy", func() {
			a, albums := e.seedArtist("Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind", monitored: true})
			Expect(e.svc.SetAlbumMonitored(e.ctx, albums[0].ID, false)).To(Succeed())
			Expect(e.client.Album.GetX(e.ctx, albums[0].ID).Monitored).To(BeFalse())
			Expect(
				e.client.Artist.GetX(e.ctx, a.ID).Monitor,
			).To(Equal(artist.MonitorAll))
		})

		It("maps a missing album to ErrAlbumNotFound", func() {
			Expect(
				e.svc.SetAlbumMonitored(e.ctx, 999, false),
			).To(MatchError(ErrAlbumNotFound))
		})
	})

	Describe("Delete", func() {
		seedFile := func(albums []*ent.Album) string {
			GinkgoHelper()
			dir := filepath.Join(
				config.Get().Library.MusicPath,
				"Nirvana",
				"Nevermind",
			)
			Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
			f := filepath.Join(dir, "01.flac")
			Expect(os.WriteFile(f, []byte("x"), 0o600)).To(Succeed())
			tr := albums[0].QueryTracks().FirstX(e.ctx)
			e.addFile(tr, f, "lossless")
			return f
		}

		It("cascades, keeps files unless asked and evicts the posters", func() {
			a, albums := e.seedArtist("Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind", tracks: []string{"A"}})
			f := seedFile(albums)
			e.posters.EXPECT().Remove("artists", a.ID).Return(nil).Once()
			e.posters.EXPECT().Remove("albums", albums[0].ID).Return(nil).Once()

			Expect(e.svc.Delete(e.ctx, a.ID, false)).To(Succeed())
			Expect(f).To(BeAnExistingFile())
			Expect(e.client.Album.Query().Count(e.ctx)).To(BeZero())
			Expect(e.client.Track.Query().Count(e.ctx)).To(BeZero())
		})

		It("removes the files on disk when asked", func() {
			a, albums := e.seedArtist("Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind", tracks: []string{"A"}})
			f := seedFile(albums)
			e.posters.EXPECT().
				Remove(mock.Anything, mock.Anything).
				Return(nil).
				Twice()

			Expect(e.svc.Delete(e.ctx, a.ID, true)).To(Succeed())
			Expect(f).NotTo(BeAnExistingFile())
		})

		It("maps a missing artist to ErrArtistNotFound", func() {
			Expect(e.svc.Delete(e.ctx, 999, false)).To(MatchError(ErrArtistNotFound))
		})
	})

	Describe("GrabAlbum", func() {
		result := indexer.SearchResult{
			Title:    "Nirvana - Nevermind (1991) [FLAC]",
			Download: "magnet:?xt=urn:btih:abc",
		}

		It("grabs the release and marks the album downloading", func() {
			_, albums := e.seedArtist(
				"Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind"},
			)
			e.dl.EXPECT().GrabAlbum(mock.Anything, result, albums[0].ID).
				Return(&ent.DownloadRecord{}, nil).Once()

			Expect(e.svc.GrabAlbumRelease(e.ctx, albums[0].ID, result)).To(Succeed())
			Expect(e.client.Album.GetX(e.ctx, albums[0].ID).Status).
				To(Equal(album.StatusDownloading))
		})

		It("flags the record to replace the old files when asked", func() {
			_, albums := e.seedArtist(
				"Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind"},
			)
			rec := e.client.DownloadRecord.Create().SetTitle("t").
				SetAlbumID(albums[0].ID).SaveX(e.ctx)
			e.dl.EXPECT().
				GrabAlbum(mock.Anything, result, albums[0].ID).
				Return(rec, nil).
				Once()

			Expect(e.svc.GrabAlbum(e.ctx, albums[0].ID, result, true)).To(Succeed())
			Expect(e.client.DownloadRecord.GetX(e.ctx, rec.ID).ReplaceMode).
				To(Equal(downloadrecord.ReplaceModeAll))
		})

		It(
			"leaves the album wanted and grab_failures at zero when the grab fails",
			func() {
				_, albums := e.seedArtist(
					"Nirvana",
					albumSeed{mbid: "rg-1", title: "Nevermind"},
				)
				boom := errors.New("client unreachable")
				e.dl.EXPECT().
					GrabAlbum(mock.Anything, result, albums[0].ID).
					Return(nil, boom).
					Once()

				err := e.svc.GrabAlbumRelease(e.ctx, albums[0].ID, result)
				Expect(err).To(MatchError(boom))
				Expect(err).To(MatchError(ContainSubstring("grab album")))
				after := e.client.Album.GetX(e.ctx, albums[0].ID)
				Expect(after.Status).To(Equal(album.StatusWanted))
				Expect(after.GrabFailures).To(BeZero())
			},
		)

		It("maps a missing album to ErrAlbumNotFound", func() {
			Expect(
				e.svc.GrabAlbumRelease(e.ctx, 999, result),
			).To(MatchError(ErrAlbumNotFound))
		})
	})
})
