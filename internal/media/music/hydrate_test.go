package music

import (
	"context"
	"errors"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/musiccredit"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("hydrator queue", Label("unit", "music"), func() {
	var h *hydrator

	BeforeEach(func() {
		h = &hydrator{}
	})

	item := func(id uint32, monitored bool, days int) hydrateItem {
		d := time.Now().Add(time.Duration(days) * 24 * time.Hour)
		return hydrateItem{albumID: id, monitored: monitored, date: &d}
	}

	It("asks for the worker once and not again while it runs", func() {
		Expect(h.add(1, []hydrateItem{item(1, true, 0)})).To(BeTrue())
		Expect(h.add(1, []hydrateItem{item(2, true, 0)})).To(BeFalse())
	})

	It("queues an album only once", func() {
		h.add(1, []hydrateItem{item(1, true, 0)})
		h.add(1, []hydrateItem{item(1, true, 0)})
		first, ok := h.pop()
		Expect(ok).To(BeTrue())
		Expect(first.albumID).To(Equal(uint32(1)))
		_, ok = h.pop()
		Expect(ok).To(BeFalse())
	})

	It("serves monitored albums first and newest first within an artist", func() {
		h.add(1, []hydrateItem{
			item(1, false, -1), item(2, true, -30), item(3, true, -10),
		})
		var got []uint32
		for {
			it, ok := h.pop()
			if !ok {
				break
			}
			got = append(got, it.albumID)
		}
		Expect(got).To(Equal([]uint32{3, 2, 1}))
	})

	It("takes one album from each artist in turn", func() {
		h.add(
			1,
			[]hydrateItem{
				item(11, true, -1),
				item(12, true, -2),
				item(13, true, -3),
			},
		)
		h.add(2, []hydrateItem{item(21, true, -1), item(22, true, -2)})
		var got []uint32
		for {
			it, ok := h.pop()
			if !ok {
				break
			}
			got = append(got, it.albumID)
		}
		Expect(got).To(Equal([]uint32{11, 21, 12, 22, 13}))
	})

	It("stops the worker under the lock when nothing is left", func() {
		h.add(1, []hydrateItem{item(1, true, 0)})
		_, ok := h.pop()
		Expect(ok).To(BeTrue())
		_, ok = h.pop()
		Expect(ok).To(BeFalse())
		h.done(1)
		Expect(h.add(1, []hydrateItem{item(2, true, 0)})).To(BeTrue(),
			"a later add has to start the worker again")
	})

	It("drops a queued album when someone hydrates it themselves", func() {
		h.add(1, []hydrateItem{item(1, true, 0), item(2, true, -1)})
		h.remove(1)
		it, ok := h.pop()
		Expect(ok).To(BeTrue())
		Expect(it.albumID).To(Equal(uint32(2)))
	})

	It("forgets everything on reset, so a dead worker blocks nobody", func() {
		h.add(1, []hydrateItem{item(1, true, 0)})
		h.reset()
		Expect(h.add(1, []hydrateItem{item(1, true, 0)})).To(BeTrue())
	})
})

