package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
)

var _ = Describe("ImportScanFile store", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
		scanID uint32
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
		s, err := store.CreateImportScan(
			ctx,
			CreateImportScanParams{
				SourcePath: "/x",
				Mode:       entimportscan.ModeInPlace,
			},
		)
		Expect(err).NotTo(HaveOccurred())
		scanID = s.ID
	})

	Describe("FilterImportScanFiles", func() {
		BeforeEach(func() {
			Expect(
				store.BulkCreateImportScanFiles(
					ctx,
					scanID,
					[]CreateImportScanFileParams{
						{
							SourcePath:     "/x/A.mkv",
							Size:           1,
							Classification: entimportscanfile.ClassificationConfirmed,
							ParsedTitle:    "Alpha",
						},
						{
							SourcePath:     "/x/B.mkv",
							Size:           1,
							Classification: entimportscanfile.ClassificationAmbiguous,
							ParsedTitle:    "Beta",
						},
						{
							SourcePath:     "/x/C.mkv",
							Size:           1,
							Classification: entimportscanfile.ClassificationUnmatched,
							ParsedTitle:    "Gamma",
						},
					},
				),
			).To(Succeed())
		})

		It("returns all when no filter", func() {
			items, total, err := store.FilterImportScanFiles(
				ctx,
				FilterImportScanFilesParams{ScanID: scanID, Limit: 50},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(total).To(Equal(uint32(3)))
			Expect(items).To(HaveLen(3))
		})

		It("filters by classification", func() {
			_, total, err := store.FilterImportScanFiles(
				ctx,
				FilterImportScanFilesParams{
					ScanID:         scanID,
					Classification: entimportscanfile.ClassificationConfirmed,
					Limit:          50,
				},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(total).To(Equal(uint32(1)))
		})

		It("filters by filename query (case-insensitive substring)", func() {
			_, total, err := store.FilterImportScanFiles(
				ctx,
				FilterImportScanFilesParams{
					ScanID: scanID, Query: "b.mkv", Limit: 50,
				},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(total).To(Equal(uint32(1)))
		})
	})

	Describe("ListImportScanFilesForCommit", func() {
		It("commits accepted + auto-matched files, never skipped ones", func() {
			Expect(
				store.BulkCreateImportScanFiles(
					ctx,
					scanID,
					[]CreateImportScanFileParams{
						{
							SourcePath:     "/x/confirmed-pending.mkv",
							Size:           1,
							Classification: entimportscanfile.ClassificationConfirmed,
						},
						{
							SourcePath:     "/x/confirmed-skip.mkv",
							Size:           1,
							Classification: entimportscanfile.ClassificationConfirmed,
						},
						{
							SourcePath:     "/x/existing-pending.mkv",
							Size:           1,
							Classification: entimportscanfile.ClassificationExisting,
						},
						{
							SourcePath:     "/x/unmatched-pending.mkv",
							Size:           1,
							Classification: entimportscanfile.ClassificationUnmatched,
						},
						{
							SourcePath:     "/x/unmatched-accept.mkv",
							Size:           1,
							Classification: entimportscanfile.ClassificationUnmatched,
						},
						{
							SourcePath:     "/x/ambiguous-skip.mkv",
							Size:           1,
							Classification: entimportscanfile.ClassificationAmbiguous,
						},
					},
				),
			).To(Succeed())
			items, _, err := store.FilterImportScanFiles(
				ctx,
				FilterImportScanFilesParams{ScanID: scanID, Limit: 50},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(items).To(HaveLen(6))

			id := map[string]uint32{}
			for _, f := range items {
				id[f.SourcePath] = f.ID
			}
			decide := func(path string, d entimportscanfile.Decision) {
				Expect(
					store.UpdateImportScanFileDecision(ctx, id[path], d, nil),
				).To(Succeed())
			}
			decide("/x/confirmed-skip.mkv", entimportscanfile.DecisionSkip)
			decide("/x/unmatched-accept.mkv", entimportscanfile.DecisionAccept)
			decide("/x/ambiguous-skip.mkv", entimportscanfile.DecisionSkip)

			got, err := store.ListImportScanFilesForCommit(ctx, scanID)
			Expect(err).ToNot(HaveOccurred())
			paths := make([]string, len(got))
			for i, f := range got {
				paths[i] = f.SourcePath
			}
			Expect(paths).To(ConsistOf(
				"/x/confirmed-pending.mkv",
				"/x/existing-pending.mkv",
				"/x/unmatched-accept.mkv",
			))
		})
	})
})
