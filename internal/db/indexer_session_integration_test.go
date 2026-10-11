package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("IndexerSession store", Label("integration", "db"), func() {
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

	It("puts, replaces and drops a session", func() {
		got, err := store.IndexerSession(ctx, "city")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeNil())

		exp := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
		Expect(store.PutIndexerSession(ctx, "city", IndexerSessionParams{
			Cookies: map[string]string{"uid": "1"}, ExpiresAt: &exp,
		})).To(Succeed())
		got, err = store.IndexerSession(ctx, "city")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Cookies).To(Equal(map[string]string{"uid": "1"}))
		Expect(got.ExpiresAt).To(HaveValue(BeTemporally("==", exp)))

		// A put replaces the row whole: the expiry it leaves out is cleared.
		retry := exp.Add(time.Minute)
		Expect(store.PutIndexerSession(ctx, "city", IndexerSessionParams{
			LoginFailures: 2, RetryAt: &retry, LastError: "bad password",
		})).To(Succeed())
		got, err = store.IndexerSession(ctx, "city")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.ExpiresAt).To(BeNil())
		Expect(got.Cookies).To(BeEmpty())
		Expect(got.LoginFailures).To(Equal(uint8(2)))
		Expect(got.LastError).To(Equal("bad password"))

		Expect(store.DeleteIndexerSession(ctx, "city")).To(Succeed())
		got, err = store.IndexerSession(ctx, "city")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeNil())
	})
})
