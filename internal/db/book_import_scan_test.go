package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanbook "github.com/datahearth/streamline/ent/importscanbook"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/schema"
)

var _ = Describe("ImportScanBook store", Label("integration", "db"), func() {
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

	It("round-trips file_paths and candidates", func() {
		scan, err := store.CreateImportScan(ctx, CreateImportScanParams{
			SourcePath: "/books",
			Kind:       entimportscan.KindBook,
			Mode:       entimportscan.ModeInPlace,
		})
		Expect(err).NotTo(HaveOccurred())
		existing := uint32(7)
		Expect(
			store.BulkCreateImportScanBooks(
				ctx,
				scan.ID,
				[]CreateImportScanBookParams{{
					FilePaths:       []string{"/books/a.epub", "/books/a.mobi"},
					Slot:            entimportscanbook.SlotEbook,
					ParsedTitle:     "Elantris",
					ParsedAuthor:    "Brandon Sanderson",
					ParsedISBN:      "9780765311771",
					Classification:  entimportscanbook.ClassificationAmbiguous,
					BookHardcoverID: 1,
					Candidates: []schema.ScannedBookCandidate{
						{
							BookHardcoverID: 1,
							Title:           "Elantris",
							Author:          "Sanderson",
							Year:            2005,
						},
					},
					ExistingBookID: &existing,
				}},
			),
		).To(Succeed())

		rows := client.ImportScanBook.Query().AllX(ctx)
		Expect(rows).To(HaveLen(1))
		Expect(
			rows[0].FilePaths,
		).To(Equal([]string{"/books/a.epub", "/books/a.mobi"}))
		Expect(rows[0].Candidates).To(HaveLen(1))
		Expect(rows[0].Candidates[0].Year).To(Equal(uint16(2005)))
		Expect(rows[0].Slot).To(Equal(entimportscanbook.SlotEbook))
		Expect(*rows[0].ExistingBookID).To(Equal(uint32(7)))
	})

	Describe("BookHardcoverIndex", func() {
		It("includes a book for the slot it has a file in, not the other", func() {
			b, err := store.CreateBook(ctx, BookSeed{
				HardcoverID: 1, Title: "Elantris", Kind: "novel",
				PreferredLanguage: "en",
			})
			Expect(err).NotTo(HaveOccurred())
			client.MediaFile.Create().
				SetPath("/books/a.epub").
				SetSize(1).SetQuality("epub").SetFormat("epub").
				SetBook(b).SetBookKind(entmediafile.BookKindEbook).
				SaveX(ctx)

			ebook, err := store.BookHardcoverIndex(ctx, "ebook")
			Expect(err).NotTo(HaveOccurred())
			Expect(ebook).To(HaveKeyWithValue(uint32(1), b.ID))
			audio, err := store.BookHardcoverIndex(ctx, "audiobook")
			Expect(err).NotTo(HaveOccurred())
			Expect(audio).To(BeEmpty())
		})
	})
})

