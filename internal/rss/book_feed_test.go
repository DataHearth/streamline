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
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/rss/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func bookConfig(names ...string) map[string]any {
	cfg := indexerConfig(names...)
	cfg["ebook_quality_default_profile"] = "ebooks"
	cfg["ebook_quality_profiles"] = []map[string]any{{
		"name": "ebooks", "formats": []string{"epub", "azw3"}, "cutoff": "epub",
	}}
	cfg["audiobook_quality_default_profile"] = "audio"
	cfg["audiobook_quality_profiles"] = []map[string]any{{
		"name": "audio", "formats": []string{"m4b"}, "cutoff": "m4b",
	}}
	return cfg
}

func wantedBook(ebook, audiobook bool) *ent.Book {
	b := &ent.Book{
		ID:              1,
		Title:           "Book Title",
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
	b.Edges.Author = &ent.Author{ID: 50, Name: "Author Name", Monitored: true}
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
