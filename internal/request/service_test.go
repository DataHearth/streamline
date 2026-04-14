package request

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	reqmocks "github.com/datahearth/streamline/internal/request/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

var _ = Describe("Request service", Label("unit", "request"), func() {
	var (
		ctx     context.Context
		storeMk *dbmocks.MockStore_Expecter
		movieMk *reqmocks.MockMovieAdder_Expecter
		showMk  *reqmocks.MockShowAdder_Expecter
		svc     *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		store := dbmocks.NewMockStore(GinkgoT())
		storeMk = store.EXPECT()
		movies := reqmocks.NewMockMovieAdder(GinkgoT())
		movieMk = movies.EXPECT()
		shows := reqmocks.NewMockShowAdder(GinkgoT())
		showMk = shows.EXPECT()
		svc = NewService(store, movies, shows)
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

			r, err := svc.Create(ctx, "movie", 5, "Flick", 9)
			Expect(err).NotTo(HaveOccurred())
			Expect(r.ID).To(Equal(uint32(1)))
		})

		It("rejects duplicates when an active request exists", func() {
			storeMk.FindActiveRequest(mock.Anything, "movie", uint32(5)).
				Return(&ent.Request{ID: 7}, nil).Once()

			_, err := svc.Create(ctx, "movie", 5, "Flick", 9)
			Expect(err).To(MatchError(ErrDuplicate))
		})

		It("rejects when the movie is already in the library", func() {
			storeMk.FindActiveRequest(mock.Anything, "movie", uint32(5)).
				Return(nil, nil).Once()
			movieMk.GetByTMDBID(mock.Anything, uint32(5)).
				Return(&ent.Movie{ID: 3}, nil).Once()

			_, err := svc.Create(ctx, "movie", 5, "Flick", 9)
			Expect(err).To(MatchError(ErrDuplicate))
		})

		It("rejects when the show is already in the library", func() {
			storeMk.FindActiveRequest(mock.Anything, "tvshow", uint32(8)).
				Return(nil, nil).Once()
			storeMk.FindTVShowByTVDBID(mock.Anything, uint32(8)).
				Return(&ent.TVShow{ID: 2}, nil).Once()

			_, err := svc.Create(ctx, "tvshow", 8, "Show", 9)
			Expect(err).To(MatchError(ErrDuplicate))
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