var _ = Describe("ImportScanBook listing", Label("integration", "db"), func() {
	var (
		ctx   context.Context
		store *DB
	)

	BeforeEach(func() {
		ctx = context.Background()
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
	})

	newScan := func() *ent.ImportScan {
		GinkgoHelper()
		scan, err := store.CreateImportScan(ctx, CreateImportScanParams{
			SourcePath: "/books",
			Kind:       entimportscan.KindBook,
			Mode:       entimportscan.ModeInPlace,
		})
		Expect(err).NotTo(HaveOccurred())
		return scan
	}

	seed := func(scanID uint32, books ...CreateImportScanBookParams) {
		GinkgoHelper()
		Expect(store.BulkCreateImportScanBooks(ctx, scanID, books)).To(Succeed())
	}

	book := func(title, author string, cls entimportscanbook.Classification) CreateImportScanBookParams {
		return CreateImportScanBookParams{
			FilePaths:      []string{"/books/" + title + ".epub"},
			Slot:           entimportscanbook.SlotEbook,
			ParsedTitle:    title,
			ParsedAuthor:   author,
			Classification: cls,
		}
	}

	It("filters by classification and query and reports the total", func() {
		scan := newScan()
		seed(
			scan.ID,
			book(
				"Elantris",
				"Brandon Sanderson",
				entimportscanbook.ClassificationConfirmed,
			),
			book(
				"Mistborn",
				"Brandon Sanderson",
				entimportscanbook.ClassificationAmbiguous,
			),
			book("Dune", "Frank Herbert", entimportscanbook.ClassificationUnmatched),
		)

		rows, total, err := store.ListImportScanBooks(
			ctx,
			ListImportScanBooksParams{ScanID: scan.ID},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(Equal(uint32(3)))
		Expect(rows[0].ParsedTitle).To(Equal("Dune"))

		_, total, err = store.ListImportScanBooks(ctx, ListImportScanBooksParams{
			ScanID:         scan.ID,
			Classification: entimportscanbook.ClassificationAmbiguous,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(Equal(uint32(1)))

		_, total, err = store.ListImportScanBooks(ctx, ListImportScanBooksParams{
			ScanID: scan.ID, Query: "SANDERSON",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(Equal(uint32(2)))

		_, total, err = store.ListImportScanBooks(ctx, ListImportScanBooksParams{
			ScanID: scan.ID, Query: "dune",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(Equal(uint32(1)))
	})

	It("paginates while keeping the full total", func() {
		scan := newScan()
		seed(scan.ID,
			book("A", "x", entimportscanbook.ClassificationConfirmed),
			book("B", "x", entimportscanbook.ClassificationConfirmed),
			book("C", "x", entimportscanbook.ClassificationConfirmed),
		)
		rows, total, err := store.ListImportScanBooks(ctx, ListImportScanBooksParams{
			ScanID: scan.ID, Offset: 1, Limit: 1,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(Equal(uint32(3)))
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].ParsedTitle).To(Equal("B"))
	})

	It("scopes list, find and decision update to the scan", func() {
		scanA, scanB := newScan(), newScan()
		seed(scanA.ID, book("A", "x", entimportscanbook.ClassificationAmbiguous))
		seed(scanB.ID, book("B", "x", entimportscanbook.ClassificationAmbiguous))
		inA, _, err := store.ListImportScanBooks(
			ctx,
			ListImportScanBooksParams{ScanID: scanA.ID},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(inA).To(HaveLen(1))
		inB, _, err := store.ListImportScanBooks(
			ctx,
			ListImportScanBooksParams{ScanID: scanB.ID},
		)
		Expect(err).NotTo(HaveOccurred())

		_, err = store.FindImportScanBook(ctx, scanA.ID, inB[0].ID)
		Expect(err).To(HaveOccurred())

		err = store.UpdateImportScanBookDecision(
			ctx, scanA.ID, inB[0].ID, entimportscanbook.DecisionAccept, nil,
		)
		Expect(err).To(MatchError(ErrImportScanBookNotFound))
		unchanged, err := store.FindImportScanBook(ctx, scanB.ID, inB[0].ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(unchanged.Decision).To(Equal(entimportscanbook.DecisionPending))
	})

	It("records a decision with a Hardcover id and clears it on nil", func() {
		scan := newScan()
		seed(scan.ID, book("A", "x", entimportscanbook.ClassificationAmbiguous))
		rows, _, err := store.ListImportScanBooks(
			ctx,
			ListImportScanBooksParams{ScanID: scan.ID},
		)
		Expect(err).NotTo(HaveOccurred())
		id := rows[0].ID

		hc := uint32(42)
		Expect(store.UpdateImportScanBookDecision(
			ctx, scan.ID, id, entimportscanbook.DecisionAccept, &hc,
		)).To(Succeed())
		got, err := store.FindImportScanBook(ctx, scan.ID, id)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Decision).To(Equal(entimportscanbook.DecisionAccept))
		Expect(got.DecisionBookHardcoverID).To(Equal(uint32(42)))

		Expect(store.UpdateImportScanBookDecision(
			ctx, scan.ID, id, entimportscanbook.DecisionSkip, nil,
		)).To(Succeed())
		got, err = store.FindImportScanBook(ctx, scan.ID, id)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.DecisionBookHardcoverID).To(BeZero())
	})
})
