package music

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
)

var _ = Describe("Music searches", Label("unit", "integration", "music"), func() {
	var e *env

	BeforeEach(func() {
		e = newEnv(false)
	})

	titles := func(rs []AlbumRelease) []string {
		out := make([]string, len(rs))
		for i, r := range rs {
			out[i] = r.Result.Title
		}
		return out
	}

	Describe("SearchAlbumReleases", func() {
		var al *ent.Album

		BeforeEach(func() {
			_, albums := e.seedArtist("Nirvana", albumSeed{
				mbid:      "rg-1",
				title:     "Nevermind",
				monitored: true,
				tracks:    []string{"A"},
				date:      new(time.Date(1991, 9, 24, 0, 0, 0, 0, time.UTC)),
			})
			al = albums[0]
		})

		It("judges every release, keeping the rejected ones set aside", func() {
			e.idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(1991)).
				Return([]indexer.SearchResult{
					{Title: "Nirvana - Nevermind (1991) [MP3 192]", Seeders: 99},
					{Title: "Nirvana - Nevermind (1991) [FLAC]", Seeders: 3},
					{Title: "Nirvana - Nevermind (1991)", Seeders: 70},
					{Title: "Nirvana - Nevermind (1991) [MP3 320]", Seeders: 50},
					{Title: "Nirvana - Nevermind (1991) [24bit FLAC]", Seeders: 1},
					{
						Title:   "Nirvana - Discography (1989-1994) [FLAC]",
						Seeders: 500,
					},
				}, nil).
				Once()

			got, err := e.svc.SearchAlbumReleases(e.ctx, al.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(titles(got)).To(Equal([]string{
				"Nirvana - Nevermind (1991) [24bit FLAC]",
				"Nirvana - Nevermind (1991) [FLAC]",
				"Nirvana - Nevermind (1991) [MP3 320]",
				"Nirvana - Nevermind (1991) [MP3 192]",
				"Nirvana - Nevermind (1991)",
			}))
			Expect(got[0].Rejected()).To(BeFalse())
			Expect(got[0].Score).To(BeNumerically(">", got[2].Score))
			Expect(got[0].Parsed.Source).To(Equal("FLAC 24-bit"))
			Expect(got[3].Rejected()).To(BeTrue())
			Expect(got[3].Reason).To(Equal("tier not in the profile"))
			Expect(
				got[4].Reason,
			).To(Equal("quality is not stated in the release name"))
		})

		It("searches with year 0 when the album has no release date", func() {
			e.client.Album.UpdateOneID(al.ID).ClearReleaseDate().ExecX(e.ctx)
			e.idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(0)).
				Return(nil, nil).
				Once()
			got, err := e.svc.SearchAlbumReleases(e.ctx, al.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})

		It("maps a missing album to ErrAlbumNotFound", func() {
			_, err := e.svc.SearchAlbumReleases(e.ctx, 999)
			Expect(err).To(MatchError(ErrAlbumNotFound))
		})

		It("errors when no music profile is configured", func() {
			e.setConfig(map[string]any{
				"music_quality_profiles":        []map[string]any{},
				"music_quality_default_profile": "",
			})
			e.client.Artist.Update().SetQualityProfile("").ExecX(e.ctx)
			_, err := e.svc.SearchAlbumReleases(e.ctx, al.ID)
			Expect(err).To(MatchError(ErrNoQualityProfile))
		})

		It("passes an indexer failure through", func() {
			boom := errors.New("indexers down")
			e.idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(1991)).
				Return(nil, boom).
				Once()
			_, err := e.svc.SearchAlbumReleases(e.ctx, al.ID)
			Expect(err).To(MatchError(boom))
		})
	})

	Describe("BrowseArtistReleases", func() {
		var a *ent.Artist

		BeforeEach(func() {
			a, _ = e.seedArtist(
				"Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind"},
			)
		})

		It(
			"keeps only this artist's discographies, judged against the profile",
			func() {
				e.idx.EXPECT().SearchArtist(mock.Anything, "Nirvana").
					Return([]indexer.SearchResult{
						{
							Title:   "Nirvana - Discography (1989-1994) [FLAC]",
							Seeders: 5,
						},
						{Title: "Nirvana - Nevermind (1991) [FLAC]", Seeders: 50},
						{
							Title:   "Nirvana Tribute - Discography (1989-1994) [FLAC]",
							Seeders: 80,
						},
						{Title: "Nirvana Discography FLAC", Seeders: 70},
						{
							Title:   "NIRVANA - Complete Collection [MP3 128]",
							Seeders: 9,
						},
					}, nil).Once()

				got, err := e.svc.BrowseArtistReleases(e.ctx, a.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(titles(got)).To(Equal([]string{
					"Nirvana - Discography (1989-1994) [FLAC]",
					"NIRVANA - Complete Collection [MP3 128]",
				}))
				Expect(got[0].Rejected()).To(BeFalse())
				Expect(got[1].Rejected()).To(BeTrue())
			},
		)

		It("errors without a usable profile", func() {
			e.setConfig(map[string]any{
				"music_quality_profiles":        []map[string]any{},
				"music_quality_default_profile": "",
			})
			e.client.Artist.UpdateOneID(a.ID).SetQualityProfile("").ExecX(e.ctx)
			_, err := e.svc.BrowseArtistReleases(e.ctx, a.ID)
			Expect(err).To(MatchError(ErrNoQualityProfile))
		})

		It("maps a missing artist to ErrArtistNotFound", func() {
			_, err := e.svc.BrowseArtistReleases(e.ctx, 999)
			Expect(err).To(MatchError(ErrArtistNotFound))
		})
	})

	Describe("GrabArtistRelease", func() {
		result := indexer.SearchResult{
			Title:    "Nirvana - Discography [FLAC]",
			Download: "magnet:?xt=urn:btih:abc",
		}
		var a *ent.Artist

		BeforeEach(func() {
			a, _ = e.seedArtist(
				"Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind"},
			)
		})

		It("hands the pack to the download manager", func() {
			e.dl.EXPECT().GrabArtistPack(mock.Anything, result, a.ID).
				Return(&ent.DownloadRecord{}, nil).Once()
			Expect(e.svc.GrabArtistRelease(e.ctx, a.ID, result, false)).To(Succeed())
		})

		It("flags the record to replace what the albums already hold", func() {
			rec := e.client.DownloadRecord.Create().
				SetTitle("t").
				SetArtistID(a.ID).
				SaveX(e.ctx)
			e.dl.EXPECT().
				GrabArtistPack(mock.Anything, result, a.ID).
				Return(rec, nil).
				Once()
			Expect(e.svc.GrabArtistRelease(e.ctx, a.ID, result, true)).To(Succeed())
			Expect(e.client.DownloadRecord.GetX(e.ctx, rec.ID).ReplaceMode).
				To(Equal(downloadrecord.ReplaceModeAll))
		})

		It("passes a refusal through", func() {
			e.dl.EXPECT().GrabArtistPack(mock.Anything, result, a.ID).
				Return(nil, download.ErrNoWantedFiles).Once()
			err := e.svc.GrabArtistRelease(e.ctx, a.ID, result, false)
			Expect(err).To(MatchError(download.ErrNoWantedFiles))
		})

		It("needs a usable profile", func() {
			e.setConfig(map[string]any{
				"music_quality_profiles":        []map[string]any{},
				"music_quality_default_profile": "",
			})
			e.client.Artist.UpdateOneID(a.ID).SetQualityProfile("").ExecX(e.ctx)
			err := e.svc.GrabArtistRelease(e.ctx, a.ID, result, false)
			Expect(err).To(MatchError(ErrNoQualityProfile))
		})

		It("maps a missing artist to ErrArtistNotFound", func() {
			err := e.svc.GrabArtistRelease(e.ctx, 999, result, false)
			Expect(err).To(MatchError(ErrArtistNotFound))
		})
	})

	Describe("search-now", func() {
		expectPass := func(albumID uint32, title string, done chan<- struct{}) {
			release := indexer.SearchResult{
				Title:    "Nirvana - " + title + " (1991) [FLAC]",
				Download: "magnet:?xt=urn:btih:" + title,
			}
			e.idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", title, mock.Anything).
				Return([]indexer.SearchResult{release}, nil).
				Once()
			e.dl.EXPECT().GrabAlbum(mock.Anything, release, albumID).
				Run(func(_ context.Context, _ indexer.SearchResult, _ uint32) {
					done <- struct{}{}
				}).
				Return(&ent.DownloadRecord{}, nil).Once()
		}

		Describe("SearchAlbumNow", func() {
			It("runs one pass in the background, waiving the cooldown", func() {
				_, albums := e.seedArtist("Nirvana", albumSeed{
					mbid:      "rg-1",
					title:     "Nevermind",
					monitored: true,
					tracks:    []string{"A"},
				})
				e.client.Album.UpdateOneID(albums[0].ID).
					SetLastSearchAt(time.Now()).SetGrabFailures(3).ExecX(e.ctx)
				done := make(chan struct{}, 1)
				expectPass(albums[0].ID, "Nevermind", done)

				Expect(e.svc.SearchAlbumNow(e.ctx, albums[0].ID)).To(Succeed())
				Eventually(done).Should(Receive())
			})

			DescribeTable(
				"does nothing for an album that cannot be searched",
				func(prepare func(e *env, al *ent.Album)) {
					_, albums := e.seedArtist("Nirvana", albumSeed{
						mbid:      "rg-1",
						title:     "Nevermind",
						monitored: true,
						tracks:    []string{"A"},
					})
					prepare(e, albums[0])
					Expect(e.svc.SearchAlbumNow(e.ctx, albums[0].ID)).To(Succeed())
				},
				Entry("available", func(e *env, al *ent.Album) {
					e.client.Album.UpdateOneID(al.ID).
						SetStatus(album.StatusAvailable).
						ExecX(e.ctx)
				}),
				Entry("upcoming", func(e *env, al *ent.Album) {
					e.client.Album.UpdateOneID(al.ID).
						SetReleaseDate(time.Now().Add(48 * time.Hour)).ExecX(e.ctx)
				}),
				Entry("already downloading", func(e *env, al *ent.Album) {
					e.client.DownloadRecord.Create().SetTitle("t").
						SetStatus(downloadrecord.StatusDownloading).
						SetAlbumID(al.ID).ExecX(e.ctx)
				}),
				Entry("covered by a pack in flight", func(e *env, al *ent.Album) {
					a := e.client.Artist.Query().OnlyX(e.ctx)
					e.client.DownloadRecord.Create().SetTitle("t").
						SetStatus(downloadrecord.StatusDownloading).
						SetArtistID(a.ID).AddAlbumIDs(al.ID).ExecX(e.ctx)
				}),
				Entry(
					"without an enabled download client",
					func(e *env, _ *ent.Album) {
						e.setConfig(
							map[string]any{"download_clients": []map[string]any{}},
						)
					},
				),
			)

			It("maps a missing album to ErrAlbumNotFound", func() {
				Expect(
					e.svc.SearchAlbumNow(e.ctx, 999),
				).To(MatchError(ErrAlbumNotFound))
			})
		})

		Describe("SearchArtistNow", func() {
			It(
				"searches the wanted, released, hydrated albums one after the other",
				func() {
					a, albums := e.seedArtist(
						"Nirvana",
						albumSeed{
							mbid:      "rg-1",
							title:     "Nevermind",
							monitored: true,
							tracks:    []string{"A"},
							date:      new(time.Now().Add(-1000 * time.Hour)),
						},
						albumSeed{
							mbid:      "rg-2",
							title:     "In Utero",
							monitored: true,
							tracks:    []string{"A"},
							date:      new(time.Now().Add(-2000 * time.Hour)),
						},
						albumSeed{mbid: "rg-3", title: "Stub", monitored: true},
						albumSeed{
							mbid:   "rg-4",
							title:  "Skipped",
							tracks: []string{"A"},
						},
					)
					done := make(chan struct{}, 2)
					expectPass(albums[0].ID, "Nevermind", done)
					expectPass(albums[1].ID, "In Utero", done)

					Expect(e.svc.SearchArtistNow(e.ctx, a.ID)).To(Succeed())
					Eventually(done).Should(Receive())
					Eventually(done).Should(Receive())
				},
			)

			It("returns at once when nothing is wanted", func() {
				a, _ := e.seedArtist("Nirvana")
				Expect(e.svc.SearchArtistNow(e.ctx, a.ID)).To(Succeed())
			})

			It("maps a missing artist to ErrArtistNotFound", func() {
				Expect(
					e.svc.SearchArtistNow(e.ctx, 999),
				).To(MatchError(ErrArtistNotFound))
			})
		})

		Describe("SearchTrackNow", func() {
			It("runs the pass of the track's album", func() {
				_, albums := e.seedArtist("Nirvana", albumSeed{
					mbid:      "rg-1",
					title:     "Nevermind",
					monitored: true,
					tracks:    []string{"A"},
				})
				done := make(chan struct{}, 1)
				expectPass(albums[0].ID, "Nevermind", done)

				tr := albums[0].QueryTracks().FirstX(e.ctx)
				Expect(e.svc.SearchTrackNow(e.ctx, tr.ID)).To(Succeed())
				Eventually(done).Should(Receive())
			})

			It("maps a missing track to ErrTrackNotFound", func() {
				Expect(
					e.svc.SearchTrackNow(e.ctx, 999),
				).To(MatchError(ErrTrackNotFound))
			})
		})
	})
})
