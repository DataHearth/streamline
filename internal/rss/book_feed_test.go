package rss

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entbook "github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/mediafile"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/rss/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func bookConfig(names ...string) map[string]any {
	cfg := indexerConfig(names...)
	cfg["book_quality_profiles"] = []map[string]any{{
		"name":            "books",
		"upgrade_allowed": true,
		"ebook": map[string]any{
			"formats": []string{"EPUB", "AZW3"}, "preferred": "EPUB",
		},
		"audiobook": map[string]any{
			"formats": []string{"M4B", "MP3"}, "preferred": "M4B",
			"min_bitrate": 64,
		},
	}}
	cfg["book_quality_default_profiles"] = map[string]any{
		"novel": "books", "bd": "books", "comic": "books", "manga": "books",
	}
	return cfg
}

func wantedBook(ebook, audiobook bool) *ent.Book {
	b := &ent.Book{
		ID:              1,
		Title:           "Book Title",
		AuthorName:      "Author Name",
		EbookStatus:     entbook.EbookStatusSkipped,
		AudiobookStatus: entbook.AudiobookStatusSkipped,
	}
	if ebook {
		b.EbookMonitored, b.EbookStatus = true, entbook.EbookStatusWanted
	}
	if audiobook {
		b.AudiobookMonitored = true
		b.AudiobookStatus = entbook.AudiobookStatusWanted
	}
	return b
}

