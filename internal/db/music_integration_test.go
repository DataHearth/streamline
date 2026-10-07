package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
)

var _ = Describe("Music persistence", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
	})

	seed := func() *ent.Artist {
		GinkgoHelper()
		early := time.Date(1991, 9, 24, 0, 0, 0, 0, time.UTC)
		late := time.Date(1993, 9, 21, 0, 0, 0, 0, time.UTC)
		a, err := store.CreateArtist(ctx, CreateArtistParams{
			MBID: "a-1", Name: "Nirvana", Monitored: true,
			Albums: []AlbumSeed{
				{MBID: "rg-2", Title: "In Utero", Type: "album", ReleaseDate: &late},
				{
					MBID:        "rg-1",
					Title:       "Nevermind",
					Type:        "album",
					ReleaseDate: &early,
					Tracks: []TrackSeed{
						{MBID: "t-2", Title: "In Bloom", Disc: 1, Position: 2},
						{
							MBID:     "t-1",
							Title:    "Smells Like Teen Spirit",
							Disc:     1,
							Position: 1,
						},
					},
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		return a
	}

	It("creates the tree and orders albums by date and tracks by position", func() {
		a := seed()
		Expect(a.Edges.Albums).To(HaveLen(2))
		Expect(a.Edges.Albums[0].Mbid).To(Equal("rg-1"))
		Expect(
			a.Edges.Albums[0].Edges.Tracks[0].Title,
		).To(Equal("Smells Like Teen Spirit"))
	})

	It("returns nil, nil for an unknown mbid", func() {
		row, err := store.FindArtistByMBID(ctx, "nope")
		Expect(err).NotTo(HaveOccurred())
		Expect(row).To(BeNil())
	})

	It("cascades monitored to every album", func() {
		a := seed()
		Expect(store.SetArtistMonitored(ctx, a.ID, false)).To(Succeed())
		Expect(
			client.Album.Query().Where(album.Monitored(true)).Count(ctx),
		).To(BeZero())
	})

	It(
		"refreshes existing albums without touching monitored or status, and adds new ones inheriting the artist flag",
		func() {
			a := seed()
			rg1 := a.Edges.Albums[0]
			Expect(store.SetAlbumMonitored(ctx, rg1.ID, false)).To(Succeed())
			client.Album.UpdateOneID(rg1.ID).
				SetStatus(album.StatusAvailable).
				ExecX(ctx)

			Expect(store.RefreshArtist(ctx, a.ID, RefreshArtistParams{
				Name: "Nirvana", RefreshedAt: time.Now(),
				Albums: []AlbumSeed{
					{MBID: "rg-1", Title: "Nevermind (Remaster)", Type: "album"},
					{MBID: "rg-3", Title: "Bleach", Type: "album"},
				},
			})).To(Succeed())

			got := client.Album.GetX(ctx, rg1.ID)
			Expect(got.Title).To(Equal("Nevermind (Remaster)"))
			Expect(got.Monitored).To(BeFalse())
			Expect(got.Status).To(Equal(album.StatusAvailable))
			bleach := client.Album.Query().Where(album.MbidEQ("rg-3")).OnlyX(ctx)
			Expect(bleach.Monitored).To(BeTrue())
			Expect(client.Artist.GetX(ctx, a.ID).LastRefreshedAt).NotTo(BeNil())
		},
	)

	It("cascades the delete to albums and tracks", func() {
		a := seed()
		Expect(store.DeleteArtist(ctx, a.ID)).To(Succeed())
		Expect(client.Album.Query().Count(ctx)).To(BeZero())
		Expect(client.Track.Query().Count(ctx)).To(BeZero())
	})
})
