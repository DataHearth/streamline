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

	Describe("UpdateImportScanFileDecision", func() {
		It("reports a not-found for an unknown file id", func() {
			err := store.UpdateImportScanFileDecision(
				ctx, scanID, 999999, entimportscanfile.DecisionSkip, nil,
			)
			Expect(err).To(MatchError(ErrImportScanFileNotFound))
		})

		It("records the decision for a file under the addressed scan", func() {
			Expect(store.BulkCreateImportScanFiles(
				ctx,
				scanID,
				[]CreateImportScanFileParams{{
					SourcePath:     "/x/own.mkv",
					Size:           1,
					Classification: entimportscanfile.ClassificationUnmatched,
				}},
			)).To(Succeed())
			files, _, err := store.FilterImportScanFiles(
				ctx,
				FilterImportScanFilesParams{ScanID: scanID, Limit: 50},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(files).To(HaveLen(1))

			tmdbID := uint32(27205)
			Expect(store.UpdateImportScanFileDecision(
				ctx, scanID, files[0].ID, entimportscanfile.DecisionAccept, &tmdbID,
			)).To(Succeed())

			row, err := store.FindImportScanFile(ctx, scanID, files[0].ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(row.Decision).To(Equal(entimportscanfile.DecisionAccept))
			Expect(row.DecisionTmdbID).To(Equal(tmdbID))
		})

		// A decision addressed to scan A carrying a file id owned by scan B used
		// to mutate B's row and only then 404 on the scoped read-back: the client
		// saw a rejection while the foreign write had already landed.
		It("leaves another scan's file untouched", func() {
			other, err := store.CreateImportScan(ctx, CreateImportScanParams{
				SourcePath: "/y",
				Mode:       entimportscan.ModeInPlace,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(store.BulkCreateImportScanFiles(
				ctx,
				other.ID,
				[]CreateImportScanFileParams{{
					SourcePath:     "/y/foreign.mkv",
					Size:           1,
					Classification: entimportscanfile.ClassificationUnmatched,
				}},
			)).To(Succeed())
			foreign, _, err := store.FilterImportScanFiles(
				ctx,
				FilterImportScanFilesParams{ScanID: other.ID, Limit: 50},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(foreign).To(HaveLen(1))
			Expect(foreign[0].Decision).
				To(Equal(entimportscanfile.DecisionPending))

			tmdbID := uint32(27205)
			err = store.UpdateImportScanFileDecision(
				ctx,
				scanID,
				foreign[0].ID,
				entimportscanfile.DecisionAccept,
				&tmdbID,
			)
			Expect(err).To(MatchError(ErrImportScanFileNotFound))

			row, err := store.FindImportScanFile(ctx, other.ID, foreign[0].ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(row.Decision).To(Equal(entimportscanfile.DecisionPending))
			Expect(row.DecisionTmdbID).To(BeZero())
		})
	})

	Describe("FindImportScanFile", func() {
		// The REST 404 for a (scan, file) pair that does not exist rides on
		// ent.IsNotFound seeing through this method's error wrapping, so a
		// %w → %v slip here would silently turn that answer into a 500.
		It("reports an ent not-found through its error wrapping", func() {
			_, err := store.FindImportScanFile(ctx, scanID, 999999)
			Expect(ent.IsNotFound(err)).To(BeTrue())
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
					store.UpdateImportScanFileDecision(
						ctx,
						scanID,
						id[path],
						d,
						nil,
					),
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
