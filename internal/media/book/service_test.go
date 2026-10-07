package book

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entbook "github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
	mockmeta "github.com/datahearth/streamline/internal/metadata/mocks"
	mockposters "github.com/datahearth/streamline/internal/posters/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Book service", Label("integration", "book"), func() {
	var (
		ctx      context.Context
		client   *ent.Client
		provider *mockmeta.MockBookProvider
		posters  *mockposters.MockManager
		svc      *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		provider = mockmeta.NewMockBookProvider(GinkgoT())
		posters = mockposters.NewMockManager(GinkgoT())
		svc = NewService(db.New(client), provider, posters)
		configtest.Setup(map[string]any{
			"library": map[string]any{
				"ebook_path":     GinkgoT().TempDir(),
				"audiobook_path": GinkgoT().TempDir(),
			},
		})
	})

	details := func(books ...metadata.BookInfo) *metadata.AuthorDetails {
		return &metadata.AuthorDetails{
			HardcoverID: 219851,
			Name:        "Brandon Sanderson",
			ImageURL:    "https://img/a.jpg",
			Books:       books,
		}
	}
	two := []metadata.BookInfo{
		{HardcoverID: 1, Title: "Elantris", CoverURL: "https://img/1.jpg"},
		{HardcoverID: 2, Title: "Warbreaker", CoverURL: "https://img/2.jpg"},
	}

	expectPosters := func(n int) chan struct{} {
		GinkgoHelper()
		done := make(chan struct{})
		remaining := n
		posters.EXPECT().
			Fetch(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Run(func(context.Context, string, uint32, string) {
				remaining--
				if remaining == 0 {
					close(done)
				}
			}).
			Return(nil).
			Times(n)
		return done
	}

	add := func(p AddParams, d *metadata.AuthorDetails) *ent.Author {
		GinkgoHelper()
		provider.EXPECT().GetAuthor(mock.Anything, uint32(219851)).
			Return(d, nil).Once()
		done := expectPosters(1 + len(d.Books))
		a, err := svc.Add(ctx, p)
		Expect(err).NotTo(HaveOccurred())
		Eventually(done).Should(BeClosed())
		return a
	}

	Describe("Add", func() {
		It("monitors the wanted kind under policy=all", func() {
			provider.EXPECT().GetAuthor(mock.Anything, uint32(219851)).
				Return(details(two...), nil).Once()
			fetched := make(chan struct{})
			posters.EXPECT().
				Fetch(mock.Anything, "authors", mock.Anything, "https://img/a.jpg").
				Return(nil).Once()
			posters.EXPECT().
				Fetch(mock.Anything, "books", mock.Anything, "https://img/1.jpg").
				Return(nil).Once()
			posters.EXPECT().
				Fetch(mock.Anything, "books", mock.Anything, "https://img/2.jpg").
				Run(func(context.Context, string, uint32, string) { close(fetched) }).
				Return(nil).Once()

			a, err := svc.Add(ctx, AddParams{
				HardcoverID: 219851, Monitored: true,
				MonitorPolicy: "all", WantKinds: "ebook",
			})
			Expect(err).NotTo(HaveOccurred())
			Eventually(fetched).Should(BeClosed())
			Expect(a.Folder).To(Equal("Brandon Sanderson"))
			Expect(a.Edges.Books).To(HaveLen(2))
			for _, b := range a.Edges.Books {
				Expect(b.EbookMonitored).To(BeTrue())
				Expect(b.EbookStatus).To(Equal(entbook.EbookStatusWanted))
				Expect(b.AudiobookMonitored).To(BeFalse())
				Expect(b.AudiobookStatus).To(Equal(entbook.AudiobookStatusSkipped))
			}
			Expect(client.Book.Query().Count(ctx)).To(Equal(2))
		})

		It("monitors nothing under policy=none", func() {
			a := add(AddParams{
				HardcoverID: 219851, MonitorPolicy: "none", WantKinds: "both",
			}, details(two...))
			for _, b := range a.Edges.Books {
				Expect(b.EbookMonitored).To(BeFalse())
				Expect(b.AudiobookMonitored).To(BeFalse())
				Expect(b.EbookStatus).To(Equal(entbook.EbookStatusSkipped))
				Expect(b.AudiobookStatus).To(Equal(entbook.AudiobookStatusSkipped))
			}
		})

		It("monitors only books released after the add under policy=future", func() {
			past := time.Now().AddDate(-1, 0, 0)
			future := time.Now().AddDate(1, 0, 0)
			a := add(AddParams{
				HardcoverID: 219851, MonitorPolicy: "future", WantKinds: "ebook",
			}, details(
				metadata.BookInfo{
					HardcoverID: 1,
					Title:       "Old",
					ReleaseDate: &past,
					CoverURL:    "u",
				},
				metadata.BookInfo{
					HardcoverID: 2,
					Title:       "New",
					ReleaseDate: &future,
					CoverURL:    "u",
				},
			))
			for _, b := range a.Edges.Books {
				Expect(b.EbookMonitored).To(Equal(b.Title == "New"))
			}
		})

		It("rejects a duplicate hardcover id", func() {
			client.Author.Create().SetHardcoverID(219851).SetName("x").SaveX(ctx)
			_, err := svc.Add(ctx, AddParams{HardcoverID: 219851})
			Expect(err).To(MatchError(ErrAuthorExists))
		})

		It("survives a panic in the poster goroutine", func() {
			provider.EXPECT().GetAuthor(mock.Anything, uint32(219851)).
				Return(details(), nil).Once()
			panicked := make(chan struct{})
			posters.EXPECT().
				Fetch(mock.Anything, "authors", mock.Anything, mock.Anything).
				Run(func(context.Context, string, uint32, string) {
					defer close(panicked)
					panic("boom")
				}).
				Return(nil).Once()
			_, err := svc.Add(ctx, AddParams{HardcoverID: 219851})
			Expect(err).NotTo(HaveOccurred())
			Eventually(panicked).Should(BeClosed())
		})
	})

	Describe("not found", func() {
		It("maps missing ids on every entry point", func() {
			_, err := svc.Get(ctx, 999)
			Expect(err).To(MatchError(ErrAuthorNotFound))
			_, err = svc.GetBook(ctx, 999)
			Expect(err).To(MatchError(ErrBookNotFound))
			Expect(svc.Delete(ctx, 999, false)).To(MatchError(ErrAuthorNotFound))
			_, err = svc.RefreshOne(ctx, 999)
			Expect(err).To(MatchError(ErrAuthorNotFound))
			Expect(svc.SetAuthorMonitored(ctx, 999, true)).
				To(MatchError(ErrAuthorNotFound))
			Expect(svc.UpdateAuthor(ctx, 999, UpdateAuthorParams{})).
				To(MatchError(ErrAuthorNotFound))
			Expect(svc.SetBookSlot(ctx, 999, "ebook", true)).
				To(MatchError(ErrBookNotFound))
			Expect(svc.SetBookSlot(ctx, 999, "paper", true)).
				To(MatchError(ErrInvalidSlotKind))
		})
	})

	Describe("SetBookSlot", func() {
		It("flips an empty audiobook slot and keeps a filled ebook slot", func() {
			a := add(AddParams{
				HardcoverID: 219851, MonitorPolicy: "all", WantKinds: "ebook",
			}, details(two[0]))
			id := a.Edges.Books[0].ID

			Expect(svc.SetBookSlot(ctx, id, "audiobook", true)).To(Succeed())
			got := client.Book.GetX(ctx, id)
			Expect(got.AudiobookStatus).To(Equal(entbook.AudiobookStatusWanted))
			Expect(svc.SetBookSlot(ctx, id, "audiobook", false)).To(Succeed())
			got = client.Book.GetX(ctx, id)
			Expect(got.AudiobookStatus).To(Equal(entbook.AudiobookStatusSkipped))

			client.MediaFile.Create().SetPath("/x.epub").SetSize(1).SetBookID(id).
				SetBookKind(mediafile.BookKindEbook).SaveX(ctx)
			client.Book.UpdateOneID(id).
				SetEbookStatus(entbook.EbookStatusAvailable).ExecX(ctx)
			Expect(svc.SetBookSlot(ctx, id, "ebook", true)).To(Succeed())
			Expect(client.Book.GetX(ctx, id).EbookStatus).
				To(Equal(entbook.EbookStatusAvailable))
		})
	})

	Describe("RefreshOne", func() {
		It(
			"applies the author's policy to new books only and persists the stamp",
			func() {
				a := add(AddParams{
					HardcoverID: 219851, MonitorPolicy: "future", WantKinds: "ebook",
				}, details(two[0]))

				past := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
				future := time.Now().AddDate(1, 0, 0)
				provider.EXPECT().GetAuthor(mock.Anything, uint32(219851)).
					Return(details(
						metadata.BookInfo{HardcoverID: 1, Title: "Elantris (10th)"},
						metadata.BookInfo{
							HardcoverID: 5, Title: "Future", ReleaseDate: &future,
							CoverURL: "https://img/5.jpg",
						},
						metadata.BookInfo{
							HardcoverID: 6, Title: "Past", ReleaseDate: &past,
							CoverURL: "https://img/6.jpg",
						},
					), nil).Once()
				done := expectPosters(2)
				existing := client.Book.Query().
					Where(entbook.HardcoverIDEQ(1)).OnlyX(ctx)
				Expect(
					svc.SetBookSlot(ctx, existing.ID, "ebook", true),
				).To(Succeed())

				got, err := svc.RefreshOne(ctx, a.ID)
				Expect(err).NotTo(HaveOccurred())
				Eventually(done).Should(BeClosed())
				Expect(got.LastRefreshedAt).NotTo(BeNil())
				Expect(client.Author.GetX(ctx, a.ID).LastRefreshedAt).NotTo(BeNil())

				byHC := func(hc uint32) *ent.Book {
					return client.Book.Query().
						Where(entbook.HardcoverIDEQ(hc)).OnlyX(ctx)
				}
				Expect(byHC(5).EbookStatus).To(Equal(entbook.EbookStatusWanted))
				Expect(byHC(6).EbookStatus).To(Equal(entbook.EbookStatusSkipped))
				old := byHC(1)
				Expect(old.Title).To(Equal("Elantris (10th)"))
				Expect(old.EbookMonitored).To(BeTrue())
				Expect(old.EbookStatus).To(Equal(entbook.EbookStatusWanted))
			},
		)
	})

	Describe("Delete", func() {
		seedFiles := func() (*ent.Author, string, string) {
			GinkgoHelper()
			a := add(AddParams{
				HardcoverID: 219851, MonitorPolicy: "all", WantKinds: "both",
			}, details(two[0]))
			lib := config.Get().Library
			ebook := filepath.Join(lib.EbookPath, "a.epub")
			audio := filepath.Join(lib.AudiobookPath, "a.m4b")
			Expect(os.WriteFile(ebook, []byte("x"), 0o600)).To(Succeed())
			Expect(os.WriteFile(audio, []byte("x"), 0o600)).To(Succeed())
			id := a.Edges.Books[0].ID
			client.MediaFile.Create().SetPath(ebook).SetSize(1).SetBookID(id).
				SetBookKind(mediafile.BookKindEbook).SaveX(ctx)
			client.MediaFile.Create().SetPath(audio).SetSize(1).SetBookID(id).
				SetBookKind(mediafile.BookKindAudiobook).SaveX(ctx)
			return a, ebook, audio
		}

		It("cascades and keeps files unless asked", func() {
			a, ebook, audio := seedFiles()
			posters.EXPECT().Remove("books", mock.Anything).Return(nil).Once()
			posters.EXPECT().Remove("authors", a.ID).Return(nil).Once()
			Expect(svc.Delete(ctx, a.ID, false)).To(Succeed())
			Expect(client.Book.Query().Count(ctx)).To(BeZero())
			Expect(ebook).To(BeAnExistingFile())
			Expect(audio).To(BeAnExistingFile())
		})

		It("removes files from the root of their kind", func() {
			a, ebook, audio := seedFiles()
			posters.EXPECT().Remove("books", mock.Anything).Return(nil).Once()
			posters.EXPECT().Remove("authors", a.ID).Return(nil).Once()
			Expect(svc.Delete(ctx, a.ID, true)).To(Succeed())
			Expect(ebook).NotTo(BeAnExistingFile())
			Expect(audio).NotTo(BeAnExistingFile())
		})
	})
})
