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
							BookHardcoverID:   1,
							AuthorHardcoverID: 2,
							Title:             "Elantris",
							Author:            "Sanderson",
							Year:              2005,
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
			a, err := store.CreateAuthor(ctx, CreateAuthorParams{
				HardcoverID:   10,
				Name:          "Sanderson",
				MonitorPolicy: "none",
				WantKinds:     "ebook",
				Books:         []BookSeed{{HardcoverID: 1, Title: "Elantris"}},
			})
			Expect(err).NotTo(HaveOccurred())
			b := a.Edges.Books[0]
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
