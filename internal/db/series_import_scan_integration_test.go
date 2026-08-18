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
		Expect(total).To(Equal(1))
		Expect(shows).To(HaveLen(1))
		Expect(shows[0].FolderPath).To(Equal("/tv/Breaking Bad"))
		Expect(shows[0].FileCount).To(Equal(uint16(62)))
	})

	It("reports a not-found when deciding an unknown show id", func() {
		scan, err := store.CreateImportScan(ctx, CreateImportScanParams{
			SourcePath: "/tv",
			Mode:       entimportscan.ModeInPlace,
			Kind:       entimportscan.KindSeries,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(store.UpdateImportScanShowDecision(
			ctx, scan.ID, 999999, entimportscanshow.DecisionSkip, nil,
		)).To(MatchError(ErrImportScanShowNotFound))
	})

	It("records the decision for a show under the addressed scan", func() {
		scan, err := store.CreateImportScan(ctx, CreateImportScanParams{
			SourcePath: "/tv",
			Mode:       entimportscan.ModeInPlace,
			Kind:       entimportscan.KindSeries,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(store.BulkCreateImportScanShows(
			ctx,
			scan.ID,
			[]CreateImportScanShowParams{{
				FolderPath:     "/tv/Own Show",
				ParsedTitle:    "Own Show",
				Classification: entimportscanshow.ClassificationUnmatched,
				FileCount:      1,
			}},
		)).To(Succeed())
		shows, _, err := store.ListImportScanShows(
			ctx,
			ListImportScanShowsParams{ScanID: scan.ID},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(shows).To(HaveLen(1))

		tvdbID := uint32(81189)
		Expect(store.UpdateImportScanShowDecision(
			ctx, scan.ID, shows[0].ID, entimportscanshow.DecisionAccept, &tvdbID,
		)).To(Succeed())

		row, err := store.FindImportScanShow(ctx, scan.ID, shows[0].ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(row.Decision).To(Equal(entimportscanshow.DecisionAccept))
		Expect(row.DecisionTvdbID).To(HaveValue(Equal(tvdbID)))
	})

	// A decision addressed to scan A carrying a show id owned by scan B used to
	// mutate B's row and only then 404 on the scoped read-back: the client saw a
	// rejection while the foreign write had already landed.
	It("leaves another scan's show untouched", func() {
		addressed, err := store.CreateImportScan(ctx, CreateImportScanParams{
			SourcePath: "/tv",
			Mode:       entimportscan.ModeInPlace,
			Kind:       entimportscan.KindSeries,
		})
		Expect(err).NotTo(HaveOccurred())
		other, err := store.CreateImportScan(ctx, CreateImportScanParams{
			SourcePath: "/tv2",
			Mode:       entimportscan.ModeInPlace,
			Kind:       entimportscan.KindSeries,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(store.BulkCreateImportScanShows(
			ctx,
			other.ID,
			[]CreateImportScanShowParams{{
				FolderPath:     "/tv2/Foreign Show",
				ParsedTitle:    "Foreign Show",
				Classification: entimportscanshow.ClassificationUnmatched,
				FileCount:      1,
			}},
		)).To(Succeed())
		foreign, _, err := store.ListImportScanShows(
			ctx,
			ListImportScanShowsParams{ScanID: other.ID},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(foreign).To(HaveLen(1))
		Expect(foreign[0].Decision).To(Equal(entimportscanshow.DecisionPending))

		tvdbID := uint32(81189)
		err = store.UpdateImportScanShowDecision(
			ctx, addressed.ID, foreign[0].ID,
			entimportscanshow.DecisionAccept, &tvdbID,
		)
		Expect(err).To(MatchError(ErrImportScanShowNotFound))

		row, err := store.FindImportScanShow(ctx, other.ID, foreign[0].ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(row.Decision).To(Equal(entimportscanshow.DecisionPending))
		Expect(row.DecisionTvdbID).To(BeNil())
	})

	// The REST 404 for a (scan, show) pair that does not exist rides on
	// ent.IsNotFound seeing through this method's error wrapping, so a
	// %w → %v slip here would silently turn that answer into a 500.
	It("reports an ent not-found through FindImportScanShow's wrapping", func() {
		scan, err := store.CreateImportScan(ctx, CreateImportScanParams{
			SourcePath: "/tv",
			Mode:       entimportscan.ModeInPlace,
			Kind:       entimportscan.KindSeries,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = store.FindImportScanShow(ctx, scan.ID, 999999)
		Expect(ent.IsNotFound(err)).To(BeTrue())
	})

	It("defaults kind to movie when unset", func() {
		scan, err := store.CreateImportScan(ctx, CreateImportScanParams{
			SourcePath: "/movies", Mode: entimportscan.ModeInPlace,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(scan.Kind).To(Equal(entimportscan.KindMovie))
	})
})
