package bulkimport

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanbook "github.com/datahearth/streamline/ent/importscanbook"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

const testOPF = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf" version="2.0">
  <metadata>
    <dc:title>Elantris</dc:title>
    <dc:creator opf:role="aut">Brandon Sanderson</dc:creator>
    <dc:identifier opf:scheme="ISBN">9780765311771</dc:identifier>
    <dc:date>2005-04-21</dc:date>
  </metadata>
</package>`

const bareOPF = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0"><metadata/></package>`

const containerXML = `<?xml version="1.0"?>` +
	`<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0">` +
	`<rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles>` +
	`</container>`

func writeEpub(path, opf string) {
	GinkgoHelper()
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	f, err := os.Create(path)
	Expect(err).NotTo(HaveOccurred())
	zw := zip.NewWriter(f)
	w, err := zw.Create("META-INF/container.xml")
	Expect(err).NotTo(HaveOccurred())
	_, err = w.Write([]byte(containerXML))
	Expect(err).NotTo(HaveOccurred())
	w, err = zw.Create("content.opf")
	Expect(err).NotTo(HaveOccurred())
	_, err = w.Write([]byte(opf))
	Expect(err).NotTo(HaveOccurred())
	Expect(zw.Close()).To(Succeed())
	Expect(f.Close()).To(Succeed())
}

func touch(path string) {
	GinkgoHelper()
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	Expect(os.WriteFile(path, nil, 0o644)).To(Succeed())
}

