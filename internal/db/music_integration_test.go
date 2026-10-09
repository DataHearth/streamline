package db

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	entartist "github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/musiccredit"
	"github.com/datahearth/streamline/ent/track"
)

var _ = Describe("Music persistence", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
		now    time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		now = time.Now()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
	})

	day := func(d int) *time.Time {
		t := now.Add(time.Duration(d) * 24 * time.Hour)
		return &t
	}

	// seed adds Nirvana with a hydrated Nevermind (released) and a stub In Utero.
	seed := func() *ent.Artist {
		GinkgoHelper()
		early := time.Date(1991, 9, 24, 0, 0, 0, 0, time.UTC)
		late := time.Date(1993, 9, 21, 0, 0, 0, 0, time.UTC)
		a, err := store.CreateArtist(ctx, CreateArtistParams{
			MBID: "a-1", Name: "Nirvana", SortName: "Nirvana", Genre: "Grunge",
			Monitor: entartist.MonitorAll, Type: "group", Since: 1987,
			Members: []MemberSeed{
				{
					Name: "Kurt Cobain",
					MBID: "m-1",
					Instruments: []string{
						"guitar",
						"vocals",
					},
					FromYear: 1987,
					ToYear:   1994,
				},
			},
			Albums: []AlbumSeed{
				{
					MBID:        "rg-2",
					Title:       "In Utero",
					Type:        "album",
					ReleaseDate: &late,
					Monitored:   true,
				},
				{
					MBID:        "rg-1",
					Title:       "Nevermind",
					Type:        "album",
					ReleaseDate: &early,
					Monitored:   true,
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		hydrateAlbum(
			ctx,
			store,
			"rg-1",
			TrackSeed{MBID: "t-2", Title: "In Bloom", Disc: 1, Position: 2},
			TrackSeed{
				MBID:     "t-1",
				Title:    "Smells Like Teen Spirit",
				Disc:     1,
				Position: 1,
			},
		)
		return a
	}

	albumByMBID := func(mbid string) *ent.Album {
		GinkgoHelper()
		return client.Album.Query().Where(album.MbidEQ(mbid)).OnlyX(ctx)
	}

	Describe("CreateArtist", func() {
		It(
			"creates the artist with its members and one stub per release group",
			func() {
				a := seed()
				Expect(a.Type).To(Equal(entartist.TypeGroup))
				Expect(a.Since).To(Equal(uint16(1987)))
				Expect(a.Monitor).To(Equal(entartist.MonitorAll))
				Expect(a.Edges.Members).To(HaveLen(1))
				Expect(a.Edges.Members[0].Instruments).To(Equal("guitar,vocals"))
				Expect(a.Edges.Albums).To(HaveLen(2))
				Expect(albumByMBID("rg-2").MetadataFetchedAt).To(BeNil())
			},
		)

		It("defaults the monitor policy to all", func() {
			a, err := store.CreateArtist(
				ctx,
				CreateArtistParams{MBID: "a-9", Name: "X"},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(a.Monitor).To(Equal(entartist.MonitorAll))
		})

		It("orders the albums newest first and the tracks by position", func() {
			a, err := store.FindArtistByID(ctx, seed().ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(a.Edges.Albums[0].Mbid).To(Equal("rg-2"))
			tracks := a.Edges.Albums[1].Edges.Tracks
			Expect(tracks[0].Title).To(Equal("Smells Like Teen Spirit"))
			Expect(tracks[1].Title).To(Equal("In Bloom"))
		})
	})

	Describe("SetAlbumStatus", func() {
		It("moves only from the listed statuses and reports whether it did", func() {
			a := seed()
			id := a.Edges.Albums[0].ID
			Expect(client.Album.GetX(ctx, id).Status).To(Equal(album.StatusWanted))

			moved, err := store.SetAlbumStatus(ctx, id,
				[]album.Status{album.StatusPaused, album.StatusSkipped},
				album.StatusDownloading)
			Expect(err).NotTo(HaveOccurred())
			Expect(moved).To(BeFalse())

			moved, err = store.SetAlbumStatus(ctx, id,
				[]album.Status{album.StatusPaused, album.StatusWanted},
				album.StatusDownloading)
			Expect(err).NotTo(HaveOccurred())
			Expect(moved).To(BeTrue())
			Expect(client.Album.GetX(ctx, id).Status).
				To(Equal(album.StatusDownloading))
		})
	})

	It("loads an album with its tracks ordered by disc and position", func() {
		seed()
		row, err := store.FindAlbumByID(ctx, albumByMBID("rg-1").ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(row.Edges.Tracks).To(HaveLen(2))
		Expect(row.Edges.Tracks[0].Title).To(Equal("Smells Like Teen Spirit"))
		Expect(row.Edges.Artist).NotTo(BeNil())
	})

	It("reports an unknown album id as not found", func() {
		_, err := store.FindAlbumByID(ctx, 999)
		Expect(ent.IsNotFound(err)).To(BeTrue())
	})

	It("loads a track with its album and artist", func() {
		seed()
		tr := client.Track.Query().Where(track.MbidEQ("t-1")).OnlyX(ctx)
		got, err := store.FindTrackByID(ctx, tr.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Edges.Album.Edges.Artist.Name).To(Equal("Nirvana"))
	})

	It("sets the artist's quality profile", func() {
		a := seed()
		Expect(store.SetArtistQualityProfile(ctx, a.ID, "lossless")).To(Succeed())
		Expect(client.Artist.GetX(ctx, a.ID).QualityProfile).To(Equal("lossless"))
	})

	It("returns nil, nil for an unknown mbid", func() {
		row, err := store.FindArtistByMBID(ctx, "nope")
		Expect(err).NotTo(HaveOccurred())
		Expect(row).To(BeNil())
	})

	It("maps the library artists among the given mbids to their ids", func() {
		a := seed()
		got, err := store.ArtistIDsByMBID(ctx, []string{"a-1", "nope"})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(map[string]uint32{"a-1": a.ID}))
		empty, err := store.ArtistIDsByMBID(ctx, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(empty).To(BeEmpty())
	})

	Describe("SetArtistMonitor", func() {
		var a *ent.Artist

		BeforeEach(func() {
			var err error
			a, err = store.CreateArtist(ctx, CreateArtistParams{
				MBID: "a-1", Name: "X",
				Albums: []AlbumSeed{
					{
						MBID:        "past",
						Title:       "Past",
						ReleaseDate: day(-30),
						Monitored:   true,
					},
					{
						MBID:        "soon",
						Title:       "Soon",
						ReleaseDate: day(30),
						Monitored:   false,
					},
					{MBID: "undated", Title: "Undated", Monitored: false},
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		monitored := func() map[string]bool {
			out := map[string]bool{}
			for _, al := range client.Album.Query().AllX(ctx) {
				out[al.Mbid] = al.Monitored
			}
			return out
		}

		DescribeTable("applies the policy to the existing albums",
			func(policy entartist.Monitor, want map[string]bool) {
				Expect(store.SetArtistMonitor(ctx, a.ID, policy, now)).To(Succeed())
				Expect(monitored()).To(Equal(want))
				Expect(client.Artist.GetX(ctx, a.ID).Monitor).To(Equal(policy))
			},
			Entry("all", entartist.MonitorAll,
				map[string]bool{"past": true, "soon": true, "undated": true}),
			Entry("none", entartist.MonitorNone,
				map[string]bool{"past": false, "soon": false, "undated": false}),
			Entry("future", entartist.MonitorFuture,
				map[string]bool{"past": false, "soon": true, "undated": true}),
			Entry("manual leaves the flags alone", entartist.MonitorManual,
				map[string]bool{"past": true, "soon": false, "undated": false}),
		)

		It("never touches an album's status", func() {
			client.Album.Update().Where(album.MbidEQ("past")).
				SetStatus(album.StatusAvailable).ExecX(ctx)
			Expect(store.SetArtistMonitor(ctx, a.ID, entartist.MonitorNone, now)).
				To(Succeed())
			Expect(albumByMBID("past").Status).To(Equal(album.StatusAvailable))
		})
	})

	Describe("RefreshArtist", func() {
		It("refreshes known albums, creates new ones and returns their ids", func() {
			a := seed()
			rg1 := albumByMBID("rg-1")
			Expect(store.SetAlbumMonitored(ctx, rg1.ID, false)).To(Succeed())
			client.Album.UpdateOneID(rg1.ID).
				SetStatus(album.StatusAvailable).ExecX(ctx)

			created, err := store.RefreshArtist(ctx, a.ID, RefreshArtistParams{
				Name: "Nirvana", Type: "group", Origin: "Aberdeen, United States",
				Since: 1987, Genre: "Rock", DeezerID: 415, WikidataID: "Q11649",
				RefreshedAt: now,
				Members: []MemberSeed{
					{Name: "Dave Grohl", MBID: "m-2"},
					{Name: "Krist Novoselic", MBID: "m-3"},
				},
				Albums: []AlbumSeed{
					{
						MBID:      "rg-1",
						Title:     "Nevermind (Remaster)",
						Type:      "album",
						Monitored: true,
					},
					{MBID: "rg-3", Title: "Bleach", Type: "album", Monitored: true},
					{
						MBID:      "rg-4",
						Title:     "Incesticide",
						Type:      "album",
						Monitored: false,
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(created).To(HaveLen(2))

			got := client.Album.GetX(ctx, rg1.ID)
			Expect(got.Title).To(Equal("Nevermind (Remaster)"))
			Expect(got.Monitored).To(BeFalse(), "a known album keeps its flag")
			Expect(got.Status).To(Equal(album.StatusAvailable))
			Expect(albumByMBID("rg-3").Monitored).To(BeTrue())
			Expect(albumByMBID("rg-4").Monitored).To(BeFalse())

			row := client.Artist.GetX(ctx, a.ID)
			Expect(row.LastRefreshedAt).NotTo(BeNil())
			Expect(row.Genre).To(Equal("Rock"))
			Expect(row.DeezerID).To(Equal(uint32(415)))
			Expect(row.WikidataID).To(Equal("Q11649"))
			Expect(row.Origin).To(Equal("Aberdeen, United States"))

			members := row.QueryMembers().AllX(ctx)
			Expect(members).To(HaveLen(2))
			Expect(members[0].Name).To(Equal("Dave Grohl"))
		})

		It("clears the type when MusicBrainz states none", func() {
			a := seed()
			_, err := store.RefreshArtist(ctx, a.ID, RefreshArtistParams{
				Name: "Nirvana", RefreshedAt: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(client.Artist.GetX(ctx, a.ID).Type).To(BeEmpty())
		})
	})

	Describe("SetArtistDetails", func() {
		It("writes the overviews that are present and stamps the step", func() {
			a := seed()
			Expect(store.SetArtistDetails(ctx, a.ID, ArtistDetailsParams{
				Overview: "English", OverviewSource: "https://en/x",
			}, now)).To(Succeed())
			row := client.Artist.GetX(ctx, a.ID)
			Expect(row.Overview).To(Equal("English"))
			Expect(row.OverviewSource).To(Equal("https://en/x"))
			Expect(row.OverviewFr).To(BeEmpty())
			Expect(row.DetailsFetchedAt).NotTo(BeNil())

			Expect(store.SetArtistDetails(ctx, a.ID, ArtistDetailsParams{
				OverviewFR: "Francais", OverviewSourceFR: "https://fr/x",
			}, now)).To(Succeed())
			row = client.Artist.GetX(ctx, a.ID)
			Expect(
				row.Overview,
			).To(Equal("English"), "an empty locale keeps what it had")
			Expect(row.OverviewFr).To(Equal("Francais"))
		})
	})

	Describe("SetAlbumHydration", func() {
		var rg2 *ent.Album

		BeforeEach(func() {
			seed()
			rg2 = albumByMBID("rg-2")
		})

		heavy := func() HydrationParams {
			return HydrationParams{
				ReleaseMBID: "rel-1", Barcode: "0720642442524",
				Label: "DGC", CatalogNumber: "DGCD-24425", Country: "US",
				Media: []string{"cd", "vinyl"}, Studio: "Sound City",
				CreditsComplete: true,
				Tracks: []TrackSeed{
					{
						MBID:      "r-1",
						Title:     "Serve the Servants",
						Disc:      1,
						Position:  1,
						Duration:  217,
						Bonus:     true,
						Featuring: []PersonSeed{{Name: "A Guest", MBID: "g-1"}},
						Writers:   []PersonSeed{{Name: "Kurt Cobain", MBID: "m-1"}},
					},
					{
						MBID:     "r-2",
						Title:    "Scentless Apprentice",
						Disc:     1,
						Position: 2,
					},
				},
				Credits: []CreditSeed{
					{
						Name: "Steve Albini", MBID: "p-1",
						Role: "producer",
					},
					{Name: "Bob Weston", Role: "mastering"},
				},
				Performers: []PerformerSeed{{
					Name: "Pat Smear", MBID: "p-2",
					Instruments: []string{"guitar"}, Guest: true,
				}},
			}
		}

		It("stores the album facts, tracks and every credit in one go", func() {
			Expect(store.SetAlbumHydration(ctx, rg2.ID, heavy(), now)).To(Succeed())
			got, err := store.FindAlbumByID(ctx, rg2.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Label).To(Equal("DGC"))
			Expect(got.CatalogNumber).To(Equal("DGCD-24425"))
			Expect(got.Country).To(Equal("US"))
			Expect(got.Media).To(Equal("cd,vinyl"))
			Expect(got.Studio).To(Equal("Sound City"))
			Expect(got.Barcode).To(Equal("0720642442524"))
			Expect(got.ReleaseMbid).To(Equal("rel-1"))
			Expect(got.MetadataFetchedAt).NotTo(BeNil())
			Expect(got.CreditsFetchedAt).NotTo(BeNil())
			Expect(got.Edges.Tracks).To(HaveLen(2))

			first := got.Edges.Tracks[0]
			Expect(first.Bonus).To(BeTrue())
			Expect(first.Duration).To(Equal(uint32(217)))
			Expect(first.Edges.Credits).To(HaveLen(2))
			kinds := []musiccredit.Kind{
				first.Edges.Credits[0].Kind,
				first.Edges.Credits[1].Kind,
			}
			Expect(
				kinds,
			).To(ConsistOf(musiccredit.KindFeaturing, musiccredit.KindWriter))

			Expect(got.Edges.Credits).To(HaveLen(3))
			perf := client.MusicCredit.Query().
				Where(musiccredit.KindEQ(musiccredit.KindPerformer)).OnlyX(ctx)
			Expect(perf.Guest).To(BeTrue())
			Expect(perf.Instruments).To(Equal("guitar"))
		})

		It("updates tracks in place so a track keeps its file", func() {
			Expect(store.SetAlbumHydration(ctx, rg2.ID, heavy(), now)).To(Succeed())
			tr := client.Track.Query().Where(track.MbidEQ("r-1")).OnlyX(ctx)
			client.MediaFile.Create().SetPath("/m/1.flac").SetSize(10).
				SetTrackID(tr.ID).ExecX(ctx)

			p := heavy()
			p.Tracks[0].Title = "Serve the Servants (2013)"
			Expect(store.SetAlbumHydration(ctx, rg2.ID, p, now)).To(Succeed())

			again := client.Track.Query().Where(track.MbidEQ("r-1")).OnlyX(ctx)
			Expect(again.ID).To(Equal(tr.ID))
			Expect(again.Title).To(Equal("Serve the Servants (2013)"))
			Expect(again.QueryMediaFiles().CountX(ctx)).To(Equal(1))
			Expect(
				client.Track.Query().Where(track.HasAlbumWith(album.IDEQ(rg2.ID))).
					CountX(ctx),
			).To(Equal(2))
		})

		It(
			"replaces credits on a repeat heavy call instead of stacking them",
			func() {
				Expect(
					store.SetAlbumHydration(ctx, rg2.ID, heavy(), now),
				).To(Succeed())
				Expect(
					store.SetAlbumHydration(ctx, rg2.ID, heavy(), now),
				).To(Succeed())
				Expect(client.MusicCredit.Query().Where(
					musiccredit.HasAlbumWith(album.IDEQ(rg2.ID)),
				).CountX(ctx)).To(Equal(3))
				Expect(client.MusicCredit.Query().Where(
					musiccredit.KindEQ(musiccredit.KindWriter),
				).CountX(ctx)).To(Equal(1))
			},
		)

		It("keeps stored credits when only the light fallback succeeded", func() {
			Expect(store.SetAlbumHydration(ctx, rg2.ID, heavy(), now)).To(Succeed())
			light := heavy()
			light.CreditsComplete = false
			light.Credits, light.Performers, light.Studio = nil, nil, ""
			light.Tracks[0].Writers = nil
			light.Tracks[0].Featuring = nil
			Expect(store.SetAlbumHydration(ctx, rg2.ID, light, now)).To(Succeed())

			got := client.Album.GetX(ctx, rg2.ID)
			Expect(got.Studio).To(Equal("Sound City"))
			Expect(client.MusicCredit.Query().Where(
				musiccredit.HasAlbumWith(album.IDEQ(rg2.ID)),
			).CountX(ctx)).To(Equal(3))
			Expect(client.MusicCredit.Query().Where(
				musiccredit.KindEQ(musiccredit.KindWriter),
			).CountX(ctx)).To(Equal(1))
			Expect(client.MusicCredit.Query().Where(
				musiccredit.KindEQ(musiccredit.KindFeaturing),
			).CountX(ctx)).To(BeZero(), "featuring comes from the light call too")
		})

		It("leaves credits_fetched_at empty after a light call", func() {
			light := HydrationParams{
				Tracks: []TrackSeed{{MBID: "r-1", Title: "T", Disc: 1, Position: 1}},
			}
			Expect(store.SetAlbumHydration(ctx, rg2.ID, light, now)).To(Succeed())
			got := client.Album.GetX(ctx, rg2.ID)
			Expect(got.MetadataFetchedAt).NotTo(BeNil())
			Expect(got.CreditsFetchedAt).To(BeNil())
		})

		It("stamps an album that came back with no tracks", func() {
			Expect(store.SetAlbumHydration(ctx, rg2.ID, HydrationParams{}, now)).
				To(Succeed())
			Expect(client.Album.GetX(ctx, rg2.ID).MetadataFetchedAt).NotTo(BeNil())
		})

		It("never touches status or monitored", func() {
			client.Album.UpdateOneID(rg2.ID).SetMonitored(false).
				SetStatus(album.StatusPaused).ExecX(ctx)
			Expect(store.SetAlbumHydration(ctx, rg2.ID, heavy(), now)).To(Succeed())
			got := client.Album.GetX(ctx, rg2.ID)
			Expect(got.Monitored).To(BeFalse())
			Expect(got.Status).To(Equal(album.StatusPaused))
		})
	})

	Describe("the artist list", func() {
		BeforeEach(func() {
			a := seed()
			Expect(a).NotTo(BeNil())
			_, err := store.CreateArtist(ctx, CreateArtistParams{
				MBID: "a-2", Name: "Björk", SortName: "Bjork", Genre: "Electronic",
				Monitor: entartist.MonitorNone,
				Albums: []AlbumSeed{
					{MBID: "b-1", Title: "Homogénic", ReleaseDate: day(-400)},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = store.CreateArtist(ctx, CreateArtistParams{
				MBID: "a-3", Name: "Zebra", SortName: "Zebra",
				Albums: []AlbumSeed{
					{
						MBID:        "z-1",
						Title:       "Coming",
						ReleaseDate: day(60),
						Monitored:   true,
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			client.Album.Update().Where(album.MbidEQ("rg-1")).
				SetStatus(album.StatusAvailable).ExecX(ctx)
			client.Album.Update().Where(album.MbidEQ("rg-2")).
				SetStatus(album.StatusDownloading).ExecX(ctx)
		})

		names := func(p ListArtistsParams) []string {
			GinkgoHelper()
			p.Now = now
			if p.Limit == 0 {
				p.Limit = 50
			}
			rows, err := store.ListArtists(ctx, p)
			Expect(err).NotTo(HaveOccurred())
			out := make([]string, len(rows))
			for i, r := range rows {
				out[i] = r.Name
			}
			return out
		}

		It("sorts by name on sort_name then name, and flips with order", func() {
			Expect(names(ListArtistsParams{Sort: "name"})).
				To(Equal([]string{"Björk", "Nirvana", "Zebra"}))
			Expect(names(ListArtistsParams{Sort: "name", Order: "desc"})).
				To(Equal([]string{"Zebra", "Nirvana", "Björk"}))
		})

		It("sorts by recently added by default, newest first", func() {
			Expect(names(ListArtistsParams{})).
				To(Equal([]string{"Zebra", "Björk", "Nirvana"}))
			Expect(names(ListArtistsParams{Sort: "recent", Order: "asc"})).
				To(Equal([]string{"Nirvana", "Björk", "Zebra"}))
		})

		It("pages", func() {
			Expect(names(ListArtistsParams{Sort: "name", Limit: 1, Offset: 1})).
				To(Equal([]string{"Nirvana"}))
		})

		It(
			"filters on the status rollup, upcoming albums not counting as wanted",
			func() {
				Expect(
					names(ListArtistsParams{Status: "downloading", Sort: "name"}),
				).
					To(Equal([]string{"Nirvana"}))
				Expect(names(ListArtistsParams{Status: "wanted", Sort: "name"})).
					To(BeEmpty())
				Expect(names(ListArtistsParams{Status: "available", Sort: "name"})).
					To(Equal([]string{"Björk", "Zebra"}))
			},
		)

		It("counts a monitored, released, wanted album as wanted", func() {
			client.Album.Update().Where(album.MbidEQ("rg-2")).
				SetStatus(album.StatusWanted).ExecX(ctx)
			Expect(
				names(ListArtistsParams{Status: "wanted"}),
			).To(Equal([]string{"Nirvana"}))
		})

		It("filters on monitoring: not none is monitored", func() {
			Expect(names(ListArtistsParams{Monitored: "unmonitored"})).
				To(Equal([]string{"Björk"}))
			Expect(names(ListArtistsParams{Monitored: "monitored", Sort: "name"})).
				To(Equal([]string{"Nirvana", "Zebra"}))
		})

		It(
			"searches name, sort name, genre and album titles, folding accents",
			func() {
				Expect(
					names(ListArtistsParams{Query: "bjork"}),
				).To(Equal([]string{"Björk"}))
				Expect(
					names(ListArtistsParams{Query: "electronic"}),
				).To(Equal([]string{"Björk"}))
				Expect(
					names(ListArtistsParams{Query: "homogenic"}),
				).To(Equal([]string{"Björk"}))
				Expect(
					names(ListArtistsParams{Query: "nevermind"}),
				).To(Equal([]string{"Nirvana"}))
				Expect(names(ListArtistsParams{Query: "nothing here"})).To(BeEmpty())
			},
		)

		It("counts what the filters match", func() {
			n, err := store.CountArtistsFiltered(ctx,
				ListArtistsParams{Monitored: "monitored", Now: now})
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(2))
		})

		It("loads each artist's albums as tiles", func() {
			rows, err := store.ListArtists(ctx,
				ListArtistsParams{Query: "nirvana", Limit: 10, Now: now})
			Expect(err).NotTo(HaveOccurred())
			Expect(rows[0].Edges.Albums).To(HaveLen(2))
			Expect(rows[0].Edges.Albums[0].Edges.Tracks).To(BeEmpty())
		})

		Describe("ArtistCounts", func() {
			It("tallies each facet against the other facet's filter", func() {
				c, err := store.ArtistCounts(ctx, ListArtistsParams{Now: now})
				Expect(err).NotTo(HaveOccurred())
				Expect(c).To(Equal(ArtistCounts{
					Total: 3, StatusTotal: 3,
					Wanted: 0, Downloading: 1, Available: 2,
					MonitoredTotal: 3, Monitored: 2, Unmonitored: 1,
					Albums: 4,
				}))
			})

			It("leaves its own facet out and applies the other", func() {
				c, err := store.ArtistCounts(
					ctx,
					ListArtistsParams{
						Monitored: "monitored",
						Status:    "available",
						Now:       now,
					},
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(c.Total).To(Equal(uint32(3)))
				Expect(c.StatusTotal).To(Equal(uint32(2)))
				Expect(c.Downloading).To(Equal(uint32(1)))
				Expect(c.Available).To(Equal(uint32(1)))
				Expect(c.MonitoredTotal).To(Equal(uint32(2)))
				Expect(c.Monitored).To(Equal(uint32(1)))
				Expect(c.Unmonitored).To(Equal(uint32(1)))
			})

			It("narrows every tally by the query", func() {
				c, err := store.ArtistCounts(ctx,
					ListArtistsParams{Query: "bjork", Now: now})
				Expect(err).NotTo(HaveOccurred())
				Expect(c.Total).To(Equal(uint32(3)))
				Expect(c.StatusTotal).To(Equal(uint32(1)))
				Expect(c.Unmonitored).To(Equal(uint32(1)))
				Expect(c.Monitored).To(BeZero())
			})
		})
	})

	Describe("AlbumRollups", func() {
		It(
			"tallies tracks, tracks with a file, bytes and runtime per album",
			func() {
				a := seed()
				rg1 := albumByMBID("rg-1")
				Expect(store.SetAlbumHydration(ctx, rg1.ID, HydrationParams{
					Tracks: []TrackSeed{
						{
							MBID:     "t-1",
							Title:    "A",
							Disc:     1,
							Position: 1,
							Duration: 100,
						},
						{
							MBID:     "t-2",
							Title:    "B",
							Disc:     1,
							Position: 2,
							Duration: 50,
						},
					},
				}, now)).To(Succeed())
				one := client.Track.Query().Where(track.MbidEQ("t-1")).OnlyX(ctx)
				client.MediaFile.Create().SetPath("/m/1.flac").SetSize(1000).
					SetTrackID(one.ID).ExecX(ctx)
				client.MediaFile.Create().SetPath("/m/1.mp3").SetSize(500).
					SetTrackID(one.ID).ExecX(ctx)

				got, err := store.AlbumRollups(ctx, []uint32{a.ID})
				Expect(err).NotTo(HaveOccurred())
				Expect(got[rg1.ID]).To(Equal(AlbumRollup{
					TrackCount: 2, TracksHave: 1, Size: 1500, Duration: 150,
				}))
				Expect(
					got,
				).NotTo(HaveKey(albumByMBID("rg-2").ID), "no tracks, no row")
			},
		)

		It("is empty for no artists", func() {
			got, err := store.AlbumRollups(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})
	})

	Describe("HydratingArtists", func() {
		It("flags an artist with an album still waiting for its tracks", func() {
			a := seed()
			got, err := store.HydratingArtists(ctx, []uint32{a.ID}, now)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(map[uint32]bool{a.ID: true}))
		})

		It("flags tracks whose credits are missing, only for a while", func() {
			a := seed()
			client.Album.Update().Where(album.MbidEQ("rg-2")).
				SetMetadataFetchedAt(now).ExecX(ctx)
			client.Track.Create().SetTitle("T").SetPosition(1).
				SetAlbumID(albumByMBID("rg-2").ID).ExecX(ctx)
			client.Album.Update().Where(album.MbidEQ("rg-1")).
				SetCreditsFetchedAt(now).ExecX(ctx)

			got, err := store.HydratingArtists(ctx, []uint32{a.ID}, now)
			Expect(err).NotTo(HaveOccurred())
			Expect(got[a.ID]).To(BeTrue())

			got, err = store.HydratingArtists(
				ctx,
				[]uint32{a.ID},
				now.Add(2*time.Hour),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(got[a.ID]).To(BeFalse())
		})

		It("does not flag an artist whose albums are all done", func() {
			a := seed()
			client.Album.Update().SetMetadataFetchedAt(now).
				SetCreditsFetchedAt(now).ExecX(ctx)
			got, err := store.HydratingArtists(ctx, []uint32{a.ID}, now)
			Expect(err).NotTo(HaveOccurred())
			Expect(got[a.ID]).To(BeFalse())
		})
	})

	Describe("the hydration sweep lists", func() {
		It("lists albums awaiting tracks, monitored and newest first", func() {
			seed()
			a2, err := store.CreateArtist(ctx, CreateArtistParams{
				MBID: "a-2", Name: "Other",
				Albums: []AlbumSeed{
					{MBID: "o-1", Title: "Quiet", ReleaseDate: day(-5)},
					{
						MBID:        "o-2",
						Title:       "Loud",
						ReleaseDate: day(-1),
						Monitored:   true,
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(a2).NotTo(BeNil())
			rows, err := store.ListAlbumsAwaitingHydration(ctx, 10)
			Expect(err).NotTo(HaveOccurred())
			mbids := make([]string, len(rows))
			for i, r := range rows {
				mbids[i] = r.Mbid
			}
			Expect(mbids).To(Equal([]string{"o-2", "rg-2", "o-1"}))
			Expect(rows[0].Edges.Artist).NotTo(BeNil())

			limited, err := store.ListAlbumsAwaitingHydration(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(limited).To(HaveLen(1))
		})

		It(
			"lists hydrated albums with tracks and no credits, small ones only",
			func() {
				seed()
				client.Album.Update().Where(album.MbidEQ("rg-2")).
					SetMetadataFetchedAt(now).ExecX(ctx)
				big := albumByMBID("rg-2")
				creates := make([]*ent.TrackCreate, MaxCreditsTracks+1)
				for i := range creates {
					creates[i] = client.Track.Create().SetTitle(fmt.Sprint(i)).
						SetPosition(uint16(i + 1)).SetAlbumID(big.ID)
				}
				client.Track.CreateBulk(creates...).ExecX(ctx)

				rows, err := store.ListAlbumsAwaitingCredits(ctx, 10)
				Expect(err).NotTo(HaveOccurred())
				Expect(rows).To(HaveLen(1))
				Expect(rows[0].Mbid).To(Equal("rg-1"))
			},
		)

		It("lists stale, trackless albums that are out or near release", func() {
			seed()
			old := now.Add(-10 * 24 * time.Hour)
			client.Album.Update().Where(album.MbidEQ("rg-2")).
				SetMetadataFetchedAt(old).ExecX(ctx)
			rows, err := store.ListAlbumsTrackless(ctx,
				now.Add(-7*24*time.Hour), now.Add(90*24*time.Hour), 5)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1))
			Expect(rows[0].Mbid).To(Equal("rg-2"))

			client.Album.Update().Where(album.MbidEQ("rg-2")).
				SetReleaseDate(*day(200)).ExecX(ctx)
			rows, err = store.ListAlbumsTrackless(ctx,
				now.Add(-7*24*time.Hour), now.Add(90*24*time.Hour), 5)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(BeEmpty())
		})
	})

	Describe("backlog search", func() {
		var (
			a                               *ent.Artist
			cutoff                          time.Time
			eligible, capped, cooled, inFly *ent.Album
			unmon, hasFile                  *ent.Album
			unreleased, unhydrated, inPack  *ent.Album
		)

		BeforeEach(func() {
			cutoff = time.Now().Add(-time.Hour)
			var err error
			a, err = store.CreateArtist(ctx, CreateArtistParams{
				MBID: "a-2", Name: "Pixies",
				Albums: []AlbumSeed{
					{MBID: "e", Title: "Eligible", Monitored: true},
					{MBID: "c", Title: "Capped", Monitored: true},
					{MBID: "k", Title: "Cooled", Monitored: true},
					{MBID: "f", Title: "InFlight", Monitored: true},
					{MBID: "u", Title: "Unmonitored", Monitored: true},
					{MBID: "h", Title: "Available", Monitored: true},
					{
						MBID:        "future",
						Title:       "Unreleased",
						ReleaseDate: day(30),
						Monitored:   true,
					},
					{MBID: "stub", Title: "Unhydrated", Monitored: true},
					{MBID: "p", Title: "InPack", Monitored: true},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			client.Album.Update().Where(album.MbidNEQ("stub")).
				SetMetadataFetchedAt(now).ExecX(ctx)
			byMBID := func(m string) *ent.Album { return albumByMBID(m) }
			eligible, capped, cooled = byMBID("e"), byMBID("c"), byMBID("k")
			inFly, unmon, hasFile = byMBID("f"), byMBID("u"), byMBID("h")
			unreleased, unhydrated, inPack = byMBID(
				"future",
			), byMBID(
				"stub",
			), byMBID(
				"p",
			)
			client.Album.UpdateOneID(capped.ID).SetGrabFailures(3).ExecX(ctx)
			client.Album.UpdateOneID(cooled.ID).
				SetLastSearchAt(time.Now()).
				ExecX(ctx)
			client.Album.UpdateOneID(unmon.ID).SetMonitored(false).ExecX(ctx)
			client.Album.UpdateOneID(hasFile.ID).
				SetStatus(album.StatusAvailable).ExecX(ctx)
			client.DownloadRecord.Create().SetTitle("x").
				SetStatus("downloading").SetAlbumID(inFly.ID).SaveX(ctx)
			client.DownloadRecord.Create().SetTitle("pack").
				SetStatus("downloading").SetArtistID(a.ID).AddAlbumIDs(inPack.ID).
				SaveX(ctx)
		})

		ids := func(rows []*ent.Album) []uint32 {
			out := make([]uint32, len(rows))
			for i, r := range rows {
				out[i] = r.ID
			}
			return out
		}

		It(
			"lists only wanted, monitored, released, hydrated, under-cap, out-of-cooldown albums with no in-flight record",
			func() {
				rows, err := store.ListEligibleAlbumsForSync(ctx, 3, cutoff)
				Expect(err).NotTo(HaveOccurred())
				Expect(ids(rows)).To(Equal([]uint32{eligible.ID}))
				Expect(rows[0].Edges.Artist).NotTo(BeNil())
				Expect(
					ids(rows),
				).NotTo(ContainElements(unreleased.ID, unhydrated.ID, inPack.ID))
			},
		)

		It("orders never-searched first, then oldest search, then id", func() {
			old := client.Album.UpdateOneID(cooled.ID).
				SetLastSearchAt(time.Now().Add(-5 * time.Hour))
			old.ExecX(ctx)
			client.Album.UpdateOneID(eligible.ID).
				SetLastSearchAt(time.Now().Add(-3 * time.Hour)).ExecX(ctx)
			client.Album.UpdateOneID(capped.ID).SetGrabFailures(0).ExecX(ctx)

			rows, err := store.ListEligibleAlbumsForSync(ctx, 3, cutoff)
			Expect(err).NotTo(HaveOccurred())
			Expect(ids(rows)).To(Equal([]uint32{capped.ID, cooled.ID, eligible.ID}))
		})

		It("counts a finished record as no longer in flight", func() {
			client.DownloadRecord.Update().
				Where(downloadrecord.HasAlbumWith(album.IDEQ(inFly.ID))).
				SetStatus("completed").ExecX(ctx)
			client.DownloadRecord.Update().
				Where(downloadrecord.HasArtistWith(entartist.IDEQ(a.ID))).
				SetStatus("completed").ExecX(ctx)
			rows, err := store.ListEligibleAlbumsForSync(ctx, 3, cutoff)
			Expect(err).NotTo(HaveOccurred())
			Expect(ids(rows)).To(ConsistOf(eligible.ID, inFly.ID, inPack.ID))
		})

		It("stamps the search time and moves the failure counter", func() {
			when := time.Now().Truncate(time.Second)
			Expect(store.SetAlbumLastSearchAt(ctx, eligible.ID, when)).To(Succeed())
			Expect(client.Album.GetX(ctx, eligible.ID).LastSearchAt).
				To(HaveValue(BeTemporally("~", when, time.Second)))

			Expect(store.IncrementAlbumGrabFailures(ctx, eligible.ID)).To(Succeed())
			Expect(store.IncrementAlbumGrabFailures(ctx, eligible.ID)).To(Succeed())
			Expect(client.Album.GetX(ctx, eligible.ID).GrabFailures).
				To(Equal(uint8(2)))
			Expect(store.ResetAlbumGrabFailures(ctx, eligible.ID)).To(Succeed())
			Expect(client.Album.GetX(ctx, eligible.ID).GrabFailures).
				To(BeZero())
		})

		It("lists the artist's albums a search-now pass may work", func() {
			rows, err := store.ListArtistAlbumsForSearch(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(ids(rows)).To(ConsistOf(
				eligible.ID, capped.ID, cooled.ID, inFly.ID, inPack.ID,
			))
		})

		It(
			"lists the albums a discography pack may cover: wanted or paused",
			func() {
				client.Album.UpdateOneID(cooled.ID).
					SetStatus(album.StatusPaused).
					ExecX(ctx)
				rows, err := store.ListPackAlbums(ctx, a.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(ids(rows)).To(ConsistOf(
					eligible.ID, capped.ID, cooled.ID, inFly.ID, inPack.ID,
				))
				Expect(ids(rows)).NotTo(ContainElements(
					hasFile.ID, unmon.ID, unreleased.ID, unhydrated.ID,
				))
			},
		)

		It(
			"reports whether a download is in flight, single or as part of a pack",
			func() {
				live, err := store.AlbumHasLiveRecord(ctx, inFly.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(live).To(BeTrue())
				live, err = store.AlbumHasLiveRecord(ctx, inPack.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(live).To(BeTrue())
				live, err = store.AlbumHasLiveRecord(ctx, eligible.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(live).To(BeFalse())
			},
		)

		It("lists stale artists oldest first within the limit", func() {
			fresh, err := store.CreateArtist(ctx, CreateArtistParams{
				MBID: "a-3", Name: "Fresh",
			})
			Expect(err).NotTo(HaveOccurred())
			client.Artist.UpdateOneID(fresh.ID).
				SetLastRefreshedAt(time.Now()).
				ExecX(ctx)
			older, err := store.CreateArtist(ctx, CreateArtistParams{
				MBID: "a-4", Name: "Older",
			})
			Expect(err).NotTo(HaveOccurred())
			client.Artist.UpdateOneID(older.ID).
				SetLastRefreshedAt(time.Now().Add(-48 * time.Hour)).ExecX(ctx)

			rows, err := store.ListArtistsStaleSince(ctx, cutoff, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(2))
			Expect(rows[0].ID).To(Equal(a.ID))
			Expect(rows[1].ID).To(Equal(older.ID))

			rows, err = store.ListArtistsStaleSince(ctx, cutoff, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1))
		})
	})

	Describe("ListWantedAlbums", func() {
		It(
			"returns only monitored, released, hydrated wanted albums under the cap with no live record",
			func() {
				a := seed()
				client.Album.Update().SetMetadataFetchedAt(now).ExecX(ctx)
				nevermind, inUtero := albumByMBID("rg-1"), albumByMBID("rg-2")
				bleach := client.Album.Create().
					SetMbid("rg-3").SetTitle("Bleach").SetArtistID(a.ID).
					SetMetadataFetchedAt(now).SaveX(ctx)
				client.Album.Create().
					SetMbid("rg-4").SetTitle("Incesticide").SetArtistID(a.ID).
					SetMetadataFetchedAt(now).SetGrabFailures(3).ExecX(ctx)
				client.Album.Create().
					SetMbid("rg-5").SetTitle("Hormoaning").SetArtistID(a.ID).
					SetMetadataFetchedAt(now).SetMonitored(false).ExecX(ctx)
				client.Album.Create().
					SetMbid("rg-6").SetTitle("Not out").SetArtistID(a.ID).
					SetMetadataFetchedAt(now).SetReleaseDate(*day(30)).ExecX(ctx)
				client.Album.Create().
					SetMbid("rg-7").SetTitle("Stub").SetArtistID(a.ID).ExecX(ctx)
				client.DownloadRecord.Create().
					SetTitle("Nirvana - In Utero").
					SetStatus(downloadrecord.StatusDownloading).
					SetAlbumID(inUtero.ID).ExecX(ctx)
				client.Album.UpdateOneID(bleach.ID).
					SetStatus(album.StatusAvailable).ExecX(ctx)

				got, err := store.ListWantedAlbums(ctx, 3)
				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(HaveLen(1))
				Expect(got[0].ID).To(Equal(nevermind.ID))
				Expect(got[0].Edges.Artist).NotTo(BeNil())
				Expect(got[0].Edges.Artist.Name).To(Equal("Nirvana"))
			},
		)
	})

	Describe("CompletePackRecord", func() {
		It("completes the record and returns unmatched albums to wanted", func() {
			a := seed()
			rec := client.DownloadRecord.Create().SetTitle("pack").
				SetStatus(downloadrecord.StatusImporting).SetArtistID(a.ID).
				AddAlbumIDs(albumByMBID("rg-1").ID, albumByMBID("rg-2").ID).
				SaveX(ctx)
			client.Album.Update().SetStatus(album.StatusDownloading).ExecX(ctx)
			client.Album.Update().Where(album.MbidEQ("rg-1")).
				SetStatus(album.StatusAvailable).ExecX(ctx)

			Expect(store.CompletePackRecord(
				ctx,
				rec.ID,
				[]uint32{
					albumByMBID("rg-1").ID,
					albumByMBID("rg-2").ID,
				},
			)).To(Succeed())

			got := client.DownloadRecord.GetX(ctx, rec.ID)
			Expect(got.Status).To(Equal(downloadrecord.StatusCompleted))
			Expect(got.ImportedAt).NotTo(BeNil())
			Expect(albumByMBID("rg-2").Status).To(Equal(album.StatusWanted))
			Expect(albumByMBID("rg-1").Status).To(Equal(album.StatusAvailable),
				"only an album still downloading goes back")
		})
	})

	It("cascades the delete to albums, tracks, members and credits", func() {
		a := seed()
		Expect(store.SetAlbumHydration(ctx, albumByMBID("rg-1").ID, HydrationParams{
			CreditsComplete: true,
			Credits: []CreditSeed{
				{Name: "P", Role: "mix"},
			},
			Tracks: []TrackSeed{{
				Title: "T", Disc: 1, Position: 1,
				Featuring: []PersonSeed{{Name: "F"}},
			}},
		}, now)).To(Succeed())
		Expect(store.DeleteArtist(ctx, a.ID)).To(Succeed())
		Expect(client.Album.Query().Count(ctx)).To(BeZero())
		Expect(client.Track.Query().Count(ctx)).To(BeZero())
		Expect(client.ArtistMember.Query().Count(ctx)).To(BeZero())
		Expect(client.MusicCredit.Query().Count(ctx)).To(BeZero())
	})
})
