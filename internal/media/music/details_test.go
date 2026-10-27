package music

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe(
	"Artist overview and photo",
	Label("unit", "integration", "music"),
	func() {
		var (
			e *env
			a *ent.Artist
		)

		const picture = "https://cdn-images.dzcdn.net/images/artist/abc/1000x1000-000000-80-0-0.jpg"

		BeforeEach(func() {
			e = newEnv(true)
			var err error
			a, err = e.store.CreateArtist(e.ctx, newArtistParams())
			Expect(err).NotTo(HaveOccurred())
		})

		row := func() *ent.Artist { return e.client.Artist.GetX(e.ctx, a.ID) }

		Describe("the overview", func() {
			BeforeEach(func() {
				e.cacheArt(a.ID)
			})

			It("stores every locale with its own source, once", func() {
				e.overviews.EXPECT().
					Overviews(mock.Anything, "Q11649", []string{"en", "fr"}).
					Return(map[string]metadata.Overview{
						"en": {
							Text:      "A band.",
							SourceURL: "https://en.wikipedia.org/wiki/N",
						},
						"fr": {
							Text:      "Un groupe.",
							SourceURL: "https://fr.wikipedia.org/wiki/N",
						},
					}, nil).
					Once()

				e.svc.fillDetails(e.ctx, a.ID, false)

				got := row()
				Expect(got.Overview).To(Equal("A band."))
				Expect(
					got.OverviewSource,
				).To(Equal("https://en.wikipedia.org/wiki/N"))
				Expect(got.OverviewFr).To(Equal("Un groupe."))
				Expect(
					got.OverviewSourceFr,
				).To(Equal("https://fr.wikipedia.org/wiki/N"))
				Expect(got.DetailsFetchedAt).NotTo(BeNil())

				e.svc.fillDetails(e.ctx, a.ID, false)
			})

			It("keeps a locale with no article empty", func() {
				e.overviews.EXPECT().
					Overviews(mock.Anything, "Q11649", []string{"en", "fr"}).
					Return(map[string]metadata.Overview{"en": {Text: "A band."}}, nil).
					Once()
				e.svc.fillDetails(e.ctx, a.ID, false)
				Expect(row().OverviewFr).To(BeEmpty())
			})

			It("is fetched again on a refresh only while English is empty", func() {
				e.overviews.EXPECT().
					Overviews(mock.Anything, "Q11649", []string{"en", "fr"}).
					Return(nil, nil).
					Once()
				e.svc.fillDetails(e.ctx, a.ID, false)

				e.overviews.EXPECT().
					Overviews(mock.Anything, "Q11649", []string{"en", "fr"}).
					Return(map[string]metadata.Overview{"en": {Text: "Now."}}, nil).
					Once()
				e.svc.fillDetails(e.ctx, a.ID, true)
				Expect(row().Overview).To(Equal("Now."))

				e.svc.fillDetails(e.ctx, a.ID, true)
			})

			It("is stamped even when Wikipedia fails", func() {
				e.overviews.EXPECT().
					Overviews(mock.Anything, mock.Anything, mock.Anything).
					Return(nil, errors.New("wikipedia down")).
					Once()
				e.svc.fillDetails(e.ctx, a.ID, false)
				Expect(row().DetailsFetchedAt).NotTo(BeNil())
				Expect(row().Overview).To(BeEmpty())
			})

			It("asks nothing of an artist with no Wikidata item", func() {
				e.client.Artist.UpdateOneID(a.ID).SetWikidataID("").ExecX(e.ctx)
				e.svc.fillDetails(e.ctx, a.ID, false)
				Expect(row().DetailsFetchedAt).NotTo(BeNil())
			})

			It("ignores an artist that is gone", func() {
				e.svc.fillDetails(e.ctx, 999, false)
			})
		})

		Describe("the photo", func() {
			const commons = "https://commons.wikimedia.org/wiki/Special:FilePath/Nirvana.jpg?width=500"

			BeforeEach(func() {
				e.client.Artist.UpdateOneID(a.ID).SetWikidataID("").ExecX(e.ctx)
			})

			linkedArt := func(art metadata.ArtistArt) {
				e.links.EXPECT().
					ArtistArt(mock.Anything, a.Mbid).
					Return(art, nil).
					Once()
			}
			fetches := func(url string) {
				e.posters.EXPECT().
					Fetch(mock.Anything, "artists", a.ID, url).
					Return(nil).
					Once()
			}

			Context("with a Deezer link from MusicBrainz", func() {
				BeforeEach(func() {
					e.client.Artist.UpdateOneID(a.ID).SetDeezerID(415).ExecX(e.ctx)
				})

				It(
					"takes the picture the link names when no other artist shares it",
					func() {
						e.links.EXPECT().
							DeezerArtistShared(mock.Anything, uint32(415)).
							Return(false, nil).
							Once()
						e.photos.EXPECT().ArtistByID(mock.Anything, uint32(415)).
							Return(&metadata.DeezerArtist{ID: 415, Name: "Nirvana", PictureURL: picture}, nil).
							Once()
						fetches(picture)

						e.svc.fillDetails(e.ctx, a.ID, false)
					},
				)

				It(
					"takes MusicBrainz's own image when another artist shares the page",
					func() {
						e.links.EXPECT().
							DeezerArtistShared(mock.Anything, uint32(415)).
							Return(true, nil).
							Once()
						linkedArt(
							metadata.ArtistArt{DeezerID: 415, ImageURL: commons},
						)
						fetches(commons)

						e.svc.fillDetails(e.ctx, a.ID, false)
					},
				)

				It(
					"takes no picture from a shared page when MusicBrainz links no image",
					func() {
						e.links.EXPECT().
							DeezerArtistShared(mock.Anything, uint32(415)).
							Return(true, nil).
							Once()
						linkedArt(metadata.ArtistArt{DeezerID: 415})

						e.svc.fillDetails(e.ctx, a.ID, false)
					},
				)

				It("takes no picture when the page cannot be checked", func() {
					e.links.EXPECT().DeezerArtistShared(mock.Anything, uint32(415)).
						Return(false, &metadata.RateLimitedError{}).Once()

					e.svc.fillDetails(e.ctx, a.ID, false)
				})

				It(
					"falls back to MusicBrainz's image over an empty Deezer hash",
					func() {
						e.links.EXPECT().
							DeezerArtistShared(mock.Anything, uint32(415)).
							Return(false, nil).
							Once()
						e.photos.EXPECT().ArtistByID(mock.Anything, uint32(415)).
							Return(&metadata.DeezerArtist{
								ID:         415,
								PictureURL: "https://cdn-images.dzcdn.net/images/artist//1000x1000.jpg",
							}, nil).Once()
						linkedArt(metadata.ArtistArt{})

						e.svc.fillDetails(e.ctx, a.ID, false)
					},
				)
			})

			It("uses the name only when exactly one hit folds equal to it", func() {
				e.photos.EXPECT().SearchArtists(mock.Anything, "Nirvana").
					Return([]metadata.DeezerArtist{
						{
							ID:         1,
							Name:       "Nirvana Tribute",
							PictureURL: "https://cdn/other.jpg",
						},
						{ID: 2, Name: "NIRVANA", PictureURL: picture},
					}, nil).Once()
				fetches(picture)

				e.svc.fillDetails(e.ctx, a.ID, false)
			})

			It("takes MusicBrainz's own image for two namesakes", func() {
				e.photos.EXPECT().SearchArtists(mock.Anything, "Nirvana").
					Return([]metadata.DeezerArtist{
						{ID: 1, Name: "Nirvana", PictureURL: picture},
						{
							ID:         2,
							Name:       "Nirvana",
							PictureURL: "https://cdn/other.jpg",
						},
					}, nil).Once()
				linkedArt(metadata.ArtistArt{ImageURL: commons})
				fetches(commons)

				e.svc.fillDetails(e.ctx, a.ID, false)
			})

			It(
				"takes no picture when no hit matches the name and MusicBrainz links none",
				func() {
					e.photos.EXPECT().SearchArtists(mock.Anything, "Nirvana").
						Return([]metadata.DeezerArtist{{ID: 1, Name: "Other", PictureURL: picture}}, nil).
						Once()
					linkedArt(metadata.ArtistArt{})

					e.svc.fillDetails(e.ctx, a.ID, false)
				},
			)

			It("leaves a cached photo alone", func() {
				e.cacheArt(a.ID)
				e.svc.fillDetails(e.ctx, a.ID, false)
			})

			It("survives Deezer failing", func() {
				e.photos.EXPECT().SearchArtists(mock.Anything, "Nirvana").
					Return(nil, errors.New("deezer down")).Once()
				e.svc.fillDetails(e.ctx, a.ID, false)
				Expect(row().DetailsFetchedAt).NotTo(BeNil())
			})

			It("survives MusicBrainz failing", func() {
				e.photos.EXPECT().SearchArtists(mock.Anything, "Nirvana").
					Return(nil, nil).Once()
				e.links.EXPECT().ArtistArt(mock.Anything, a.Mbid).
					Return(metadata.ArtistArt{}, errors.New("musicbrainz down")).
					Once()
				e.svc.fillDetails(e.ctx, a.ID, false)
				Expect(row().DetailsFetchedAt).NotTo(BeNil())
			})
		})
	},
)
