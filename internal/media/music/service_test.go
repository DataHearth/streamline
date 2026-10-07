package music

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
	mockmeta "github.com/datahearth/streamline/internal/metadata/mocks"
	mockposters "github.com/datahearth/streamline/internal/posters/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Music service", Label("integration", "music"), func() {
	var (
		ctx      context.Context
		client   *ent.Client
		provider *mockmeta.MockMusicProvider
		posters  *mockposters.MockManager
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
		svc = NewService(db.New(client), provider, posters)
		configtest.Setup(map[string]any{
			"library": map[string]any{"music_path": GinkgoT().TempDir()},
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
})
