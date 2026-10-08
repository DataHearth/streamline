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
			bookmeta.EXPECT().GetBook(mock.Anything, uint32(1)).
				Return(&metadata.BookDetails{
					HardcoverID:     1,
					Title:           "Elantris",
					AuthorHardcover: 10,
				}, nil).Once()
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
				Expect(elantris.AuthorHardcoverID).To(Equal(uint32(10)))
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
		a, err := store.CreateAuthor(ctx, db.CreateAuthorParams{
			HardcoverID: 10, Name: "Brandon Sanderson", MonitorPolicy: "none",
			WantKinds: "ebook",
			Books:     []db.BookSeed{{HardcoverID: 1, Title: "Elantris"}},
		})
		Expect(err).NotTo(HaveOccurred())
		held := a.Edges.Books[0]
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
		bookmeta.EXPECT().GetBook(mock.Anything, uint32(1)).
			Return(&metadata.BookDetails{AuthorHardcover: 10}, nil).Once()

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
