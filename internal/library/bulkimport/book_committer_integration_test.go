package bulkimport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entauthor "github.com/datahearth/streamline/ent/author"
	entbook "github.com/datahearth/streamline/ent/book"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanbook "github.com/datahearth/streamline/ent/importscanbook"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	postersmocks "github.com/datahearth/streamline/internal/posters/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe(
	"Book commit (adopt books)",
	Label("integration", "bulkimport"),
	func() {
		var (
			ctx      context.Context
			tmpDir   string
			client   *ent.Client
			store    db.Store
			bookmeta *metamocks.MockBookProvider
			svc      *Service
			scanID   uint32
		)

		BeforeEach(func() {
			ctx = context.Background()
			configtest.Setup(map[string]any{})
			tmpDir = GinkgoT().TempDir()
			client = dbtest.SetupTestDB(ctx)
			DeferCleanup(client.Close)
			store = db.New(client)
			bookmeta = metamocks.NewMockBookProvider(GinkgoT())
			bookSvc := book.NewService(
				store, bookmeta, postersmocks.NewMockManager(GinkgoT()), nil, nil,
			)
			svc = NewService(
				store,
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				tmpDir,
				tmpDir,
				nil,
				nil,
				bookmeta,
				bookSvc,
			)

			scan, err := store.CreateImportScan(ctx, db.CreateImportScanParams{
				SourcePath: tmpDir,
				Kind:       entimportscan.KindBook,
				Mode:       entimportscan.ModeInPlace,
			})
			Expect(err).NotTo(HaveOccurred())
			scanID = scan.ID
			Expect(store.UpdateImportScanStatus(
				ctx,
				scanID,
				entimportscan.StatusAwaitingReview,
				db.UpdateScanStatusOpts{},
			)).To(Succeed())
		})

		writeBookFile := func(name string) string {
			GinkgoHelper()
			p := filepath.Join(tmpDir, name)
			Expect(os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
			Expect(os.WriteFile(p, []byte("x"), 0o644)).To(Succeed())
			return p
		}

		type seedRow struct {
			paths      []string
			slot       entimportscanbook.Slot
			class      entimportscanbook.Classification
			bookHC     uint32
			authorHC   uint32
			existingID *uint32
			decision   entimportscanbook.Decision
		}
		seed := func(r seedRow) *ent.ImportScanBook {
			GinkgoHelper()
			if r.slot == "" {
				r.slot = entimportscanbook.SlotEbook
			}
			if r.class == "" {
				r.class = entimportscanbook.ClassificationConfirmed
			}
			c := client.ImportScanBook.Create().
				SetScanID(scanID).
				SetFilePaths(r.paths).
				SetSlot(r.slot).
				SetClassification(r.class).
				SetBookHardcoverID(r.bookHC).
				SetAuthorHardcoverID(r.authorHC).
				SetNillableExistingBookID(r.existingID)
			if r.decision != "" {
				c.SetDecision(r.decision)
			}
			return c.SaveX(ctx)
		}

		authorDetails := func(books ...uint32) *metadata.AuthorDetails {
			d := &metadata.AuthorDetails{
				HardcoverID: 10, Name: "Author",
			}
			for _, b := range books {
				d.Books = append(
					d.Books,
					metadata.BookInfo{HardcoverID: b, Title: "Book"},
				)
			}
			return d
		}

		commit := func() *ent.ImportScan {
			GinkgoHelper()
			Expect(svc.Commit(ctx, scanID)).To(Succeed())
			Eventually(func() entimportscan.Status {
				cur, err := store.FindImportScan(ctx, scanID)
				Expect(err).NotTo(HaveOccurred())
				return cur.Status
			}, 5*time.Second, 20*time.Millisecond).Should(Equal(entimportscan.StatusCompleted))
			cur, err := store.FindImportScan(ctx, scanID)
			Expect(err).NotTo(HaveOccurred())
			return cur
		}

		reload := func(r *ent.ImportScanBook) *ent.ImportScanBook {
			GinkgoHelper()
			return client.ImportScanBook.GetX(ctx, r.ID)
		}

		It(
			"creates the author unmonitored-by-policy and attaches every ebook file",
			func() {
				epub := writeBookFile("a/Elantris.epub")
				mobi := writeBookFile("a/Elantris.mobi")
				row := seed(
					seedRow{paths: []string{epub, mobi}, bookHC: 1, authorHC: 10},
				)
				bookmeta.EXPECT().GetAuthor(mock.Anything, uint32(10)).
					Return(authorDetails(1, 2), nil).Once()

				done := commit()
				Expect(done.CommitSuccessCount).To(Equal(uint32(1)))
				Expect(done.CommitFailedCount).To(BeZero())

				got := reload(row)
				Expect(got.Outcome).To(Equal(entimportscanbook.OutcomeCreated))
				Expect(got.CreatedBookID).NotTo(BeNil())

				a := client.Author.Query().
					Where(entauthor.HardcoverID(10)).
					OnlyX(ctx)
				Expect(a.Monitored).To(BeTrue())
				Expect(a.MonitorPolicy).To(Equal(entauthor.MonitorPolicyNone))

				b := client.Book.GetX(ctx, *got.CreatedBookID)
				Expect(b.HardcoverID).To(Equal(uint32(1)))
				Expect(b.EbookStatus).To(Equal(entbook.EbookStatusAvailable))
				Expect(b.EbookMonitored).To(BeTrue())
				Expect(b.AudiobookStatus).To(Equal(entbook.AudiobookStatusSkipped))

				files := b.QueryMediaFiles().AllX(ctx)
				Expect(files).To(HaveLen(2))
				for _, f := range files {
					Expect(f.Source).To(Equal(entmediafile.SourceOrphan))
					Expect(f.BookKind).To(Equal(entmediafile.BookKindEbook))
					Expect(f.Size).To(Equal(int64(1)))
				}
				Expect([]string{files[0].Quality, files[1].Quality}).
					To(ConsistOf("EPUB", "MOBI"))
			},
		)

		It("fetches an author once for several of their books", func() {
			s1 := seed(seedRow{
				paths: []string{
					writeBookFile("Elantris.epub"),
				},
				bookHC:   1,
				authorHC: 10,
			})
			s2 := seed(seedRow{
				paths: []string{
					writeBookFile("Warbreaker.epub"),
				},
				bookHC:   2,
				authorHC: 10,
			})
			bookmeta.EXPECT().GetAuthor(mock.Anything, uint32(10)).
				Return(authorDetails(1, 2), nil).Once()

			done := commit()
			Expect(done.CommitSuccessCount).To(Equal(uint32(2)))
			Expect(reload(s1).Outcome).To(Equal(entimportscanbook.OutcomeCreated))
			Expect(reload(s2).Outcome).To(Equal(entimportscanbook.OutcomeCreated))
			Expect(client.Author.Query().CountX(ctx)).To(Equal(1))
		})

		It(
			"reuses an existing author, resolving a missing author id through GetBook",
			func() {
				_, err := store.CreateAuthor(ctx, db.CreateAuthorParams{
					HardcoverID:   10,
					Name:          "Author",
					MonitorPolicy: "none",
					WantKinds:     "ebook",
					Books: []db.BookSeed{
						{HardcoverID: 1, Title: "Elantris"},
					},
				})
				Expect(err).NotTo(HaveOccurred())
				bookmeta.EXPECT().GetBook(mock.Anything, uint32(1)).
					Return(&metadata.BookDetails{AuthorHardcover: 10}, nil).Once()
				row := seed(seedRow{
					paths: []string{writeBookFile("Elantris.epub")}, bookHC: 1,
				})

				commit()
				Expect(
					reload(row).Outcome,
				).To(Equal(entimportscanbook.OutcomeCreated))
				Expect(client.Author.Query().CountX(ctx)).To(Equal(1))
			},
		)

		It("attaches every audiobook file and leaves the ebook slot alone", func() {
			p1 := writeBookFile("Elantris/Part 01.mp3")
			p2 := writeBookFile("Elantris/Part 02.mp3")
			row := seed(seedRow{
				paths: []string{p1, p2}, slot: entimportscanbook.SlotAudiobook,
				bookHC: 1, authorHC: 10,
			})
			bookmeta.EXPECT().GetAuthor(mock.Anything, uint32(10)).
				Return(authorDetails(1), nil).Once()

			commit()
			got := reload(row)
			Expect(got.Outcome).To(Equal(entimportscanbook.OutcomeCreated))
			b := client.Book.GetX(ctx, *got.CreatedBookID)
			Expect(b.AudiobookStatus).To(Equal(entbook.AudiobookStatusAvailable))
			Expect(b.AudiobookMonitored).To(BeTrue())
			Expect(b.EbookStatus).To(Equal(entbook.EbookStatusSkipped))
			Expect(b.EbookMonitored).To(BeFalse())
			files := b.QueryMediaFiles().AllX(ctx)
			Expect(files).To(HaveLen(2))
			for _, f := range files {
				Expect(f.BookKind).To(Equal(entmediafile.BookKindAudiobook))
			}
		})

		It("records a failed candidate and still commits the others", func() {
			bad := seed(seedRow{
				paths: []string{writeBookFile("Bad.epub")}, bookHC: 5,
			})
			badAuthor := seed(seedRow{
				paths: []string{
					writeBookFile("BadAuthor.epub"),
				},
				bookHC:   6,
				authorHC: 11,
			})
			good := seed(seedRow{
				paths: []string{writeBookFile("Good.epub")}, bookHC: 1, authorHC: 10,
			})
			bookmeta.EXPECT().GetBook(mock.Anything, uint32(5)).
				Return(nil, errors.New("hardcover down")).Once()
			bookmeta.EXPECT().GetAuthor(mock.Anything, uint32(11)).
				Return(nil, errors.New("author gone")).Once()
			bookmeta.EXPECT().GetAuthor(mock.Anything, uint32(10)).
				Return(authorDetails(1), nil).Once()

			done := commit()
			Expect(done.CommitSuccessCount).To(Equal(uint32(1)))
			Expect(done.CommitFailedCount).To(Equal(uint32(2)))

			Expect(reload(bad).Outcome).To(Equal(entimportscanbook.OutcomeFailed))
			Expect(reload(bad).OutcomeMessage).To(ContainSubstring("hardcover down"))
			Expect(
				reload(badAuthor).Outcome,
			).To(Equal(entimportscanbook.OutcomeFailed))
			Expect(
				reload(badAuthor).OutcomeMessage,
			).To(ContainSubstring("author gone"))
			Expect(reload(good).Outcome).To(Equal(entimportscanbook.OutcomeCreated))
		})

		It("leaves skipped candidates untouched", func() {
			skipped := seed(seedRow{
				paths: []string{writeBookFile("Skip.epub")}, bookHC: 1, authorHC: 10,
				decision: entimportscanbook.DecisionSkip,
			})

			done := commit()
			Expect(done.CommitSuccessCount).To(BeZero())
			Expect(
				reload(skipped).Outcome,
			).To(Equal(entimportscanbook.OutcomePending))
			Expect(client.MediaFile.Query().CountX(ctx)).To(BeZero())
		})

		It("does not attach a path the book already holds", func() {
			a, err := store.CreateAuthor(ctx, db.CreateAuthorParams{
				HardcoverID:   10,
				Name:          "Author",
				MonitorPolicy: "none",
				WantKinds:     "ebook",
				Books:         []db.BookSeed{{HardcoverID: 1, Title: "Elantris"}},
			})
			Expect(err).NotTo(HaveOccurred())
			held := a.Edges.Books[0]
			epub := writeBookFile("Elantris.epub")
			mobi := writeBookFile("Elantris.mobi")
			_, err = store.CreateMediaFile(ctx, db.CreateMediaFileParams{
				BookID: held.ID, BookKind: entmediafile.BookKindEbook,
				Path: epub, Size: 1, Quality: "epub", Format: "epub",
				Source: entmediafile.SourceOrphan,
			})
			Expect(err).NotTo(HaveOccurred())
			existing := held.ID
			row := seed(seedRow{
				paths:      []string{epub, mobi},
				bookHC:     1,
				authorHC:   10,
				class:      entimportscanbook.ClassificationExisting,
				existingID: &existing,
			})

			commit()
			Expect(reload(row).Outcome).To(Equal(entimportscanbook.OutcomeCreated))
			Expect(client.MediaFile.Query().CountX(ctx)).To(Equal(2))
		})

		It("fails a candidate with no Hardcover match", func() {
			row := seed(seedRow{
				paths:    []string{writeBookFile("Mystery.epub")},
				class:    entimportscanbook.ClassificationUnmatched,
				decision: entimportscanbook.DecisionAccept,
			})

			done := commit()
			Expect(done.CommitFailedCount).To(Equal(uint32(1)))
			Expect(
				reload(row).OutcomeMessage,
			).To(ContainSubstring("no hardcover match"))
		})
	},
)
