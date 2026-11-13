package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TorrentDataRemoval store", Label("integration", "db"), func() {
	var (
		ctx   context.Context
		store *DB
	)
	owed := TorrentDataRemovalParams{
		InfoHash: "aabbccddeeff00112233445566778899aabbccdd",
		Name:     "Test.Release.2024",
		Dir:      "/downloads",
		Info:     []byte("d4:name17:Test.Release.2024e"),
	}

	BeforeEach(func() {
		ctx = context.Background()
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
		store = New(client)
	})

	It("keeps a removal until it is deleted", func() {
		Expect(store.KeepTorrentDataRemoval(ctx, owed)).To(Succeed())

		list, err := store.ListTorrentDataRemovals(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(HaveLen(1))
		Expect(list[0].InfoHash).To(Equal(owed.InfoHash))
		Expect(list[0].Name).To(Equal(owed.Name))
		Expect(list[0].Dir).To(Equal(owed.Dir))
		Expect(list[0].Info).To(Equal(owed.Info))

		Expect(store.DeleteTorrentDataRemoval(ctx, owed.InfoHash)).To(Succeed())
		Expect(store.ListTorrentDataRemovals(ctx)).To(BeEmpty())
	})

	It("keeps one row for a hash kept twice", func() {
		Expect(store.KeepTorrentDataRemoval(ctx, owed)).To(Succeed())
		Expect(store.KeepTorrentDataRemoval(ctx, owed)).To(Succeed())

		Expect(store.ListTorrentDataRemovals(ctx)).To(HaveLen(1))
	})

	It("deletes an unknown hash without error", func() {
		Expect(store.DeleteTorrentDataRemoval(ctx, "nope")).To(Succeed())
	})
})
