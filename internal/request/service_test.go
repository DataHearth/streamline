package request_test

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/datahearth/streamline/ent"
	entrequest "github.com/datahearth/streamline/ent/request"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/media/book"
	bookmocks "github.com/datahearth/streamline/internal/media/book/mocks"
	"github.com/datahearth/streamline/internal/media/music"
	musicmocks "github.com/datahearth/streamline/internal/media/music/mocks"
	"github.com/datahearth/streamline/internal/metadata"
	metadatamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/request"
	reqmocks "github.com/datahearth/streamline/internal/request/mocks"
	"github.com/datahearth/streamline/internal/role"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

// barrierStore holds every insert until both callers have cleared the
// duplicate pre-check, reproducing the interleaving the unique index has to
// catch instead of leaving it to the scheduler.
type barrierStore struct {
	db.Store
	gate *sync.WaitGroup
}

func (s barrierStore) CreateRequest(
	ctx context.Context,
	p db.CreateRequestParams,
) (*ent.Request, error) {
	s.gate.Done()
	s.gate.Wait()
	return s.Store.CreateRequest(ctx, p)
}

var _ = Describe("Request service", Label("unit", "request"), func() {
	var (
		ctx        context.Context
		storeMk    *dbmocks.MockStore_Expecter
		movieMk    *reqmocks.MockMovieAdder_Expecter
		showMk     *reqmocks.MockShowAdder_Expecter
		musicMk    *musicmocks.MockAdder_Expecter
		metaMk     *metadatamocks.MockMusicProvider_Expecter
		bookMk     *bookmocks.MockAdder_Expecter
		bookMetaMk *metadatamocks.MockBookProvider_Expecter
		svc        *request.Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		store := dbmocks.NewMockStore(GinkgoT())
		storeMk = store.EXPECT()
		movies := reqmocks.NewMockMovieAdder(GinkgoT())
		movieMk = movies.EXPECT()
		shows := reqmocks.NewMockShowAdder(GinkgoT())
		showMk = shows.EXPECT()
		musicAdder := musicmocks.NewMockAdder(GinkgoT())
		musicMk = musicAdder.EXPECT()
		musicMeta := metadatamocks.NewMockMusicProvider(GinkgoT())
		metaMk = musicMeta.EXPECT()
		bookAdder := bookmocks.NewMockAdder(GinkgoT())
		bookMk = bookAdder.EXPECT()
		bookMeta := metadatamocks.NewMockBookProvider(GinkgoT())
		bookMetaMk = bookMeta.EXPECT()
		svc = request.NewService(
			store, movies, shows, musicAdder, bookAdder, musicMeta, bookMeta,
		)
	})

	Describe("Create", func() {
		It("creates a movie request when none active and not in library", func() {
			storeMk.FindActiveRequest(mock.Anything, "movie", uint32(5)).
				Return(nil, nil).Once()
			movieMk.GetByTMDBID(mock.Anything, uint32(5)).
				Return(nil, errors.New("not found")).Once()
			storeMk.CreateRequest(mock.Anything, mock.MatchedBy(func(p db.CreateRequestParams) bool {
				return p.MediaType == "movie" && p.MediaID == 5 && p.RequesterID == 9
			})).
				Return(&ent.Request{ID: 1}, nil).
				Once()

			r, err := svc.Create(
				ctx,
				request.CreateParams{
					MediaType:      "movie",
					MediaID:        5,
					Title:          "Flick",
					RequesterID:    9,
					QualityProfile: "",
				},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(r.ID).To(Equal(uint32(1)))
		})

		It("records the requester's preferred quality profile", func() {
			storeMk.FindActiveRequest(mock.Anything, "movie", uint32(5)).
				Return(nil, nil).Once()
			movieMk.GetByTMDBID(mock.Anything, uint32(5)).
				Return(nil, errors.New("not found")).Once()
			storeMk.CreateRequest(mock.Anything, mock.MatchedBy(func(p db.CreateRequestParams) bool {
				return p.QualityProfile == "Remux"
			})).
				Return(&ent.Request{ID: 1}, nil).
				Once()

			_, err := svc.Create(
				ctx,
				request.CreateParams{
					MediaType:      "movie",
					MediaID:        5,
					Title:          "Flick",
					RequesterID:    9,
					QualityProfile: "Remux",
				},
			)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects duplicates when an active request exists", func() {
			storeMk.FindActiveRequest(mock.Anything, "movie", uint32(5)).
				Return(&ent.Request{ID: 7}, nil).Once()

			_, err := svc.Create(
				ctx,
				request.CreateParams{
					MediaType:      "movie",
					MediaID:        5,
					Title:          "Flick",
					RequesterID:    9,
					QualityProfile: "",
				},
			)
			Expect(err).To(MatchError(request.ErrDuplicate))
		})

		It("rejects when the movie is already in the library", func() {
			storeMk.FindActiveRequest(mock.Anything, "movie", uint32(5)).
				Return(nil, nil).Once()
			movieMk.GetByTMDBID(mock.Anything, uint32(5)).
				Return(&ent.Movie{ID: 3}, nil).Once()

			_, err := svc.Create(
				ctx,
				request.CreateParams{
					MediaType:      "movie",
					MediaID:        5,
					Title:          "Flick",
					RequesterID:    9,
					QualityProfile: "",
				},
			)
			Expect(err).To(MatchError(request.ErrDuplicate))
		})

		It("maps a unique-index violation to request.ErrDuplicate", func() {
			storeMk.FindActiveRequest(mock.Anything, "movie", uint32(5)).
				Return(nil, nil).Once()
			movieMk.GetByTMDBID(mock.Anything, uint32(5)).
				Return(nil, errors.New("not found")).Once()
			storeMk.CreateRequest(mock.Anything, mock.Anything).
				Return(nil, &ent.ConstraintError{}).Once()

			_, err := svc.Create(
				ctx,
				request.CreateParams{
					MediaType:      "movie",
					MediaID:        5,
					Title:          "Flick",
					RequesterID:    9,
					QualityProfile: "",
				},
			)
			Expect(err).To(MatchError(request.ErrDuplicate))
		})

		It("rejects when the show is already in the library", func() {
			storeMk.FindActiveRequest(mock.Anything, "tvshow", uint32(8)).
				Return(nil, nil).Once()
			storeMk.FindTVShowByTVDBID(mock.Anything, uint32(8)).
				Return(&ent.TVShow{ID: 2}, nil).Once()

			_, err := svc.Create(
				ctx,
				request.CreateParams{
					MediaType:      "tvshow",
					MediaID:        8,
					Title:          "Show",
					RequesterID:    9,
					QualityProfile: "",
				},
			)
			Expect(err).To(MatchError(request.ErrDuplicate))
		})
	})

	Describe("Create music requests", func() {
		const (
			artistMBID = "artist-1"
			rgMBID     = "rg-1"
		)

		It("persists an artist request keyed by MBID", func() {
			storeMk.FindActiveRequestByMBID(mock.Anything, "artist", artistMBID).
				Return(nil, nil).Once()
			storeMk.FindArtistByMBID(mock.Anything, artistMBID).
				Return(nil, nil).Once()
			storeMk.CreateRequest(mock.Anything, mock.MatchedBy(func(p db.CreateRequestParams) bool {
				return p.MediaType == "artist" && p.MediaMBID == artistMBID &&
					p.MediaID == 0
			})).
				Return(&ent.Request{ID: 1}, nil).
				Once()

			r, err := svc.Create(ctx, request.CreateParams{
				MediaType:   "artist",
				MediaMBID:   artistMBID,
				Title:       "A",
				RequesterID: 9,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(r.ID).To(Equal(uint32(1)))
		})

		It("rejects a duplicate active artist request", func() {
			storeMk.FindActiveRequestByMBID(mock.Anything, "artist", artistMBID).
				Return(&ent.Request{ID: 7}, nil).Once()

			_, err := svc.Create(ctx, request.CreateParams{
				MediaType:   "artist",
				MediaMBID:   artistMBID,
				Title:       "A",
				RequesterID: 9,
			})
			Expect(err).To(MatchError(request.ErrDuplicate))
		})

		It("rejects an artist already in the library", func() {
			storeMk.FindActiveRequestByMBID(mock.Anything, "artist", artistMBID).
				Return(nil, nil).Once()
			storeMk.FindArtistByMBID(mock.Anything, artistMBID).
				Return(&ent.Artist{ID: 2}, nil).Once()

			_, err := svc.Create(ctx, request.CreateParams{
				MediaType:   "artist",
				MediaMBID:   artistMBID,
				Title:       "A",
				RequesterID: 9,
			})
			Expect(err).To(MatchError(request.ErrDuplicate))
		})

		It(
			"maps a unique-index violation on an artist to request.ErrDuplicate",
			func() {
				storeMk.FindActiveRequestByMBID(mock.Anything, "artist", artistMBID).
					Return(nil, nil).Once()
				storeMk.FindArtistByMBID(mock.Anything, artistMBID).
					Return(nil, nil).Once()
				storeMk.CreateRequest(mock.Anything, mock.Anything).
					Return(nil, &ent.ConstraintError{}).Once()

				_, err := svc.Create(ctx, request.CreateParams{
					MediaType:   "artist",
					MediaMBID:   artistMBID,
					Title:       "A",
					RequesterID: 9,
				})
				Expect(err).To(MatchError(request.ErrDuplicate))
			},
		)

		It("persists an album request without an in-library pre-check", func() {
			storeMk.FindActiveRequestByMBID(mock.Anything, "album", rgMBID).
				Return(nil, nil).Once()
			storeMk.CreateRequest(mock.Anything, mock.MatchedBy(func(p db.CreateRequestParams) bool {
				return p.MediaType == "album" && p.MediaMBID == rgMBID
			})).
				Return(&ent.Request{ID: 2}, nil).
				Once()

			_, err := svc.Create(ctx, request.CreateParams{
				MediaType: "album", MediaMBID: rgMBID, Title: "Al", RequesterID: 9,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		DescribeTable("rejects an invalid identity before any store call",
			func(p request.CreateParams) {
				_, err := svc.Create(ctx, p)
				Expect(err).To(MatchError(request.ErrInvalidRequest))
			},
			Entry("artist without an mbid",
				request.CreateParams{MediaType: "artist", Title: "A"}),
			Entry(
				"album with a media_id",
				request.CreateParams{
					MediaType: "album",
					MediaMBID: "x",
					MediaID:   3,
				},
			),
			Entry("movie without a media_id",
				request.CreateParams{MediaType: "movie", Title: "M"}),
			Entry(
				"tvshow with an mbid",
				request.CreateParams{
					MediaType: "tvshow",
					MediaID:   3,
					MediaMBID: "x",
				},
			),
			Entry("author without a media_id",
				request.CreateParams{MediaType: "author", Title: "A"}),
			Entry("book with an mbid",
				request.CreateParams{MediaType: "book", MediaID: 3, MediaMBID: "x"}),
			Entry(
				"book_kind on a movie",
				request.CreateParams{
					MediaType: "movie",
					MediaID:   3,
					BookKind:  "ebook",
				},
			),
		)
	})

	Describe("Approve music requests", func() {
		const (
			artistMBID = "artist-1"
			rgMBID     = "rg-1"
		)

		It("adds a monitored artist then approves, recording no event", func() {
			storeMk.GetRequest(mock.Anything, uint32(1)).
				Return(&ent.Request{ID: 1, MediaType: "artist", MediaMbid: artistMBID}, nil).
				Twice()
			musicMk.Add(mock.Anything, music.AddParams{
				MBID: artistMBID, Monitored: true, QualityProfile: "lossless",
			}).Return(&ent.Artist{ID: 4}, nil).Once()
			storeMk.ApproveRequest(mock.Anything, uint32(1), uint32(9)).
				Return(nil).Once()

			_, err := svc.Approve(ctx, 1, 9, "lossless")
			Expect(err).NotTo(HaveOccurred())
		})

		It("marks the request available when the artist already exists", func() {
			storeMk.GetRequest(mock.Anything, uint32(1)).
				Return(&ent.Request{ID: 1, MediaType: "artist", MediaMbid: artistMBID}, nil).
				Twice()
			musicMk.Add(mock.Anything, mock.Anything).
				Return(nil, fmt.Errorf("%w: mbid", music.ErrArtistExists)).Once()
			storeMk.ApproveRequest(mock.Anything, uint32(1), uint32(9)).
				Return(nil).Once()
			storeMk.MarkRequestAvailable(mock.Anything, uint32(1)).
				Return(nil).Once()

			_, err := svc.Approve(ctx, 1, 9, "")
			Expect(err).NotTo(HaveOccurred())
		})

		It("does not approve when the artist add fails", func() {
			storeMk.GetRequest(mock.Anything, uint32(1)).
				Return(&ent.Request{ID: 1, MediaType: "artist", MediaMbid: artistMBID}, nil).
				Once()
			musicMk.Add(mock.Anything, mock.Anything).
				Return(nil, errors.New("musicbrainz down")).Once()

			_, err := svc.Approve(ctx, 1, 9, "")
			Expect(err).To(MatchError(ContainSubstring("approve: add artist")))
		})

		It("adds the absent artist unmonitored and monitors only the album", func() {
			storeMk.GetRequest(mock.Anything, uint32(2)).
				Return(&ent.Request{ID: 2, MediaType: "album", MediaMbid: rgMBID}, nil).
				Twice()
			metaMk.GetReleaseGroup(mock.Anything, rgMBID).
				Return(&metadata.ReleaseGroupDetails{ArtistMBID: artistMBID}, nil).
				Once()
			storeMk.FindArtistByMBID(mock.Anything, artistMBID).
				Return(nil, nil).Once()
			musicMk.Add(mock.Anything, music.AddParams{
				MBID: artistMBID, Monitored: false, QualityProfile: "lossless",
			}).Return(&ent.Artist{ID: 4, Edges: ent.ArtistEdges{Albums: []*ent.Album{
				{ID: 10, Mbid: "other"}, {ID: 11, Mbid: rgMBID},
			}}}, nil).Once()
			musicMk.SetAlbumMonitored(mock.Anything, uint32(11), true).
				Return(nil).Once()
			storeMk.ApproveRequest(mock.Anything, uint32(2), uint32(9)).
				Return(nil).Once()

			_, err := svc.Approve(ctx, 2, 9, "lossless")
			Expect(err).NotTo(HaveOccurred())
		})

		It("monitors the album under an artist already in the library", func() {
			storeMk.GetRequest(mock.Anything, uint32(2)).
				Return(&ent.Request{ID: 2, MediaType: "album", MediaMbid: rgMBID}, nil).
				Twice()
			metaMk.GetReleaseGroup(mock.Anything, rgMBID).
				Return(&metadata.ReleaseGroupDetails{ArtistMBID: artistMBID}, nil).
				Once()
			storeMk.FindArtistByMBID(mock.Anything, artistMBID).
				Return(&ent.Artist{ID: 4}, nil).Once()
			musicMk.Get(mock.Anything, uint32(4)).
				Return(&ent.Artist{ID: 4, Edges: ent.ArtistEdges{Albums: []*ent.Album{
					{ID: 11, Mbid: rgMBID},
				}}}, nil).
				Once()
			musicMk.SetAlbumMonitored(mock.Anything, uint32(11), true).
				Return(nil).Once()
			storeMk.ApproveRequest(mock.Anything, uint32(2), uint32(9)).
				Return(nil).Once()

			_, err := svc.Approve(ctx, 2, 9, "")
			Expect(err).NotTo(HaveOccurred())
		})

		It("fails without approving when the album is not in the artist", func() {
			storeMk.GetRequest(mock.Anything, uint32(2)).
				Return(&ent.Request{ID: 2, MediaType: "album", MediaMbid: rgMBID}, nil).
				Once()
			metaMk.GetReleaseGroup(mock.Anything, rgMBID).
				Return(&metadata.ReleaseGroupDetails{ArtistMBID: artistMBID}, nil).
				Once()
			storeMk.FindArtistByMBID(mock.Anything, artistMBID).
				Return(&ent.Artist{ID: 4}, nil).Once()
			musicMk.Get(mock.Anything, uint32(4)).
				Return(&ent.Artist{ID: 4}, nil).Once()

			_, err := svc.Approve(ctx, 2, 9, "")
			Expect(err).To(MatchError(ContainSubstring("approve: add album")))
		})
	})

	Describe("Create book requests", func() {
		It("persists an author request", func() {
			storeMk.FindActiveRequest(mock.Anything, "author", uint32(30)).
				Return(nil, nil).Once()
			storeMk.FindAuthorByHardcoverID(mock.Anything, uint32(30)).
				Return(nil, nil).Once()
			storeMk.CreateRequest(mock.Anything, mock.MatchedBy(func(p db.CreateRequestParams) bool {
				return p.MediaType == "author" && p.MediaID == 30
			})).
				Return(&ent.Request{ID: 1}, nil).
				Once()

			_, err := svc.Create(ctx, request.CreateParams{
				MediaType: "author", MediaID: 30, Title: "A", RequesterID: 9,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects a duplicate active author request", func() {
			storeMk.FindActiveRequest(mock.Anything, "author", uint32(30)).
				Return(&ent.Request{ID: 7}, nil).Once()

			_, err := svc.Create(ctx, request.CreateParams{
				MediaType: "author", MediaID: 30, Title: "A", RequesterID: 9,
			})
			Expect(err).To(MatchError(request.ErrDuplicate))
		})

		It("rejects an author already in the library", func() {
			storeMk.FindActiveRequest(mock.Anything, "author", uint32(30)).
				Return(nil, nil).Once()
			storeMk.FindAuthorByHardcoverID(mock.Anything, uint32(30)).
				Return(&ent.Author{ID: 2}, nil).Once()

			_, err := svc.Create(ctx, request.CreateParams{
				MediaType: "author", MediaID: 30, Title: "A", RequesterID: 9,
			})
			Expect(err).To(MatchError(request.ErrDuplicate))
		})

		It("persists a book request with its kind", func() {
			storeMk.FindActiveRequest(mock.Anything, "book", uint32(40)).
				Return(nil, nil).Once()
			storeMk.CreateRequest(mock.Anything, mock.MatchedBy(func(p db.CreateRequestParams) bool {
				return p.MediaType == "book" && p.MediaID == 40 &&
					p.BookKind == "ebook"
			})).
				Return(&ent.Request{ID: 3}, nil).
				Once()

			_, err := svc.Create(ctx, request.CreateParams{
				MediaType: "book", MediaID: 40, BookKind: "ebook",
				Title: "B", RequesterID: 9,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		DescribeTable("rejects a book without a valid kind",
			func(kind string) {
				_, err := svc.Create(ctx, request.CreateParams{
					MediaType: "book", MediaID: 40, BookKind: kind, Title: "B",
				})
				Expect(err).To(MatchError(request.ErrInvalidRequest))
			},
			Entry("missing", ""),
			Entry("unknown", "paperback"),
		)

		// The dedup index ignores book_kind, so a second kind for a book
		// with an active request is refused rather than widening the first.
		It("refuses a different kind for an already-requested book", func() {
			storeMk.FindActiveRequest(mock.Anything, "book", uint32(40)).
				Return(&ent.Request{ID: 3, BookKind: "ebook"}, nil).Once()

			_, err := svc.Create(ctx, request.CreateParams{
				MediaType: "book", MediaID: 40, BookKind: "audiobook",
				Title: "B", RequesterID: 9,
			})
			Expect(err).To(MatchError(request.ErrDuplicate))
		})
	})

	Describe("Approve book requests", func() {
		bookReq := func(kind string) *ent.Request {
			return &ent.Request{
				ID:        5,
				MediaType: "book",
				MediaID:   40,
				BookKind:  entrequest.BookKind(kind),
			}
		}

		It("adds a monitored author with the default policy", func() {
			storeMk.GetRequest(mock.Anything, uint32(4)).
				Return(&ent.Request{ID: 4, MediaType: "author", MediaID: 30}, nil).
				Twice()
			bookMk.Add(mock.Anything, book.AddParams{
				HardcoverID: 30, Monitored: true,
			}).Return(&ent.Author{ID: 8}, nil).Once()
			storeMk.ApproveRequest(mock.Anything, uint32(4), uint32(9)).
				Return(nil).Once()

			_, err := svc.Approve(ctx, 4, 9, "")
			Expect(err).NotTo(HaveOccurred())
		})

		It("marks the request available when the author already exists", func() {
			storeMk.GetRequest(mock.Anything, uint32(4)).
				Return(&ent.Request{ID: 4, MediaType: "author", MediaID: 30}, nil).
				Twice()
			bookMk.Add(mock.Anything, mock.Anything).
				Return(nil, fmt.Errorf("%w: id", book.ErrAuthorExists)).Once()
			storeMk.ApproveRequest(mock.Anything, uint32(4), uint32(9)).
				Return(nil).Once()
			storeMk.MarkRequestAvailable(mock.Anything, uint32(4)).
				Return(nil).Once()

			_, err := svc.Approve(ctx, 4, 9, "")
			Expect(err).NotTo(HaveOccurred())
		})

		It(
			"adds an absent author with policy none and wants the ebook slot",
			func() {
				storeMk.GetRequest(mock.Anything, uint32(5)).
					Return(bookReq("ebook"), nil).Twice()
				bookMetaMk.GetBook(mock.Anything, uint32(40)).
					Return(&metadata.BookDetails{AuthorHardcover: 30}, nil).Once()
				storeMk.FindAuthorByHardcoverID(mock.Anything, uint32(30)).
					Return(nil, nil).Once()
				bookMk.Add(mock.Anything, book.AddParams{
					HardcoverID: 30, Monitored: true, MonitorPolicy: "none",
				}).Return(&ent.Author{ID: 8, Edges: ent.AuthorEdges{Books: []*ent.Book{
					{ID: 20, HardcoverID: 41}, {ID: 21, HardcoverID: 40},
				}}}, nil).Once()
				bookMk.SetBookSlot(mock.Anything, uint32(21), "ebook", true).
					Return(nil).Once()
				storeMk.ApproveRequest(mock.Anything, uint32(5), uint32(9)).
					Return(nil).Once()

				_, err := svc.Approve(ctx, 5, 9, "")
				Expect(err).NotTo(HaveOccurred())
			},
		)

		It("wants both slots in order for a both request", func() {
			storeMk.GetRequest(mock.Anything, uint32(5)).
				Return(bookReq("both"), nil).Twice()
			bookMetaMk.GetBook(mock.Anything, uint32(40)).
				Return(&metadata.BookDetails{AuthorHardcover: 30}, nil).Once()
			storeMk.FindAuthorByHardcoverID(mock.Anything, uint32(30)).
				Return(&ent.Author{ID: 8}, nil).Once()
			bookMk.Get(mock.Anything, uint32(8)).
				Return(&ent.Author{ID: 8, Edges: ent.AuthorEdges{Books: []*ent.Book{
					{ID: 21, HardcoverID: 40},
				}}}, nil).Once()
			bookMk.SetBookSlot(mock.Anything, uint32(21), "ebook", true).
				Return(nil).Once()
			bookMk.SetBookSlot(mock.Anything, uint32(21), "audiobook", true).
				Return(nil).Once()
			storeMk.ApproveRequest(mock.Anything, uint32(5), uint32(9)).
				Return(nil).Once()

			_, err := svc.Approve(ctx, 5, 9, "")
			Expect(err).NotTo(HaveOccurred())
		})

		It("fails without approving when the book is not in the author", func() {
			storeMk.GetRequest(mock.Anything, uint32(5)).
				Return(bookReq("ebook"), nil).Once()
			bookMetaMk.GetBook(mock.Anything, uint32(40)).
				Return(&metadata.BookDetails{AuthorHardcover: 30}, nil).Once()
			storeMk.FindAuthorByHardcoverID(mock.Anything, uint32(30)).
				Return(&ent.Author{ID: 8}, nil).Once()
			bookMk.Get(mock.Anything, uint32(8)).
				Return(&ent.Author{ID: 8}, nil).Once()

			_, err := svc.Approve(ctx, 5, 9, "")
			Expect(err).To(MatchError(ContainSubstring("approve: add book")))
		})

		It("fails before any store write when hardcover is not configured", func() {
			store := dbmocks.NewMockStore(GinkgoT())
			store.EXPECT().GetRequest(mock.Anything, uint32(5)).
				Return(bookReq("ebook"), nil).Once()
			noMeta := request.NewService(
				store,
				reqmocks.NewMockMovieAdder(GinkgoT()),
				reqmocks.NewMockShowAdder(GinkgoT()),
				musicmocks.NewMockAdder(GinkgoT()),
				bookmocks.NewMockAdder(GinkgoT()),
				metadatamocks.NewMockMusicProvider(GinkgoT()),
				nil,
			)

			_, err := noMeta.Approve(ctx, 5, 9, "")
			Expect(err).To(MatchError(book.ErrNotConfigured))
		})
	})

	Describe("Create against a real store", func() {
		It("lets only one of two concurrent creates through", func() {
			client, err := db.Open(ctx, ":memory:")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
			store := db.New(client)
			u, err := store.CreateUser(ctx, db.CreateUserParams{
				Email:      "u@x.io",
				Role:       role.Seed(user.RoleMember),
				AuthMethod: user.AuthMethodLocal,
			})
			Expect(err).NotTo(HaveOccurred())

			movies := reqmocks.NewMockMovieAdder(GinkgoT())
			movies.EXPECT().GetByTMDBID(mock.Anything, uint32(5)).
				Return(nil, errors.New("not found")).Twice()
			gate := &sync.WaitGroup{}
			gate.Add(2)
			svc := request.NewService(
				barrierStore{Store: store, gate: gate},
				movies,
				reqmocks.NewMockShowAdder(GinkgoT()),
				musicmocks.NewMockAdder(GinkgoT()),
				bookmocks.NewMockAdder(GinkgoT()),
				metadatamocks.NewMockMusicProvider(GinkgoT()),
				metadatamocks.NewMockBookProvider(GinkgoT()),
			)

			errs := make([]error, 2)
			var wg sync.WaitGroup
			for i := range errs {
				wg.Go(func() {
					_, errs[i] = svc.Create(
						ctx,
						request.CreateParams{
							MediaType:   "movie",
							MediaID:     5,
							Title:       "Flick",
							RequesterID: u.ID,
						},
					)
				})
			}
			wg.Wait()

			Expect(errs).To(ConsistOf(BeNil(), MatchError(request.ErrDuplicate)))
			rows, total, err := store.ListRequests(ctx, db.ListRequestsParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(1))
			Expect(rows).To(HaveLen(1))
		})
	})

	Describe("Approve", func() {
		It("adds the movie then marks the request approved", func() {
			storeMk.GetRequest(mock.Anything, uint32(1)).
				Return(&ent.Request{ID: 1, MediaType: "movie", MediaID: 5}, nil).
				Twice()
			movieMk.Add(mock.Anything, uint32(5), "hd").
				Return(&ent.Movie{ID: 3}, "", nil).Once()
			storeMk.ApproveRequest(mock.Anything, uint32(1), uint32(9)).
				Return(nil).Once()

			r, err := svc.Approve(ctx, 1, 9, "hd")
			Expect(err).NotTo(HaveOccurred())
			Expect(r.ID).To(Equal(uint32(1)))
		})

		It("adds the show for tvshow requests", func() {
			storeMk.GetRequest(mock.Anything, uint32(2)).
				Return(&ent.Request{ID: 2, MediaType: "tvshow", MediaID: 8}, nil).
				Twice()
			showMk.Add(mock.Anything, uint32(8), "").
				Return(&ent.TVShow{ID: 4}, nil).Once()
			storeMk.ApproveRequest(mock.Anything, uint32(2), uint32(9)).
				Return(nil).Once()

			_, err := svc.Approve(ctx, 2, 9, "")
			Expect(err).NotTo(HaveOccurred())
		})

		It("does not approve when the add fails", func() {
			storeMk.GetRequest(mock.Anything, uint32(1)).
				Return(&ent.Request{ID: 1, MediaType: "movie", MediaID: 5}, nil).
				Once()
			movieMk.Add(mock.Anything, uint32(5), "").
				Return(nil, "", errors.New("tmdb down")).Once()

			_, err := svc.Approve(ctx, 1, 9, "")
			Expect(err).To(HaveOccurred())
		})

		It(
			"maps NotFound to request.ErrRequestNotFound without calling TMDB",
			func() {
				storeMk.GetRequest(mock.Anything, uint32(99)).
					Return(nil, &ent.NotFoundError{}).Once()

				_, err := svc.Approve(ctx, 99, 9, "")
				Expect(err).To(MatchError(request.ErrRequestNotFound))
				Expect(err).To(MatchError(ContainSubstring("request 99")))
			},
		)
	})

	Describe("Deny", func() {
		It("sets denied with a reason", func() {
			storeMk.DenyRequest(mock.Anything, uint32(1), uint32(9), "low quality").
				Return(nil).Once()
			storeMk.GetRequest(mock.Anything, uint32(1)).
				Return(&ent.Request{ID: 1}, nil).Once()

			_, err := svc.Deny(ctx, 1, 9, "low quality")
			Expect(err).NotTo(HaveOccurred())
		})

		It("maps NotFound to request.ErrRequestNotFound", func() {
			storeMk.DenyRequest(mock.Anything, uint32(99), uint32(9), "reason").
				Return(&ent.NotFoundError{}).Once()

			_, err := svc.Deny(ctx, 99, 9, "reason")
			Expect(err).To(MatchError(request.ErrRequestNotFound))
			Expect(err).To(MatchError(ContainSubstring("request 99")))
		})
	})

	Describe("Reopen", func() {
		It("reopens a denied request", func() {
			storeMk.ReopenRequest(mock.Anything, uint32(1)).
				Return(nil).Once()
			storeMk.GetRequest(mock.Anything, uint32(1)).
				Return(&ent.Request{ID: 1}, nil).Once()

			_, err := svc.Reopen(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
		})

		It("maps NotFound to request.ErrRequestNotFound", func() {
			storeMk.ReopenRequest(mock.Anything, uint32(99)).
				Return(&ent.NotFoundError{}).Once()

			_, err := svc.Reopen(ctx, 99)
			Expect(err).To(MatchError(request.ErrRequestNotFound))
			Expect(err).To(MatchError(ContainSubstring("request 99")))
		})
	})

	Describe("List", func() {
		It("passes params through to the store", func() {
			storeMk.ListRequests(mock.Anything, mock.MatchedBy(func(p db.ListRequestsParams) bool {
				return p.RequesterID == 9
			})).
				Return([]*ent.Request{{ID: 1}}, 1, nil).
				Once()

			rows, total, err := svc.List(ctx, db.ListRequestsParams{RequesterID: 9})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(1))
			Expect(rows).To(HaveLen(1))
		})
	})
})