var _ = Describe("Book scan", Label("unit", "bulkimport"), func() {
	var (
		ctx       context.Context
		client    *ent.Client
		store     db.Store
		bookmeta  *metamocks.MockBookProvider
		svc       *Service
		ebookRoot string
		audioRoot string
	)

	BeforeEach(func() {
		ctx = context.Background()
		ebookRoot = GinkgoT().TempDir()
		audioRoot = GinkgoT().TempDir()
		configtest.Setup(map[string]any{
			"library": map[string]any{
				"ebook_path":     ebookRoot,
				"audiobook_path": audioRoot,
			},
		})
		client = dbtest.SetupTestDB(ctx)
		DeferCleanup(client.Close)
		store = db.New(client)
		bookmeta = metamocks.NewMockBookProvider(GinkgoT())
		svc = NewService(
			store,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			"/m",
			"/s",
			nil,
			nil,
			bookmeta,
			nil,
		)
	})

	scan := func(root string) []*ent.ImportScanBook {
		GinkgoHelper()
		sc, err := svc.StartScan(ctx, StartScanParams{
			SourcePath: root,
			Kind:       entimportscan.KindBook,
			Mode:       entimportscan.ModeInPlace,
		})
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() entimportscan.Status {
			cur, ferr := store.FindImportScan(ctx, sc.ID)
			Expect(ferr).NotTo(HaveOccurred())
			return cur.Status
		}, 5*time.Second, 20*time.Millisecond).Should(Equal(entimportscan.StatusAwaitingReview))
		return client.ImportScanBook.Query().
			Order(ent.Asc("parsed_title")).AllX(ctx)
	}

	Describe("ebooks", func() {
		BeforeEach(func() {
			dir := filepath.Join(ebookRoot, "Brandon Sanderson", "Elantris (1)")
			writeEpub(
				filepath.Join(dir, "Elantris - Brandon Sanderson.epub"),
				strings.Replace(
					testOPF,
					"<dc:title>Elantris",
					"<dc:title>Wrong Embedded",
					1,
				),
			)
			touch(filepath.Join(dir, "Elantris - Brandon Sanderson.mobi"))
			Expect(
				os.WriteFile(
					filepath.Join(dir, "metadata.opf"),
					[]byte(testOPF),
					0o644,
				),
			).To(Succeed())
			writeEpub(
				filepath.Join(ebookRoot, "loose", "Some Unknown Book.epub"),
				bareOPF,
			)
		})

		var rows []*ent.ImportScanBook

		BeforeEach(func() {
			bookmeta.EXPECT().BookByISBN(mock.Anything, "9780765311771").
				Return(uint32(1), nil).Once()
			bookmeta.EXPECT().SearchBooks(mock.Anything, "Some Unknown Book loose").
				Return(nil, nil).Once()
			rows = scan(ebookRoot)
		})

		It(
			"groups a Calibre folder into one row and confirms it by ISBN without searching",
			func() {
				Expect(rows).To(HaveLen(2))
				var elantris *ent.ImportScanBook
				for _, r := range rows {
					if r.ParsedTitle == "Elantris" {
						elantris = r
					}
				}
				Expect(elantris).NotTo(BeNil())
				Expect(elantris.FilePaths).To(HaveLen(2))
				Expect(elantris.Slot).To(Equal(entimportscanbook.SlotEbook))
				Expect(elantris.ParsedAuthor).To(Equal("Brandon Sanderson"))
				Expect(elantris.ParsedIsbn).To(Equal("9780765311771"))
				Expect(
					elantris.Classification,
				).To(Equal(entimportscanbook.ClassificationConfirmed))
				Expect(elantris.BookHardcoverID).To(Equal(uint32(1)))
				Expect(elantris.Candidates).To(HaveLen(1))
				Expect(elantris.Candidates[0].Title).To(BeEmpty())
			},
		)

		It("gives a bare loose epub its own unmatched candidate", func() {
			var loose *ent.ImportScanBook
			for _, r := range rows {
				if r.ParsedTitle == "Some Unknown Book" {
					loose = r
				}
			}
			Expect(loose).NotTo(BeNil())
			Expect(loose.FilePaths).To(HaveLen(1))
			Expect(
				loose.Classification,
			).To(Equal(entimportscanbook.ClassificationUnmatched))
		})
	})

	It("flags a hit already held in the slot as existing", func() {
		now := time.Now()
		held, err := store.CreateBook(ctx, db.BookSeed{
			HardcoverID: 1, Title: "Elantris", AuthorName: "Brandon Sanderson",
			Kind: "novel", PreferredLanguage: "en", RefreshedAt: &now,
			Credits: []db.CreditSeed{{
				AuthorHardcoverID: 10, Name: "Brandon Sanderson", Role: "author",
			}},
		})
		Expect(err).NotTo(HaveOccurred())
		client.MediaFile.Create().
			SetPath("/elsewhere/elantris.epub").SetSize(1).
			SetQuality("epub").SetFormat("epub").
			SetBook(held).SetBookKind(entmediafile.BookKindEbook).
			SaveX(ctx)

		touch(filepath.Join(ebookRoot, "Elantris - Brandon Sanderson.mobi"))
		bookmeta.EXPECT().SearchBooks(mock.Anything, mock.Anything).
			Return([]metadata.BookSearchResult{
				{HardcoverID: 1, Title: "Elantris", Author: "Brandon Sanderson"},
				{HardcoverID: 2, Title: "Elantris Reissue", Author: "Other Person"},
			}, nil).Once()

		rows := scan(ebookRoot)
		Expect(rows).To(HaveLen(1))
		Expect(
			rows[0].Classification,
		).To(Equal(entimportscanbook.ClassificationExisting))
		Expect(*rows[0].ExistingBookID).To(Equal(held.ID))
	})

	It("turns an audiobook folder into one candidate named by its path", func() {
		dir := filepath.Join(audioRoot, "Brandon Sanderson", "Elantris")
		touch(filepath.Join(dir, "Part 02.mp3"))
		touch(filepath.Join(dir, "Part 01.mp3"))
		bookmeta.EXPECT().SearchBooks(mock.Anything, "Elantris Brandon Sanderson").
			Return(nil, nil).Once()

		rows := scan(audioRoot)
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].Slot).To(Equal(entimportscanbook.SlotAudiobook))
		Expect(rows[0].FilePaths).To(Equal([]string{
			filepath.Join(dir, "Part 01.mp3"),
			filepath.Join(dir, "Part 02.mp3"),
		}))
		Expect(rows[0].ParsedTitle).To(Equal("Elantris"))
		Expect(rows[0].ParsedAuthor).To(Equal("Brandon Sanderson"))
	})

	It("fails the scan when the source root cannot be walked", func() {
		sc, err := store.CreateImportScan(ctx, db.CreateImportScanParams{
			SourcePath: filepath.Join(ebookRoot, "missing"),
			Kind:       entimportscan.KindBook,
			Mode:       entimportscan.ModeInPlace,
		})
		Expect(err).NotTo(HaveOccurred())

		svc.runScanBooks(ctx, sc)

		cur, err := store.FindImportScan(ctx, sc.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(cur.Status).To(Equal(entimportscan.StatusFailed))
		Expect(cur.FailureReason).To(ContainSubstring("walk source path"))
	})

	It("never searches by title once the ISBN resolves", func() {
		dir := filepath.Join(ebookRoot, "Brandon Sanderson", "Elantris (1)")
		writeEpub(filepath.Join(dir, "Elantris - Brandon Sanderson.epub"), testOPF)
		bookmeta.EXPECT().BookByISBN(mock.Anything, "9780765311771").
			Return(uint32(1), nil).Once()

		rows := scan(ebookRoot)
		Expect(rows).To(HaveLen(1))
		bookmeta.AssertNotCalled(
			GinkgoT(),
			"SearchBooks",
			mock.Anything,
			mock.Anything,
		)
	})

	Describe("Hardcover budget", func() {
		var budget *metamocks.MockBudgeter

		failedScan := func(root string) *ent.ImportScan {
			GinkgoHelper()
			sc, err := store.CreateImportScan(ctx, db.CreateImportScanParams{
				SourcePath: root,
				Kind:       entimportscan.KindBook,
				Mode:       entimportscan.ModeInPlace,
			})
			Expect(err).NotTo(HaveOccurred())
			svc.runScanBooks(ctx, sc)
			cur, err := store.FindImportScan(ctx, sc.ID)
			Expect(err).NotTo(HaveOccurred())
			return cur
		}

		BeforeEach(func() {
			budget = metamocks.NewMockBudgeter(GinkgoT())
			budget.EXPECT().ScanReserve().Return(500).Maybe()
			svc.bookmeta = struct {
				metadata.BookProvider
				metadata.Budgeter
			}{bookmeta, budget}
			touch(filepath.Join(ebookRoot, "Elantris - Brandon Sanderson.epub"))
		})

		It(
			"fails a re-runnable scan before looking anything up when only the reserve is left",
			func() {
				budget.EXPECT().Remaining().Return(500).Once()

				cur := failedScan(ebookRoot)
				Expect(cur.Status).To(Equal(entimportscan.StatusFailed))
				Expect(
					cur.FailureReason,
				).To(ContainSubstring("daily request budget"))
				Expect(cur.FailureReason).To(ContainSubstring("resets"))
				Expect(client.ImportScanBook.Query().CountX(ctx)).To(BeZero())
			},
		)

		It("scans while the budget clears the reserve", func() {
			budget.EXPECT().Remaining().Return(501).Once()
			bookmeta.EXPECT().SearchBooks(mock.Anything, mock.Anything).
				Return(nil, nil).Once()

			cur := failedScan(ebookRoot)
			Expect(cur.Status).To(Equal(entimportscan.StatusAwaitingReview))
		})

		It(
			"fails the scan with the reset time when a lookup is rate limited",
			func() {
				budget.EXPECT().Remaining().Return(4000).Once()
				bookmeta.EXPECT().SearchBooks(mock.Anything, mock.Anything).
					Return(nil, &metadata.RateLimitedError{RetryAfter: 90 * time.Second}).
					Once()

				cur := failedScan(ebookRoot)
				Expect(cur.Status).To(Equal(entimportscan.StatusFailed))
				Expect(cur.FailureReason).To(ContainSubstring("rate limit"))
				Expect(cur.FailureReason).To(ContainSubstring("Re-run the scan"))
				Expect(client.ImportScanBook.Query().CountX(ctx)).To(BeZero())
			},
		)
	})

	Describe("StartScan dispatch", func() {
		It("rejects a kind with no scanner before writing a scan row", func() {
			_, err := svc.StartScan(ctx, StartScanParams{
				SourcePath: ebookRoot,
				Kind:       entimportscan.Kind("bogus"),
				Mode:       entimportscan.ModeInPlace,
			})
			Expect(err).To(MatchError(ErrUnsupportedKind))
			Expect(client.ImportScan.Query().CountX(ctx)).To(BeZero())
		})

		It("refuses a book scan when Hardcover is not configured", func() {
			svc = NewService(
				store,
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				"/m",
				"/s",
				nil,
				nil,
				nil,
				nil,
			)
			_, err := svc.StartScan(ctx, StartScanParams{
				SourcePath: ebookRoot,
				Kind:       entimportscan.KindBook,
				Mode:       entimportscan.ModeInPlace,
			})
			Expect(err).To(MatchError(book.ErrNotConfigured))
			Expect(client.ImportScan.Query().CountX(ctx)).To(BeZero())
		})
	})
})
