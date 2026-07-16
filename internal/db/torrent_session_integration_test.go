package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TorrentSession store", Label("integration", "db"), func() {
	var (
		ctx   context.Context
		store *DB
	)

	BeforeEach(func() {
		ctx = context.Background()
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
		store = New(client)
	})

	It("round-trips a session through its lifecycle", func() {
		created, err := store.CreateTorrentSession(ctx, CreateTorrentSessionParams{
			InfoHash:      "aabbccddeeff00112233445566778899aabbccdd",
			Name:          "Test.Release.2024",
			SavePath:      "/downloads",
			SourceTorrent: []byte("d4:infoe"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(created.Paused).To(BeFalse())

		list, err := store.ListTorrentSessions(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(HaveLen(1))

		Expect(store.SetTorrentSessionPaused(
			ctx, created.InfoHash, true,
		)).To(Succeed())
		Expect(store.SetTorrentSessionName(
			ctx, created.InfoHash, "Resolved Name",
		)).To(Succeed())
		now := time.Now()
		Expect(store.SetTorrentSessionCompleted(
			ctx, created.InfoHash, now,
		)).To(Succeed())
		Expect(store.SetTorrentSessionSeedStopped(
			ctx, created.InfoHash,
		)).To(Succeed())

		list, err = store.ListTorrentSessions(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(list[0].Paused).To(BeTrue())
		Expect(list[0].Name).To(Equal("Resolved Name"))
		Expect(list[0].CompletedAt).NotTo(BeNil())
		Expect(list[0].SeedStopped).To(BeTrue())

		Expect(store.DeleteTorrentSessionByHash(
			ctx, created.InfoHash,
		)).To(Succeed())
		list, err = store.ListTorrentSessions(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(BeEmpty())
	})

	It("rejects a duplicate info_hash", func() {
		p := CreateTorrentSessionParams{
			InfoHash: "aabbccddeeff00112233445566778899aabbccdd",
			SavePath: "/downloads", SourceMagnet: "magnet:?xt=urn:btih:x",
		}
		_, err := store.CreateTorrentSession(ctx, p)
		Expect(err).NotTo(HaveOccurred())
		_, err = store.CreateTorrentSession(ctx, p)
		Expect(err).To(HaveOccurred())
	})
})
