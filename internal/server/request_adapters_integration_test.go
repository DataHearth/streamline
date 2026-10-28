package server

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/media/music"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	postersmocks "github.com/datahearth/streamline/internal/posters/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe(
	"album request approval",
	Label("integration", "server", "music"),
	func() {
		It(
			"monitors a shared album once its first credited artist is added",
			func() {
				ctx := context.Background()
				root := GinkgoT().TempDir()
				configtest.Setup(map[string]any{
					"library": map[string]any{
						"music_path": filepath.Join(root, "music"),
					},
				})
				client := dbtest.SetupTestDB(ctx)
				DeferCleanup(func() { client.Close() })
				store := db.New(client)

				_, err := store.CreateArtist(ctx, db.CreateArtistParams{
					MBID: "a-off", Name: "The Offspring",
					Albums: []db.AlbumSeed{
						{
							MBID:  "rg-collab",
							Title: "Let the Good Times Roll",
							Type:  "single",
						},
					},
				})
				Expect(err).NotTo(HaveOccurred())

				mb := metamocks.NewMockMusicProvider(GinkgoT())
				mb.EXPECT().
					GetArtist(mock.Anything, "a-ec").
					Return(&metadata.ArtistDetails{
						MBID: "a-ec",
						Name: "Electric Callboy",
						ReleaseGroups: []metadata.ReleaseGroupInfo{
							{
								MBID:  "rg-collab",
								Title: "Let the Good Times Roll",
								Type:  metadata.AlbumTypeSingle,
							},
						},
					}, nil).
					Once()
				covers := postersmocks.NewMockManager(GinkgoT())
				covers.EXPECT().Path(mock.Anything, mock.Anything).
					Return(filepath.Join(root, "no-cover")).Maybe()
				svc := music.NewService(
					store,
					mb,
					covers,
					nil,
					nil,
					nil,
					nil,
					nil,
					nil,
					nil,
				)

				Expect(
					artistRequestAdder{
						svc: svc,
					}.AddArtist(
						ctx,
						"a-ec",
						"manual",
						"",
					),
				).
					To(Succeed())
				held, err := albumRequestMonitor{svc: svc, store: store}.
					MonitorAlbum(ctx, "a-ec", "rg-collab")
				Expect(err).NotTo(HaveOccurred())
				Expect(held).To(BeFalse())

				rg := client.Album.Query().Where(album.MbidEQ("rg-collab")).
					WithArtist().WithCoArtists().OnlyX(ctx)
				Expect(rg.Monitored).To(BeTrue())
				Expect(rg.Edges.Artist.Mbid).To(Equal("a-off"))
				Expect(rg.Edges.CoArtists).To(HaveLen(1))
				Expect(rg.Edges.CoArtists[0].Mbid).To(Equal("a-ec"))
			},
		)
	},
)
