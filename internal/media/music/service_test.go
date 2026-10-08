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
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	mockdownload "github.com/datahearth/streamline/internal/download/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	mockindexer "github.com/datahearth/streamline/internal/indexer/mocks"
	"github.com/datahearth/streamline/internal/metadata"
	mockmeta "github.com/datahearth/streamline/internal/metadata/mocks"
	mockposters "github.com/datahearth/streamline/internal/posters/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Music service", Label("unit", "integration", "music"), func() {
	var (
		ctx      context.Context
		client   *ent.Client
		provider *mockmeta.MockMusicProvider
		posters  *mockposters.MockManager
		idx      *mockindexer.MockManager
		dl       *mockdownload.MockDownloader
		svc      *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		provider = mockmeta.NewMockMusicProvider(GinkgoT())
		posters = mockposters.NewMockManager(GinkgoT())
		idx = mockindexer.NewMockManager(GinkgoT())
		dl = mockdownload.NewMockDownloader(GinkgoT())
		svc = NewService(db.New(client), provider, posters, idx, dl)
		configtest.Setup(map[string]any{
			"library": map[string]any{"music_path": GinkgoT().TempDir()},
			"music_quality_profiles": []map[string]any{
				{
					"name":    "lossless",
					"formats": []string{"flac-24", "flac", "mp3-320"},
					"cutoff":  "flac",
				},
			},
			"music_quality_default_profile": "lossless",
		})
	})

	addSeeded := func() *ent.Artist {
		GinkgoHelper()
		provider.EXPECT().
			GetArtist(mock.Anything, "mbid-1").
			Return(&metadata.ArtistDetails{
				MBID:     "mbid-1",
				Name:     "Nirvana",
				SortName: "Nirvana",
				ReleaseGroups: []metadata.ReleaseGroupInfo{
					{
						MBID:  "rg-1",
						Title: "Nevermind",
						Type:  metadata.AlbumTypeAlbum,
					},
				},
			}, nil).
			Once()
		provider.EXPECT().
			GetReleaseGroup(mock.Anything, "rg-1").
			Return(&metadata.ReleaseGroupDetails{
				MBID:        "rg-1",
				Title:       "Nevermind",
				ReleaseMBID: "rel-1",
				Tracks: []metadata.TrackInfo{
					{
						MBID:     "rec-1",
						Title:    "Smells Like Teen Spirit",
						Disc:     1,
						Position: 1,
					},
				},
			}, nil).
			Once()
		fetched := make(chan struct{})
		posters.EXPECT().
			Fetch(mock.Anything, "albums", mock.Anything, metadata.CoverArtURL("rg-1")).
			Run(func(context.Context, string, uint32, string) { close(fetched) }).
			Return(nil).
			Once()
		artist, err := svc.Add(ctx, AddParams{MBID: "mbid-1", Monitored: true})
		Expect(err).NotTo(HaveOccurred())
		Eventually(fetched).Should(BeClosed())
		return artist
	}

	Describe("Add", func() {
		It("creates the artist with albums and tracks from MusicBrainz", func() {
			artist := addSeeded()
			Expect(artist.Name).To(Equal("Nirvana"))
			Expect(
				artist.Path,
			).To(Equal(filepath.Join(config.Get().Library.MusicPath, "Nirvana")))
			Expect(artist.Edges.Albums).To(HaveLen(1))
			Expect(artist.Edges.Albums[0].Edges.Tracks).To(HaveLen(1))
		})

		It("rejects a duplicate mbid", func() {
			client.Artist.Create().SetMbid("mbid-1").SetName("Nirvana").SaveX(ctx)
			_, err := svc.Add(ctx, AddParams{MBID: "mbid-1"})
			Expect(err).To(MatchError(ErrArtistExists))
		})
	})

	Describe("Get", func() {
		It("maps a missing row to ErrArtistNotFound", func() {
			_, err := svc.Get(ctx, 999)
			Expect(err).To(MatchError(ErrArtistNotFound))
		})
	})

	Describe("RefreshOne", func() {
		It(
			"adds a new release-group inheriting the artist's monitored flag and leaves existing albums' monitored/status alone",
			func() {
				artist := addSeeded()
				rg1 := artist.Edges.Albums[0]
				client.Album.UpdateOneID(rg1.ID).
					SetMonitored(false).SetStatus(album.StatusSkipped).ExecX(ctx)

				provider.EXPECT().
					GetArtist(mock.Anything, "mbid-1").
					Return(&metadata.ArtistDetails{
						MBID:     "mbid-1",
						Name:     "Nirvana",
						SortName: "Nirvana",
						ReleaseGroups: []metadata.ReleaseGroupInfo{
							{
								MBID:  "rg-1",
								Title: "Nevermind (Remaster)",
								Type:  metadata.AlbumTypeAlbum,
							},
							{
								MBID:  "rg-2",
								Title: "In Utero",
								Type:  metadata.AlbumTypeAlbum,
							},
						},
					}, nil).
					Once()
				provider.EXPECT().
					GetReleaseGroup(mock.Anything, "rg-2").
					Return(&metadata.ReleaseGroupDetails{
						MBID:        "rg-2",
						Title:       "In Utero",
						ReleaseMBID: "rel-2",
						Tracks: []metadata.TrackInfo{
							{
								MBID:     "rec-2",
								Title:    "Heart-Shaped Box",
								Disc:     1,
								Position: 1,
							},
						},
					}, nil).
					Once()
				fetched := make(chan struct{})
				posters.EXPECT().
					Fetch(mock.Anything, "albums", mock.Anything, metadata.CoverArtURL("rg-2")).
					Run(func(context.Context, string, uint32, string) { close(fetched) }).
					Return(nil).
					Once()

				got, err := svc.RefreshOne(ctx, artist.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.LastRefreshedAt).NotTo(BeNil())
				Expect(got.Edges.Albums).To(HaveLen(2))

				old := client.Album.GetX(ctx, rg1.ID)
				Expect(old.Title).To(Equal("Nevermind (Remaster)"))
				Expect(old.Monitored).To(BeFalse())
				Expect(old.Status).To(Equal(album.StatusSkipped))
				added := client.Album.Query().Where(album.MbidEQ("rg-2")).OnlyX(ctx)
				Expect(added.Monitored).To(BeTrue())
				Eventually(fetched).Should(BeClosed())
			},
		)
	})

	Describe("GetAlbum", func() {
		It("returns the album with its tracks", func() {
			artist := addSeeded()
			got, err := svc.GetAlbum(ctx, artist.Edges.Albums[0].ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.Tracks).To(HaveLen(1))
		})

		It("maps a missing row to ErrAlbumNotFound", func() {
			_, err := svc.GetAlbum(ctx, 999)
			Expect(err).To(MatchError(ErrAlbumNotFound))
		})
	})

	Describe("SetArtistQualityProfile", func() {
		It("stores the profile name", func() {
			artist := addSeeded()
			Expect(
				svc.SetArtistQualityProfile(ctx, artist.ID, "lossless"),
			).To(Succeed())
			Expect(
				client.Artist.GetX(ctx, artist.ID).QualityProfile,
			).To(Equal("lossless"))
		})

		It("maps a missing artist to ErrArtistNotFound", func() {
			Expect(
				svc.SetArtistQualityProfile(ctx, 999, "lossless"),
			).To(MatchError(ErrArtistNotFound))
		})
	})

	Describe("SetArtistMonitored", func() {
		It("cascades to the artist's albums", func() {
			artist := addSeeded()
			Expect(svc.SetArtistMonitored(ctx, artist.ID, false)).To(Succeed())
			Expect(
				client.Album.Query().Where(album.Monitored(true)).Count(ctx),
			).To(BeZero())
			Expect(client.Artist.GetX(ctx, artist.ID).Monitored).To(BeFalse())
		})

		It("maps a missing artist to ErrArtistNotFound", func() {
			Expect(
				svc.SetArtistMonitored(ctx, 999, false),
			).To(MatchError(ErrArtistNotFound))
		})

		It("maps a missing album to ErrAlbumNotFound", func() {
			Expect(
				svc.SetAlbumMonitored(ctx, 999, false),
			).To(MatchError(ErrAlbumNotFound))
		})
	})

	Describe("Delete", func() {
		seedFile := func(artist *ent.Artist) string {
			GinkgoHelper()
			dir := filepath.Join(
				config.Get().Library.MusicPath,
				"Nirvana",
				"Nevermind",
			)
			Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
			f := filepath.Join(dir, "01.flac")
			Expect(os.WriteFile(f, []byte("x"), 0o600)).To(Succeed())
			tr := artist.Edges.Albums[0].Edges.Tracks[0]
			client.MediaFile.Create().
				SetPath(f).
				SetSize(1).
				SetTrackID(tr.ID).
				SaveX(ctx)
			return f
		}

		It(
			"cascades albums and tracks, keeps files unless asked, and evicts the album posters",
			func() {
				artist := addSeeded()
				f := seedFile(artist)
				posters.EXPECT().Remove("albums", mock.Anything).Return(nil).Once()

				Expect(svc.Delete(ctx, artist.ID, false)).To(Succeed())
				Expect(f).To(BeAnExistingFile())
				Expect(client.Album.Query().Count(ctx)).To(BeZero())
				Expect(client.Track.Query().Count(ctx)).To(BeZero())
			},
		)

		It("removes the files on disk when asked", func() {
			artist := addSeeded()
			f := seedFile(artist)
			posters.EXPECT().Remove("albums", mock.Anything).Return(nil).Once()

			Expect(svc.Delete(ctx, artist.ID, true)).To(Succeed())
			Expect(f).NotTo(BeAnExistingFile())
		})

		It("maps a missing artist to ErrArtistNotFound", func() {
			Expect(svc.Delete(ctx, 999, false)).To(MatchError(ErrArtistNotFound))
		})
	})

	Describe("SearchAlbumReleases", func() {
		It(
			"scores, drops rejects and orders best first with seeders as tiebreak",
			func() {
				artist := addSeeded()
				al := artist.Edges.Albums[0]
				year := time.Date(1991, 9, 24, 0, 0, 0, 0, time.UTC)
				client.Album.UpdateOneID(al.ID).SetReleaseDate(year).ExecX(ctx)

				idx.EXPECT().
					SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(1991)).
					Return([]indexer.SearchResult{
						{Title: "Nirvana - Nevermind (1991) [MP3 320]", Seeders: 50},
						{Title: "Nirvana - Nevermind (1991) [FLAC]", Seeders: 3},
						{Title: "Nirvana - Nevermind (1991) [MP3 192]", Seeders: 99},
						{
							Title:   "Nirvana - Nevermind (1991) [FLAC] rip",
							Seeders: 10,
						},
						{
							Title:   "Nirvana - Nevermind (1991) [24bit FLAC]",
							Seeders: 1,
						},
					}, nil).
					Once()

				got, err := svc.SearchAlbumReleases(ctx, al.ID)
				Expect(err).NotTo(HaveOccurred())

				titles := make([]string, len(got))
				for i, g := range got {
					titles[i] = g.Result.Title
				}
				Expect(titles).To(Equal([]string{
					"Nirvana - Nevermind (1991) [24bit FLAC]",
					"Nirvana - Nevermind (1991) [FLAC] rip",
					"Nirvana - Nevermind (1991) [FLAC]",
					"Nirvana - Nevermind (1991) [MP3 320]",
				}))
				Expect(got[0].Format).To(Equal("flac-24"))
				Expect(got[0].Score).To(BeNumerically(">", got[3].Score))
			},
		)

		It("searches with year 0 when the album has no release date", func() {
			artist := addSeeded()
			idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(0)).
				Return(nil, nil).
				Once()

			got, err := svc.SearchAlbumReleases(ctx, artist.Edges.Albums[0].ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})

		It("maps a missing album to ErrAlbumNotFound", func() {
			_, err := svc.SearchAlbumReleases(ctx, 999)
			Expect(err).To(MatchError(ErrAlbumNotFound))
		})

		It("errors when no music profile is configured", func() {
			artist := addSeeded()
			configtest.Setup(map[string]any{
				"library": map[string]any{"music_path": GinkgoT().TempDir()},
			})

			_, err := svc.SearchAlbumReleases(ctx, artist.Edges.Albums[0].ID)
			Expect(err).To(MatchError(ErrNoQualityProfile))
		})
	})

	Describe("GrabAlbumRelease", func() {
		result := indexer.SearchResult{
			Title:    "Nirvana - Nevermind (1991) [FLAC]",
			Download: "magnet:?xt=urn:btih:abc",
		}

		It("grabs the release and marks the album downloading", func() {
			artist := addSeeded()
			al := artist.Edges.Albums[0]
			dl.EXPECT().
				GrabAlbum(mock.Anything, result, al.ID).
				Return(&ent.DownloadRecord{}, nil).
				Once()

			Expect(svc.GrabAlbumRelease(ctx, al.ID, result)).To(Succeed())
			Expect(client.Album.GetX(ctx, al.ID).Status).
				To(Equal(album.StatusDownloading))
		})

		It(
			"leaves the album wanted and grab_failures at zero when the grab fails",
			func() {
				artist := addSeeded()
				al := artist.Edges.Albums[0]
				boom := errors.New("client unreachable")
				dl.EXPECT().
					GrabAlbum(mock.Anything, result, al.ID).
					Return(nil, boom).
					Once()

				err := svc.GrabAlbumRelease(ctx, al.ID, result)
				Expect(err).To(MatchError(boom))
				Expect(err).To(MatchError(ContainSubstring("grab album")))
				after := client.Album.GetX(ctx, al.ID)
				Expect(after.Status).To(Equal(album.StatusWanted))
				Expect(after.GrabFailures).To(BeZero())
			},
		)
	})
})
