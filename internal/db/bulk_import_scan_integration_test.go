package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
)

var _ = Describe("ImportScan store", Label("integration", "db"), func() {
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

	Describe("CreateImportScan", func() {
		It("persists the row with default status=running", func() {
			s, err := store.CreateImportScan(ctx, CreateImportScanParams{
				SourcePath: "/tmp/test",
				Mode:       entimportscan.ModeInPlace,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(s.Status).To(Equal(entimportscan.StatusRunning))
			Expect(s.SourcePath).To(Equal("/tmp/test"))
		})
	})

	Describe("UpdateImportScanStatus", func() {
		It("updates status and optional opts", func() {
			s, err := store.CreateImportScan(
				ctx,
				CreateImportScanParams{
					SourcePath: "/x",
					Mode:       entimportscan.ModeInPlace,
				},
			)
			Expect(err).ToNot(HaveOccurred())
			total := uint32(42)
			now := time.Now()
			Expect(
				store.UpdateImportScanStatus(
					ctx,
					s.ID,
					entimportscan.StatusAwaitingReview,
					UpdateScanStatusOpts{
						TotalCount: &total,
						ScannedAt:  &now,
					},
				),
			).To(Succeed())
			got, err := store.FindImportScan(ctx, s.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Status).To(Equal(entimportscan.StatusAwaitingReview))
			Expect(got.TotalCount).To(Equal(uint32(42)))
			Expect(got.ScannedAt).ToNot(BeNil())
		})
	})

	Describe("CountActiveImportScans", func() {
		It("counts running and committing rows only", func() {
			running, err := store.CreateImportScan(
				ctx,
				CreateImportScanParams{
					SourcePath: "/a",
					Mode:       entimportscan.ModeInPlace,
				},
			)
			Expect(err).ToNot(HaveOccurred())
			_, err = store.CreateImportScan(
				ctx,
				CreateImportScanParams{
					SourcePath: "/b",
					Mode:       entimportscan.ModeInPlace,
				},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(
				store.UpdateImportScanStatus(
					ctx,
					running.ID,
					entimportscan.StatusCompleted,
					UpdateScanStatusOpts{},
				),
			).To(Succeed())

			n, err := store.CountActiveImportScans(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(n).To(Equal(uint32(1)))
		})
	})

	Describe("AbortInflightImportScans", func() {
		It("flips running and committing rows to failed", func() {
			r, err := store.CreateImportScan(
				ctx,
				CreateImportScanParams{
					SourcePath: "/r",
					Mode:       entimportscan.ModeInPlace,
				},
			)
			Expect(err).ToNot(HaveOccurred())
			c, err := store.CreateImportScan(
				ctx,
				CreateImportScanParams{
					SourcePath: "/c",
					Mode:       entimportscan.ModeInPlace,
				},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(
				store.UpdateImportScanStatus(
					ctx,
					c.ID,
					entimportscan.StatusCommitting,
					UpdateScanStatusOpts{},
				),
			).To(Succeed())

			n, err := store.AbortInflightImportScans(ctx, "boot")
			Expect(err).ToNot(HaveOccurred())
			Expect(n).To(Equal(uint32(2)))

			rgot, err := store.FindImportScan(ctx, r.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(rgot.Status).To(Equal(entimportscan.StatusFailed))
			Expect(rgot.FailureReason).To(Equal("boot"))
			cgot, err := store.FindImportScan(ctx, c.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(cgot.Status).To(Equal(entimportscan.StatusFailed))
		})

		It("returns 0 when no inflight rows", func() {
			n, err := store.AbortInflightImportScans(ctx, "boot")
			Expect(err).ToNot(HaveOccurred())
			Expect(n).To(Equal(uint32(0)))
		})
	})

	Describe("DeleteImportScan cascades files", func() {
		It("deletes the file rows when the parent is deleted", func() {
			s, err := store.CreateImportScan(
				ctx,
				CreateImportScanParams{
					SourcePath: "/a",
					Mode:       entimportscan.ModeInPlace,
				},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(
				store.BulkCreateImportScanFiles(
					ctx,
					s.ID,
					[]CreateImportScanFileParams{
						{
							SourcePath:     "/a/x.mkv",
							Size:           1,
							Classification: "unmatched",
						},
						{
							SourcePath:     "/a/y.mkv",
							Size:           1,
							Classification: "unmatched",
						},
					},
				),
			).To(Succeed())
			Expect(store.DeleteImportScan(ctx, s.ID)).To(Succeed())
			items, total, err := store.FilterImportScanFiles(
				ctx,
				FilterImportScanFilesParams{ScanID: s.ID, Limit: 50},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(total).To(Equal(uint32(0)))
			Expect(items).To(BeEmpty())
		})
	})
})