var _ = Describe("FeedScanner book pass", Label("unit", "rss"), func() {
	var (
		ctx     context.Context
		store   *dbmocks.MockStore
		feeder  *mocks.MockIndexerFeeder
		grabber *mocks.MockDownloader
		books   *mocks.MockBookGrabber
		scanner *FeedScanner
		// upgradeBooks is what the store lists as holding files.
		upgradeBooks []*ent.Book
	)

	run := func(wanted []*ent.Book, items ...indexer.SearchResult) {
		GinkgoHelper()
		store.EXPECT().ListWantedMovies(mock.Anything).Return(nil, nil).Once()
		store.EXPECT().ListUpgradeCandidateMovies(mock.Anything).
			Return(nil, nil).Once()
		store.EXPECT().ListWantedAlbums(mock.Anything, uint8(3)).
			Return(nil, nil).Once()
		store.EXPECT().ListWantedBooks(mock.Anything, uint8(3)).
			Return(wanted, nil).Once()
		store.EXPECT().ListUpgradeCandidateAlbums(mock.Anything).
			Return(nil, nil).Once()
		store.EXPECT().ListUpgradeCandidateBooks(mock.Anything).
			Return(upgradeBooks, nil).Once()
		feeder.EXPECT().Feed(mock.Anything, "idx").Return(items, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	}

	expectBookkeeping := func(kind string) {
		store.EXPECT().ResetBookSlotGrabFailures(mock.Anything, uint32(1), kind).
			Return(nil).Once()
		store.EXPECT().SetBookSlotLastSearchAt(
			mock.Anything, uint32(1), kind, mock.AnythingOfType("time.Time"),
		).Return(nil).Once()
	}

	ebookItem := indexer.SearchResult{
		Title:    "Author Name - Book Title (2015) EPUB",
		Category: "7020",
	}
	audioItem := indexer.SearchResult{
		Title:    "Author Name - Book Title (2015) M4B",
		Category: "3030",
	}

	BeforeEach(func() {
		ctx = context.Background()
		upgradeBooks = nil
		store = dbmocks.NewMockStore(GinkgoT())
		feeder = mocks.NewMockIndexerFeeder(GinkgoT())
		grabber = mocks.NewMockDownloader(GinkgoT())
		books = mocks.NewMockBookGrabber(GinkgoT())
		configtest.Setup(bookConfig("idx"))
		scanner = NewFeedScanner(store, feeder, grabber, nil, books)
	})

	It("grabs a wanted ebook slot and records the success", func() {
		books.EXPECT().GrabBookRelease(
			mock.Anything, uint32(1),
			book.GrabParams{Kind: "ebook", Result: ebookItem},
		).Return(nil).Once()
		expectBookkeeping("ebook")
		run([]*ent.Book{wantedBook(true, false)}, ebookItem, ebookItem)
	})

	It("grabs a wanted audiobook slot", func() {
		books.EXPECT().GrabBookRelease(
			mock.Anything, uint32(1),
			book.GrabParams{Kind: "audiobook", Result: audioItem},
		).Return(nil).Once()
		expectBookkeeping("audiobook")
		run([]*ent.Book{wantedBook(false, true)}, audioItem)
	})

	It("never crosses kinds between category and format", func() {
		run(
			[]*ent.Book{wantedBook(true, true)},
			indexer.SearchResult{
				Title:    "Author Name - Book Title (2015) EPUB",
				Category: "3030",
			},
			indexer.SearchResult{
				Title:    "Author Name - Book Title (2015) M4B",
				Category: "7020",
			},
		)
	})

	It("skips a format outside the profile", func() {
		run(
			[]*ent.Book{wantedBook(true, false)},
			indexer.SearchResult{
				Title:    "Author Name - Book Title (2015) PDF",
				Category: "7020",
			},
		)
	})

	It("does not let a recent last_search_at block a feed grab", func() {
		b := wantedBook(true, false)
		recent := time.Now()
		b.EbookLastSearchAt = &recent
		books.EXPECT().GrabBookRelease(mock.Anything, uint32(1), mock.Anything).
			Return(nil).Once()
		expectBookkeeping("ebook")
		run([]*ent.Book{b}, ebookItem)
	})

	It("skips a slot at the failure cap", func() {
		b := wantedBook(true, false)
		b.EbookGrabFailures = 3
		run([]*ent.Book{b}, ebookItem)
	})

	It("fills each slot of one book from its own item in the same tick", func() {
		books.EXPECT().GrabBookRelease(
			mock.Anything, uint32(1),
			book.GrabParams{Kind: "ebook", Result: ebookItem},
		).Return(nil).Once()
		books.EXPECT().GrabBookRelease(
			mock.Anything, uint32(1),
			book.GrabParams{Kind: "audiobook", Result: audioItem},
		).Return(nil).Once()
		expectBookkeeping("ebook")
		expectBookkeeping("audiobook")
		run([]*ent.Book{wantedBook(true, true)}, ebookItem, audioItem)
	})

	Describe("series volumes", func() {
		volume := func(position float64) *ent.Book {
			b := wantedBook(true, false)
			b.AuthorName = "Eiichiro Oda"
			b.Title = fmt.Sprintf("One Piece %g", position)
			b.SeriesPosition = &position
			b.Edges.Series = &ent.BookSeries{ID: 9, Title: "One Piece"}
			return b
		}
		volumeItem := indexer.SearchResult{
			Title:    "One Piece T03 CBZ",
			Category: "7020",
		}

		BeforeEach(func() {
			cfg := bookConfig("idx")
			cfg["book_quality_profiles"] = []map[string]any{{
				"name": "books",
				"ebook": map[string]any{
					"formats": []string{"EPUB", "CBZ"}, "preferred": "EPUB",
				},
				"audiobook": map[string]any{
					"formats": []string{"M4B"}, "preferred": "M4B",
				},
			}}
			configtest.Setup(cfg)
		})

		It(
			"matches a volume by the series name and number, no author needed",
			func() {
				books.EXPECT().GrabBookRelease(
					mock.Anything, uint32(1),
					book.GrabParams{Kind: "ebook", Result: volumeItem},
				).Return(nil).Once()
				expectBookkeeping("ebook")
				run([]*ent.Book{volume(3)}, volumeItem)
			},
		)

		It("leaves a release of another volume alone", func() {
			run([]*ent.Book{volume(4)}, volumeItem)
		})

		It("leaves a release of another series alone", func() {
			run([]*ent.Book{volume(3)}, indexer.SearchResult{
				Title: "Two Piece T03 CBZ", Category: "7020",
			})
		})

		It("skips a release tagged with another language than the edition", func() {
			v := volume(3)
			v.Edges.EbookEdition = &ent.BookEdition{Language: "en"}
			run([]*ent.Book{v}, indexer.SearchResult{
				Title: "One Piece T03 FRENCH CBZ", Category: "7020",
			})
		})

		It("takes a release tagged with the edition's own language", func() {
			v := volume(3)
			v.Edges.EbookEdition = &ent.BookEdition{Language: "fr"}
			item := indexer.SearchResult{
				Title: "One Piece T03 FRENCH CBZ", Category: "7020",
			}
			books.EXPECT().GrabBookRelease(
				mock.Anything, uint32(1),
				book.GrabParams{Kind: "ebook", Result: item},
			).Return(nil).Once()
			expectBookkeeping("ebook")
			run([]*ent.Book{v}, item)
		})
	})

	It("matches a book by its original title as well", func() {
		b := wantedBook(true, false)
		b.Title = "Le Titre"
		b.OriginalTitle = "Book Title"
		books.EXPECT().GrabBookRelease(
			mock.Anything, uint32(1),
			book.GrabParams{Kind: "ebook", Result: ebookItem},
		).Return(nil).Once()
		expectBookkeeping("ebook")
		run([]*ent.Book{b}, ebookItem)
	})

	It("skips collections", func() {
		run(
			[]*ent.Book{wantedBook(true, false)},
			indexer.SearchResult{
				Title:    "Author Name - Book Title Collection EPUB",
				Category: "7020",
			},
		)
	})

	DescribeTable("never matches items outside the book categories",
		func(category string) {
			run(
				[]*ent.Book{wantedBook(true, true)},
				indexer.SearchResult{Title: ebookItem.Title, Category: category},
			)
		},
		Entry("empty", ""),
		Entry("music", "3040"),
		Entry("movies", "2040"),
		Entry("just past ebooks", "8000"),
	)

	It("bumps the slot's grab_failures on a release failure", func() {
		books.EXPECT().GrabBookRelease(mock.Anything, uint32(1), mock.Anything).
			Return(fmt.Errorf("add: %w", download.ErrNoWantedFiles)).Once()
		store.EXPECT().IncrementBookSlotGrabFailures(
			mock.Anything, uint32(1), "ebook",
		).Return(nil).Once()
		run([]*ent.Book{wantedBook(true, false)}, ebookItem)
	})

	It("does not bump grab_failures on a transport failure", func() {
		books.EXPECT().GrabBookRelease(mock.Anything, uint32(1), mock.Anything).
			Return(fmt.Errorf("grab: %w", download.ErrUnreachable)).Once()
		run([]*ent.Book{wantedBook(true, false)}, ebookItem)
	})

	Describe("upgrades", func() {
		heldBook := func(kind mediafile.BookKind, quality string, bitrate uint32) *ent.Book {
			b := wantedBook(false, false)
			if kind == mediafile.BookKindEbook {
				b.EbookMonitored, b.EbookStatus = true, entbook.EbookStatusAvailable
			} else {
				b.AudiobookMonitored = true
				b.AudiobookStatus = entbook.AudiobookStatusAvailable
			}
			b.Edges.MediaFiles = []*ent.MediaFile{{
				BookKind: kind, Quality: quality, Bitrate: bitrate,
			}}
			return b
		}
		azw3Item := indexer.SearchResult{
			Title:    "Author Name - Book Title (2015) AZW3",
			Category: "7020",
		}
		expectFlag := func(kind downloadrecord.BookKind) {
			store.EXPECT().SetLiveBookRecordReplaceMode(
				mock.Anything, uint32(1), kind, downloadrecord.ReplaceModeUpgrades,
			).Return(nil).Once()
		}
		upgrade := func(b *ent.Book, items ...indexer.SearchResult) {
			upgradeBooks = []*ent.Book{b}
			run(nil, items...)
		}

		It("grabs a better ebook format and flags the record", func() {
			books.EXPECT().GrabBookRelease(mock.Anything, uint32(1), mock.Anything).
				Return(nil).Once()
			expectFlag(downloadrecord.BookKindEbook)
			upgrade(
				heldBook(mediafile.BookKindEbook, "AZW3", 0),
				ebookItem,
				ebookItem,
			)
		})

		It("leaves an ebook at the preferred format alone", func() {
			upgrade(heldBook(mediafile.BookKindEbook, "EPUB", 0), ebookItem)
		})

		It("does not take a worse format", func() {
			upgrade(heldBook(mediafile.BookKindEbook, "EPUB", 0), azw3Item)
		})

		It("does not touch a slot that is not monitored", func() {
			b := heldBook(mediafile.BookKindEbook, "AZW3", 0)
			b.EbookMonitored = false
			upgrade(b, ebookItem)
		})

		It("upgrades an audiobook folder measured under the floor", func() {
			books.EXPECT().GrabBookRelease(mock.Anything, uint32(1), mock.Anything).
				Return(nil).Once()
			expectFlag(downloadrecord.BookKindAudiobook)
			upgrade(heldBook(mediafile.BookKindAudiobook, "M4B", 32000), audioItem)
		})

		It("leaves a healthy audiobook at the preferred format alone", func() {
			upgrade(heldBook(mediafile.BookKindAudiobook, "M4B", 128000), audioItem)
		})

		It("does nothing when the profile forbids upgrades", func() {
			cfg := bookConfig("idx")
			cfg["book_quality_profiles"] = []map[string]any{{
				"name": "books",
				"ebook": map[string]any{
					"formats": []string{"EPUB", "AZW3"}, "preferred": "EPUB",
				},
				"audiobook": map[string]any{
					"formats": []string{"M4B"}, "preferred": "M4B",
				},
			}}
			configtest.Setup(cfg)
			upgrade(heldBook(mediafile.BookKindEbook, "AZW3", 0), ebookItem)
		})

		It("bumps the slot's grab_failures when the upgrade grab fails", func() {
			books.EXPECT().GrabBookRelease(mock.Anything, uint32(1), mock.Anything).
				Return(fmt.Errorf("add: %w", download.ErrNoWantedFiles)).Once()
			store.EXPECT().IncrementBookSlotGrabFailures(
				mock.Anything, uint32(1), "ebook",
			).Return(nil).Once()
			upgrade(heldBook(mediafile.BookKindEbook, "AZW3", 0), ebookItem)
		})
	})
})

var _ = DescribeTable("bookKindForCategory", Label("unit", "rss"),
	func(cat, want string) {
		Expect(bookKindForCategory(cat)).To(Equal(want))
	},
	Entry("audiobook", "3030", "audiobook"),
	Entry("ebook", "7020", "ebook"),
	Entry("ebook parent", "7000", "ebook"),
	Entry("music", "3040", ""),
	Entry("empty", "", ""),
	Entry("junk", "x", ""),
)
