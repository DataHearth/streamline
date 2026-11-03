package db

import (
	"context"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Read pool", Label("integration", "db"), func() {
	It("serves reads while a transaction holds the writer", func() {
		ctx := context.Background()
		client, err := Open(ctx, filepath.Join(GinkgoT().TempDir(), "streamline.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(client.Close)

		tx, err := client.Tx(ctx)
		Expect(err).NotTo(HaveOccurred())
		_, err = tx.User.Create().
			SetEmail("pending@example.com").
			SetRole("admin").
			SetAuthMethod("local").
			Save(ctx)
		Expect(err).NotTo(HaveOccurred())

		By(
			"reading outside the transaction, which one shared connection would block",
		)
		readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		count, err := client.User.Query().Count(readCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(BeZero(), "an uncommitted row must not be visible")

		Expect(tx.Commit()).To(Succeed())

		By("reading again once the transaction committed")
		count, err = client.User.Query().Count(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(1))
	})
})
