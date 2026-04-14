package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
)

var _ = Describe("Series import scan store", Label("integration", "db"), func() {
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

	It("creates a series scan, appends shows, and lists pending folders", func() {
		scan, err := store.CreateImportScan(ctx, CreateImportScanParams{
			SourcePath: "/tv",
			Mode:       entimportscan.ModeInPlace,
			Kind:       entimportscan.KindSeries,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(scan.Kind).To(Equal(entimportscan.KindSeries))
		Expect(store.UpdateImportScanStatus(
			ctx,
			scan.ID,
			entimportscan.StatusAwaitingReview,
			UpdateScanStatusOpts{},
		)).To(Succeed())

		Expect(
			store.BulkCreateImportScanShows(
				ctx,
				scan.ID,
				[]CreateImportScanShowParams{
					{
						FolderPath:     "/tv/Breaking Bad",
						ParsedTitle:    "Breaking Bad",
						Classification: entimportscanshow.ClassificationConfirmed,
						FileCount:      62,
					},
				},
			),
		).To(Succeed())

		folders, err := store.ListPendingImportScanShowFolders(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(folders).To(ConsistOf("/tv/Breaking Bad"))

		shows, total, err := store.ListImportScanShows(
			ctx,
			ListImportScanShowsParams{ScanID: scan.ID},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(Equal(uint32(1)))
		Expect(shows).To(HaveLen(1))
		Expect(shows[0].FolderPath).To(Equal("/tv/Breaking Bad"))
		Expect(shows[0].FileCount).To(Equal(uint16(62)))
	})

	It("defaults kind to movie when unset", func() {
		scan, err := store.CreateImportScan(ctx, CreateImportScanParams{
			SourcePath: "/movies", Mode: entimportscan.ModeInPlace,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(scan.Kind).To(Equal(entimportscan.KindMovie))
	})
})
