package music

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
)

var _ = Describe(
	"Reading the library",
	Label("unit", "integration", "music"),
	func() {
		var e *env

		BeforeEach(func() {
			e = newEnv(false)
		})

		queue := func(items ...download.QueueEntry) {
			e.dl.EXPECT().Queue(mock.Anything).
				Return(download.QueueSnapshot{Items: items}, nil).Maybe()
		}

		day := func(d int) *time.Time {
			t := time.Now().Add(time.Duration(d) * 24 * time.Hour)
			return &t
		}

		// seedLibrary adds Nirvana (a hydrated album with one file, a stub, an
		// upcoming album) and Bjork (one stub).
		seedLibrary := func() (*ent.Artist, []*ent.Album) {
			GinkgoHelper()
			a, albums := e.seedArtist(
				"Nirvana",
				albumSeed{
					mbid:      "rg-1",
					title:     "Nevermind",
					date:      day(-9000),
					monitored: true,
					tracks:    []string{"Drain You", "Lithium"},
				},
				albumSeed{
					mbid:      "rg-2",
					title:     "In Utero",
					date:      day(-8000),
					monitored: true,
				},
				albumSeed{
					mbid:      "rg-3",
					title:     "Coming",
					date:      day(60),
					monitored: true,
				},
			)
			e.addFile(albums[0].QueryTracks().FirstX(e.ctx), "/m/1.flac", "lossless")
			e.seedArtist(
				"Bjork",
				albumSeed{mbid: "b-1", title: "Homogenic", date: day(-100)},
			)
			return a, albums
		}

		Describe("List", func() {
			It("reports each artist's rollups and its albums newest first", func() {
				queue()
				_, albums := seedLibrary()
				e.client.Album.UpdateOneID(albums[0].ID).
					SetStatus(album.StatusAvailable).
					ExecX(e.ctx)

				page, err := e.svc.List(e.ctx, db.ListArtistsParams{Sort: "name"})
				Expect(err).NotTo(HaveOccurred())
				Expect(page.Total).To(Equal(uint32(2)))
				Expect(page.Items).To(HaveLen(2))

				nirvana := page.Items[1]
				Expect(nirvana.Artist.Name).To(Equal("Nirvana"))
				Expect(nirvana.AlbumCount).To(Equal(uint32(3)))
				Expect(nirvana.TracksHave).To(Equal(uint32(1)))
				Expect(nirvana.Size).To(Equal(int64(1000)))
				Expect(nirvana.Hydrating).To(BeTrue(), "In Utero has no tracks yet")
				Expect(
					nirvana.Status,
				).To(Equal("wanted"), "In Utero is out and missing")

				titles := make([]string, 0, len(nirvana.Albums))
				for _, al := range nirvana.Albums {
					titles = append(titles, al.Album.Title)
				}
				Expect(titles).To(Equal([]string{"Coming", "In Utero", "Nevermind"}))
				coming, utero, nevermind := nirvana.Albums[0], nirvana.Albums[1], nirvana.Albums[2]
				Expect(coming.Status).To(Equal("upcoming"))
				Expect(nevermind.TrackCount).To(Equal(uint32(2)))
				Expect(nevermind.TracksHave).To(Equal(uint32(1)))
				Expect(nevermind.TracksPending).To(BeFalse())
				Expect(utero.TracksPending).To(BeTrue())
				Expect(
					nirvana.Albums[0].Tracks,
				).To(BeEmpty(), "a tile carries no tracks")
			})

			It("serves the English overview on the list", func() {
				queue()
				a, _ := seedLibrary()
				e.client.Artist.UpdateOneID(a.ID).SetOverview("A band.").
					SetOverviewSource("https://en").
					SetOverviewFr("Un groupe.").ExecX(e.ctx)
				page, err := e.svc.List(
					e.ctx,
					db.ListArtistsParams{Query: "nirvana"},
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(page.Items[0].Overview).To(Equal("A band."))
				Expect(page.Items[0].OverviewSource).To(Equal("https://en"))
			})

			It("filters and totals what the filters match", func() {
				queue()
				seedLibrary()
				page, err := e.svc.List(
					e.ctx,
					db.ListArtistsParams{Query: "homogenic", Limit: 1},
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(page.Total).To(Equal(uint32(1)))
				Expect(page.Items[0].Artist.Name).To(Equal("Bjork"))
			})

			It("adds the live progress of a downloading album", func() {
				_, albums := seedLibrary()
				e.client.Album.UpdateOneID(albums[0].ID).
					SetStatus(album.StatusDownloading).
					ExecX(e.ctx)
				e.client.Album.UpdateOneID(albums[1].ID).
					SetStatus(album.StatusDownloading).
					ExecX(e.ctx)
				queue(
					download.QueueEntry{
						Status:   "downloading",
						Progress: 0.4,
						Album:    &ent.Album{ID: albums[0].ID},
					},
					download.QueueEntry{
						Status: "importing", Progress: 1,
						Artist: &ent.Artist{}, PackAlbumIDs: []uint32{albums[1].ID},
					},
				)

				page, err := e.svc.List(
					e.ctx,
					db.ListArtistsParams{Query: "nirvana"},
				)
				Expect(err).NotTo(HaveOccurred())
				byTitle := map[string]AlbumView{}
				for _, al := range page.Items[0].Albums {
					byTitle[al.Album.Title] = al
				}
				Expect(*byTitle["Nevermind"].Progress).To(BeNumerically("~", 40))
				Expect(*byTitle["In Utero"].Progress).To(BeNumerically("==", 100))
				Expect(byTitle["Coming"].Progress).To(BeNil())
			})

			It("reports no progress when the queue cannot be read", func() {
				_, albums := seedLibrary()
				e.client.Album.UpdateOneID(albums[0].ID).
					SetStatus(album.StatusDownloading).
					ExecX(e.ctx)
				e.dl.EXPECT().Queue(mock.Anything).
					Return(download.QueueSnapshot{}, errors.New("down")).Maybe()

				page, err := e.svc.List(
					e.ctx,
					db.ListArtistsParams{Query: "nirvana"},
				)
				Expect(err).NotTo(HaveOccurred())
				for _, al := range page.Items[0].Albums {
					Expect(al.Progress).To(BeNil())
				}
			})
		})

		Describe("Counts", func() {
			It("tallies the facets", func() {
				seedLibrary()
				c, err := e.svc.Counts(e.ctx, db.ListArtistsParams{})
				Expect(err).NotTo(HaveOccurred())
				Expect(c.Total).To(Equal(uint32(2)))
				Expect(c.Albums).To(Equal(uint32(4)))
				Expect(c.Wanted).To(Equal(uint32(1)))
			})
		})

		Describe("Detail", func() {
			It(
				"reads the full tree with the overview in the requested language",
				func() {
					queue()
					a, _ := seedLibrary()
					e.client.Artist.UpdateOneID(a.ID).
						SetOverview("A band.").SetOverviewSource("https://en").
						SetOverviewFr("Un groupe.").
						SetOverviewSourceFr("https://fr").
						SetSince(1987).ExecX(e.ctx)

					fr, err := e.svc.Detail(e.ctx, a.ID, "fr")
					Expect(err).NotTo(HaveOccurred())
					Expect(fr.Overview).To(Equal("Un groupe."))
					Expect(fr.OverviewSource).To(Equal("https://fr"))
					Expect(fr.Albums).To(HaveLen(3))
					Expect(fr.Albums[2].Tracks).To(HaveLen(2))

					en, err := e.svc.Detail(e.ctx, a.ID, "en")
					Expect(err).NotTo(HaveOccurred())
					Expect(en.Overview).To(Equal("A band."))
				},
			)

			It(
				"resolves members and credits that are library artists, in one lookup",
				func() {
					queue()
					a, albums := seedLibrary()
					bjork := e.client.Artist.Query().Where().AllX(e.ctx)
					var otherMBID string
					var otherID uint32
					for _, o := range bjork {
						if o.ID != a.ID {
							otherMBID, otherID = o.Mbid, o.ID
						}
					}
					e.client.ArtistMember.Create().
						SetName("Kurt").
						SetMbid(otherMBID).
						SetInstruments("guitar,vocals").
						SetFromYear(0).
						SetToYear(1994).
						SetOrdinal(0).
						SetArtistID(a.ID).
						ExecX(e.ctx)
					e.client.ArtistMember.Create().SetName("Dave").SetMbid("m-dave").
						SetFromYear(1990).
						SetOrdinal(1).SetArtistID(a.ID).ExecX(e.ctx)
					e.client.Artist.UpdateOneID(a.ID).SetSince(1987).ExecX(e.ctx)

					Expect(
						e.store.SetAlbumHydration(
							e.ctx,
							albums[0].ID,
							db.HydrationParams{
								CreditsComplete: true,
								Tracks: []db.TrackSeed{
									{
										MBID:     "t",
										Title:    "Drain You",
										Disc:     1,
										Position: 1,
										Featuring: []db.PersonSeed{
											{Name: "Kurt", MBID: otherMBID},
										},
										Writers: []db.PersonSeed{{Name: "Nobody"}},
									},
								},
								Credits: []db.AlbumCreditSeed{
									{
										Name: "Producer",
										MBID: otherMBID,
										Role: "producer",
									},
								},
								Performers: []db.PerformerSeed{
									{
										Name:        "Pat",
										MBID:        "m-pat",
										Instruments: []string{"guitar"},
										Guest:       true,
									},
								},
							},
							time.Now(),
						),
					).To(Succeed())

					d, err := e.svc.Detail(e.ctx, a.ID, "en")
					Expect(err).NotTo(HaveOccurred())
					Expect(d.Members).To(HaveLen(2))
					Expect(d.Members[0].ArtistID).To(Equal(otherID))
					Expect(
						d.Members[0].Instruments,
					).To(Equal([]string{"guitar", "vocals"}))
					Expect(
						d.Members[0].From,
					).To(Equal(uint16(1987)), "falls back to the artist's start")
					Expect(d.Members[0].To).To(Equal(uint16(1994)))
					Expect(d.Members[1].ArtistID).To(BeZero())
					Expect(d.Members[1].From).To(Equal(uint16(1990)))

					nevermind := d.Albums[2]
					Expect(nevermind.Credits).To(HaveLen(1))
					Expect(nevermind.Credits[0].ArtistID).To(Equal(otherID))
					Expect(nevermind.Credits[0].Role).To(Equal("producer"))
					Expect(nevermind.Personnel).To(HaveLen(1))
					Expect(nevermind.Personnel[0].Guest).To(BeTrue())
					Expect(
						nevermind.Tracks[0].Featuring[0].ArtistID,
					).To(Equal(otherID))
					Expect(nevermind.Tracks[0].Writers[0].ArtistID).To(BeZero())
				},
			)

			It("reports the weakest tier and format of an album's files", func() {
				queue()
				a, albums := seedLibrary()
				tracks := albums[0].QueryTracks().AllX(e.ctx)
				e.client.MediaFile.Update().SetQuality("hires").ExecX(e.ctx)
				f := e.addFile(tracks[1], "/m/2.mp3", "high")
				e.client.MediaFile.UpdateOneID(f.ID).
					SetFormat("mp3").
					SetAudioCodec("mp3").
					SetBitrate(320000).
					ExecX(e.ctx)

				d, err := e.svc.Detail(e.ctx, a.ID, "en")
				Expect(err).NotTo(HaveOccurred())
				nevermind := d.Albums[2]
				Expect(nevermind.Quality).To(Equal("high"))
				Expect(nevermind.Format).To(Equal("MP3 320"))
				Expect(nevermind.Size).To(Equal(int64(2000)))
				Expect(nevermind.TracksHave).To(Equal(uint32(2)))
				Expect(nevermind.Tracks[0].HasFile).To(BeTrue())
				Expect(d.TracksHave).To(Equal(uint32(2)))
			})

			It("leaves quality empty when no file states a tier", func() {
				queue()
				a, _ := seedLibrary()
				e.client.MediaFile.Update().SetQuality("").ExecX(e.ctx)
				d, err := e.svc.Detail(e.ctx, a.ID, "en")
				Expect(err).NotTo(HaveOccurred())
				Expect(d.Albums[2].Quality).To(BeEmpty())
			})

			It("maps a missing artist to ErrArtistNotFound", func() {
				_, err := e.svc.Detail(e.ctx, 999, "en")
				Expect(err).To(MatchError(ErrArtistNotFound))
			})
		})

		Describe("AlbumDetail", func() {
			It("reads one album with its tracks", func() {
				queue()
				_, albums := seedLibrary()
				v, err := e.svc.AlbumDetail(e.ctx, albums[0].ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(v.Tracks).To(HaveLen(2))
				Expect(v.Album.Edges.Artist.Name).To(Equal("Nirvana"))
				Expect(v.Duration).To(BeZero())
			})

			It("maps a missing album to ErrAlbumNotFound", func() {
				_, err := e.svc.AlbumDetail(e.ctx, 999)
				Expect(err).To(MatchError(ErrAlbumNotFound))
			})
		})
	},
)