var _ = Describe("Album hydration", Label("unit", "integration", "music"), func() {
	var e *env

	BeforeEach(func() {
		e = newEnv(false)
		e.posters.EXPECT().
			Fetch(mock.Anything, "albums", mock.Anything, mock.Anything).
			Return(nil).
			Maybe()
	})

	Describe("HydrateAlbum", func() {
		It("fetches the album's release once and stores it", func() {
			_, albums := e.seedArtist("Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind", monitored: true})
			d := hydrated("rg-1", "Drain You", "Lithium")
			d.Country, d.CatalogNumber, d.Media = "US", "DGCD-24425", []string{
				"cd",
				"vinyl",
			}
			d.Tracks[0].Featuring = []metadata.PersonInfo{
				{Name: "Guest", MBID: "g-1"},
			}
			d.Tracks[1].Bonus = true
			e.provider.EXPECT().
				GetReleaseGroup(mock.Anything, "rg-1").
				Return(d, nil).
				Once()

			Expect(e.svc.HydrateAlbum(e.ctx, albums[0].ID)).To(Succeed())
			Expect(e.svc.HydrateAlbum(e.ctx, albums[0].ID)).To(Succeed())

			got := e.client.Album.GetX(e.ctx, albums[0].ID)
			Expect(got.MetadataFetchedAt).NotTo(BeNil())
			Expect(got.CreditsFetchedAt).NotTo(BeNil())
			Expect(got.Label).To(Equal("DGC"))
			Expect(got.Country).To(Equal("US"))
			Expect(got.CatalogNumber).To(Equal("DGCD-24425"))
			Expect(got.Media).To(Equal("cd,vinyl"))
			Expect(got.Barcode).To(Equal("0720642442524"))
			tracks := got.QueryTracks().AllX(e.ctx)
			Expect(tracks).To(HaveLen(2))
			Expect(tracks[1].Bonus).To(BeTrue())
			Expect(e.client.MusicCredit.Query().
				Where(musiccredit.KindEQ(musiccredit.KindFeaturing)).
				CountX(e.ctx)).To(Equal(1))
		})

		It("leaves the credits unfetched after the light fallback", func() {
			_, albums := e.seedArtist(
				"Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind"},
			)
			d := hydrated("rg-1", "A")
			d.CreditsComplete = false
			e.provider.EXPECT().
				GetReleaseGroup(mock.Anything, "rg-1").
				Return(d, nil).
				Once()

			Expect(e.svc.HydrateAlbum(e.ctx, albums[0].ID)).To(Succeed())
			got := e.client.Album.GetX(e.ctx, albums[0].ID)
			Expect(got.MetadataFetchedAt).NotTo(BeNil())
			Expect(got.CreditsFetchedAt).To(BeNil())
		})

		It("leaves the album unhydrated and reports the failure", func() {
			_, albums := e.seedArtist(
				"Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind"},
			)
			boom := errors.New("musicbrainz down")
			e.provider.EXPECT().
				GetReleaseGroup(mock.Anything, "rg-1").
				Return(nil, boom).
				Once()

			Expect(e.svc.HydrateAlbum(e.ctx, albums[0].ID)).To(MatchError(boom))
			Expect(
				e.client.Album.GetX(e.ctx, albums[0].ID).MetadataFetchedAt,
			).To(BeNil())
		})

		It("stamps an album MusicBrainz no longer has", func() {
			_, albums := e.seedArtist(
				"Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind"},
			)
			e.provider.EXPECT().GetReleaseGroup(mock.Anything, "rg-1").
				Return(nil, metadata.ErrNotFound).Once()

			Expect(e.svc.HydrateAlbum(e.ctx, albums[0].ID)).To(Succeed())
			Expect(
				e.client.Album.GetX(e.ctx, albums[0].ID).MetadataFetchedAt,
			).NotTo(BeNil())
		})

		It("ignores an album that is gone", func() {
			Expect(e.svc.HydrateAlbum(e.ctx, 999)).To(Succeed())
		})

		It("marks as guests only the performers nobody expects", func() {
			a, albums := e.seedArtist(
				"Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind"},
			)
			e.client.ArtistMember.Create().SetName("Dave").SetMbid("m-dave").
				SetOrdinal(0).SetArtistID(a.ID).ExecX(e.ctx)
			d := hydrated("rg-1", "A", "B", "C", "D")
			d.PerformerRecordings = 4
			person := func(name, mbid string, n int) metadata.PerformerInfo {
				return metadata.PerformerInfo{
					Name: name, MBID: mbid,
					Instruments: []string{"guitar"}, Recordings: n,
				}
			}
			d.Performers = []metadata.PerformerInfo{
				person("Nirvana", "mbid-Nirvana", 1),
				person("Dave", "m-dave", 1),
				person("Regular", "p-reg", 3),
				person("Guest", "p-guest", 1),
				person("Half", "p-half", 2),
			}
			e.provider.EXPECT().
				GetReleaseGroup(mock.Anything, "rg-1").
				Return(d, nil).
				Once()

			Expect(e.svc.HydrateAlbum(e.ctx, albums[0].ID)).To(Succeed())
			guests := map[string]bool{}
			for _, c := range e.client.MusicCredit.Query().
				Where(musiccredit.KindEQ(musiccredit.KindPerformer)).AllX(e.ctx) {
				guests[c.Name] = c.Guest
			}
			Expect(guests).To(Equal(map[string]bool{
				"Nirvana": false, "Dave": false, "Regular": false,
				"Guest": true, "Half": true,
			}))
		})

		It("takes the album out of the background queue first", func() {
			a, albums := e.seedArtist(
				"Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind"},
			)
			e.svc.hydrate.add(a.ID, []hydrateItem{{albumID: albums[0].ID}})
			e.provider.EXPECT().GetReleaseGroup(mock.Anything, "rg-1").
				Return(hydrated("rg-1", "A"), nil).Once()

			Expect(e.svc.HydrateAlbum(e.ctx, albums[0].ID)).To(Succeed())
			_, ok := e.svc.hydrate.pop()
			Expect(ok).To(BeFalse())
		})
	})

	Describe("the worker", func() {
		bg := func() context.Context { return metadata.Background(e.ctx) }

		It("hydrates one album from each artist in turn", func() {
			a1, albums1 := e.seedArtist("Alpha",
				albumSeed{mbid: "a1", title: "A1", monitored: true},
				albumSeed{mbid: "a2", title: "A2", monitored: true})
			a2, albums2 := e.seedArtist("Beta",
				albumSeed{mbid: "b1", title: "B1", monitored: true},
				albumSeed{mbid: "b2", title: "B2", monitored: true})
			var (
				mu    sync.Mutex
				order []string
			)
			e.provider.EXPECT().GetReleaseGroup(mock.Anything, mock.Anything).
				RunAndReturn(func(_ context.Context, mbid string) (*metadata.ReleaseGroupDetails, error) {
					mu.Lock()
					defer mu.Unlock()
					order = append(order, mbid)
					return hydrated(mbid, "T"), nil
				}).
				Times(4)
			e.svc.hydrate.add(a1.ID, []hydrateItem{
				{
					albumID:   albums1[0].ID,
					monitored: true,
				},
				{albumID: albums1[1].ID, monitored: true},
			})
			e.svc.hydrate.add(a2.ID, []hydrateItem{
				{
					albumID:   albums2[0].ID,
					monitored: true,
				},
				{albumID: albums2[1].ID, monitored: true},
			})

			e.svc.hydrationWorker(bg())

			Expect(order).To(Equal([]string{"a1", "b1", "a2", "b2"}))
			Expect(e.idle()).To(BeTrue())
		})

		It("finds the albums a previous run left behind", func() {
			_, albums := e.seedArtist("Alpha", albumSeed{mbid: "a1", title: "A1"})
			e.provider.EXPECT().GetReleaseGroup(mock.Anything, "a1").
				Return(hydrated("a1", "T"), nil).Once()

			e.svc.hydrationWorker(bg())

			Expect(
				e.client.Album.GetX(e.ctx, albums[0].ID).MetadataFetchedAt,
			).NotTo(BeNil())
		})

		It("does not retry a failed album within the run", func() {
			_, albums := e.seedArtist("Alpha", albumSeed{mbid: "a1", title: "A1"})
			e.provider.EXPECT().GetReleaseGroup(mock.Anything, "a1").
				Return(nil, errors.New("503")).Once()

			e.svc.hydrationWorker(bg())

			Expect(
				e.client.Album.GetX(e.ctx, albums[0].ID).MetadataFetchedAt,
			).To(BeNil())
			Expect(e.idle()).To(BeTrue())
		})

		It("stands down for the Retry-After and tries the album again", func() {
			a, albums := e.seedArtist("Alpha", albumSeed{mbid: "a1", title: "A1"})
			e.provider.EXPECT().GetReleaseGroup(mock.Anything, "a1").
				Return(nil, &metadata.RateLimitedError{RetryAfter: 20 * time.Millisecond}).
				Once()
			e.provider.EXPECT().GetReleaseGroup(mock.Anything, "a1").
				Return(hydrated("a1", "T"), nil).Once()
			e.svc.hydrate.add(a.ID, []hydrateItem{{albumID: albums[0].ID}})

			e.svc.hydrationWorker(bg())

			Expect(
				e.client.Album.GetX(e.ctx, albums[0].ID).MetadataFetchedAt,
			).NotTo(BeNil())
		})

		It("resolves the album's cover once its tracks are in", func() {
			a, albums := e.seedArtist("Alpha", albumSeed{mbid: "a1", title: "A1"})
			e.provider.EXPECT().GetReleaseGroup(mock.Anything, "a1").
				Return(hydrated("a1", "T"), nil).Once()
			e.svc.hydrate.add(a.ID, []hydrateItem{{albumID: albums[0].ID}})

			e.svc.hydrationWorker(bg())

			e.posters.AssertCalled(GinkgoT(), "Fetch", mock.Anything, "albums",
				albums[0].ID, metadata.CoverArtURL("a1"))
		})
	})

	Describe("the sweep", func() {
		It(
			"queues unfinished albums, credit-less ones and old trackless ones",
			func() {
				e.setConfig(nil)
				_, albums := e.seedArtist(
					"Alpha",
					albumSeed{mbid: "stub", title: "Stub", monitored: true},
					albumSeed{
						mbid:   "nocredits",
						title:  "NoCredits",
						tracks: []string{"A"},
					},
					albumSeed{mbid: "empty", title: "Empty"},
					albumSeed{mbid: "done", title: "Done", tracks: []string{"A"}},
				)
				e.client.Album.UpdateOneID(albums[1].ID).
					SetMetadataFetchedAt(time.Now().Add(-time.Hour)).ExecX(e.ctx)
				e.client.Album.UpdateOneID(albums[2].ID).
					SetMetadataFetchedAt(time.Now().Add(-10 * 24 * time.Hour)).
					ExecX(e.ctx)
				e.client.Album.UpdateOneID(albums[3].ID).
					SetMetadataFetchedAt(time.Now()).
					SetCreditsFetchedAt(time.Now()).ExecX(e.ctx)
				for _, m := range []string{"stub", "nocredits", "empty"} {
					e.provider.EXPECT().GetReleaseGroup(mock.Anything, m).
						Return(hydrated(m, "T"), nil).Once()
				}

				e.svc.sweepHydration(e.ctx)
				Eventually(e.idle).Should(BeTrue())

				Expect(
					e.client.Album.Query().
						Where(album.CreditsFetchedAtNotNil()).
						CountX(e.ctx),
				).
					To(Equal(4))
			},
		)
	})
})
