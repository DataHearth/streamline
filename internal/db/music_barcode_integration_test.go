package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Album barcode", Label("integration", "db"), func() {
	It("is stored from the seed and read back with the album", func() {
		ctx := context.Background()
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store := New(client)

		_, err = store.CreateArtist(ctx, CreateArtistParams{
			MBID: "artist-1",
			Name: "Nirvana",
			Albums: []AlbumSeed{
				{MBID: "rg-1", Title: "Nevermind", Barcode: "0720642442524"},
				{MBID: "rg-2", Title: "In Utero"},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		with, err := store.FindAlbumByMBID(ctx, "rg-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(with.Barcode).To(Equal("0720642442524"))

		without, err := store.FindAlbumByMBID(ctx, "rg-2")
		Expect(err).NotTo(HaveOccurred())
		Expect(without.Barcode).To(BeEmpty())
	})
})
