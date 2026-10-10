package request_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/metadata"
	metadatamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/request"
	reqmocks "github.com/datahearth/streamline/internal/request/mocks"
	"github.com/datahearth/streamline/internal/role"
	"github.com/datahearth/streamline/internal/testutil/configtest"
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

const artistUUID = "5b11f4ce-a62d-471e-81fc-a69a8278c7da"

var _ = Describe("Request service", Label("unit", "request"), func() {
	var (
		ctx      context.Context
		storeMk  *dbmocks.MockStore_Expecter
		movieMk  *reqmocks.MockMovieAdder_Expecter
		showMk   *reqmocks.MockShowAdder_Expecter
		artistMk *reqmocks.MockArtistAdder_Expecter
		albumMk  *reqmocks.MockAlbumMonitor_Expecter
		bookMk   *reqmocks.MockBookAdder_Expecter
		metaMk   *metadatamocks.MockMusicProvider_Expecter
		svc      *request.Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		configtest.Setup(map[string]any{
			"quality_profiles": []map[string]any{{
				"name":                 "hd",
				"preferred_resolution": "1080p",
				"min_resolution":       "720p",
			}},
			"movie_quality_default_profile":  "hd",
			"series_quality_default_profile": "hd",
			"music_quality_profiles": []map[string]any{{
				"name": "Lossless", "tiers": []string{"lossless"},
				"preferred": "lossless",
			}},
			"music_quality_default_profile": "Lossless",
			"book_quality_profiles": []map[string]any{
				{
					"name": "Retail",
					"ebook": map[string]any{
						"formats":   []string{"EPUB"},
						"preferred": "EPUB",
					},
					"audiobook": map[string]any{
						"formats":   []string{"M4B"},
						"preferred": "M4B",
					},
				},
			},
			"book_quality_default_profiles": map[string]any{
				"novel": "Retail",
				"bd":    "Retail",
				"comic": "Retail",
				"manga": "Retail",
			},
		})
		store := dbmocks.NewMockStore(GinkgoT())
		storeMk = store.EXPECT()
		movies := reqmocks.NewMockMovieAdder(GinkgoT())
		movieMk = movies.EXPECT()
		shows := reqmocks.NewMockShowAdder(GinkgoT())
		showMk = shows.EXPECT()
		artists := reqmocks.NewMockArtistAdder(GinkgoT())
		artistMk = artists.EXPECT()
		albums := reqmocks.NewMockAlbumMonitor(GinkgoT())
		albumMk = albums.EXPECT()
		books := reqmocks.NewMockBookAdder(GinkgoT())
		bookMk = books.EXPECT()
		meta := metadatamocks.NewMockMusicProvider(GinkgoT())
		metaMk = meta.EXPECT()
		svc = request.NewService(store, movies, shows, artists, albums, books, meta)
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
		const artistMBID = "artist-1"

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

		DescribeTable("rejects an invalid identity or type before any store call",
			func(p request.CreateParams) {
				_, err := svc.Create(ctx, p)
				Expect(err).To(MatchError(request.ErrInvalidRequest))
			},
			Entry("artist without an mbid",
				request.CreateParams{MediaType: "artist", Title: "A"}),
			Entry(
				"artist with a media_id",
				request.CreateParams{
					MediaType: "artist",
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
			Entry("book without a media_id",
				request.CreateParams{MediaType: "book", Title: "B"}),
			Entry("book with an mbid",
				request.CreateParams{MediaType: "book", MediaID: 3, MediaMBID: "x"}),
			Entry("book_series without a media_id",
				request.CreateParams{MediaType: "book_series", Title: "S"}),
			Entry("album without hints",
				request.CreateParams{MediaType: "album", MediaMBID: "x"}),
			Entry("album with a media_id", request.CreateParams{
				MediaType: "album", MediaMBID: "x", MediaID: 3,
				ArtistMBID: artistUUID, ArtistName: "Nirvana",
			}),
			Entry("album with an artist_mbid that is not a UUID",
				request.CreateParams{
					MediaType: "album", MediaMBID: "x",
					ArtistMBID: "artist-1", ArtistName: "Nirvana",
				}),
			Entry("album with an empty artist name", request.CreateParams{
				MediaType: "album", MediaMBID: "x", ArtistMBID: artistUUID,
			}),
			Entry("album with an over-long artist name", request.CreateParams{
				MediaType: "album", MediaMBID: "x", ArtistMBID: artistUUID,
				ArtistName: strings.Repeat("n", 201),
			}),
			Entry("artist hints on a movie", request.CreateParams{
				MediaType: "movie", MediaID: 3, ArtistName: "Nirvana",
			}),
			Entry("the removed author type",
				request.CreateParams{MediaType: "author", MediaID: 3}),
		)
	})

	Describe("Approve music requests", func() {
		const artistMBID = "artist-1"
		artistReq := &ent.Request{ID: 1, MediaType: "artist", MediaMbid: artistMBID}

		It("adds the artist monitored all, then approves", func() {
			storeMk.GetRequest(mock.Anything, uint32(1)).
				Return(artistReq, nil).Twice()
			artistMk.AddArtist(mock.Anything, artistMBID, "all", "Lossless").
				Return(nil).Once()
			storeMk.ApproveRequest(mock.Anything, uint32(1), uint32(9)).
				Return(nil).Once()

			_, err := svc.Approve(ctx, 1, 9, "Lossless")
			Expect(err).NotTo(HaveOccurred())
		})

		It("passes an empty profile through as the family default", func() {
			storeMk.GetRequest(mock.Anything, uint32(1)).
				Return(artistReq, nil).Twice()
			artistMk.AddArtist(mock.Anything, artistMBID, "all", "").
				Return(nil).Once()
			storeMk.ApproveRequest(mock.Anything, uint32(1), uint32(9)).
				Return(nil).Once()

			_, err := svc.Approve(ctx, 1, 9, "")
			Expect(err).NotTo(HaveOccurred())
		})

		It("marks the request available when the artist already exists", func() {
			storeMk.GetRequest(mock.Anything, uint32(1)).
				Return(artistReq, nil).Twice()
			artistMk.AddArtist(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Return(fmt.Errorf("%w: mbid", request.ErrAlreadyInLibrary)).
				Once()
			storeMk.ApproveRequest(mock.Anything, uint32(1), uint32(9)).
				Return(nil).Once()
			storeMk.MarkRequestAvailable(mock.Anything, uint32(1)).
				Return(nil).Once()

			_, err := svc.Approve(ctx, 1, 9, "")
			Expect(err).NotTo(HaveOccurred())
		})

		It("does not approve when the artist add fails", func() {
			storeMk.GetRequest(mock.Anything, uint32(1)).
				Return(artistReq, nil).Once()
			artistMk.AddArtist(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Return(errors.New("musicbrainz down")).
				Once()

			_, err := svc.Approve(ctx, 1, 9, "")
			Expect(err).To(MatchError(ContainSubstring("approve: add artist")))
		})

		It("refuses a profile from another family before adding anything", func() {
			storeMk.GetRequest(mock.Anything, uint32(1)).
				Return(artistReq, nil).Once()

			_, err := svc.Approve(ctx, 1, 9, "Retail")
			Expect(err).To(MatchError(request.ErrUnknownProfile))
		})
	})

	Describe("Create album requests", func() {
		const rgMBID = "rg-1"
		params := request.CreateParams{
			MediaType: "album", MediaMBID: rgMBID, Title: "Nevermind",
			ArtistMBID: artistUUID, ArtistName: "Nirvana", RequesterID: 9,
		}

		It("persists the hints and the requester's wording", func() {
			storeMk.FindActiveRequestByMBID(mock.Anything, "album", rgMBID).
				Return(nil, nil).Once()
			storeMk.IsAlbumMonitoredByMBID(mock.Anything, rgMBID).
				Return(false, nil).Once()
			storeMk.CreateRequest(mock.Anything, mock.MatchedBy(func(p db.CreateRequestParams) bool {
				return p.MediaType == "album" && p.MediaMBID == rgMBID &&
					p.ArtistMBID == artistUUID && p.ArtistName == "Nirvana" &&
					p.RequestedAs == "Nirvana"
			})).
				Return(&ent.Request{ID: 2}, nil).
				Once()

			_, err := svc.Create(ctx, params)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects an album already monitored in the library", func() {
			storeMk.FindActiveRequestByMBID(mock.Anything, "album", rgMBID).
				Return(nil, nil).Once()
			storeMk.IsAlbumMonitoredByMBID(mock.Anything, rgMBID).
				Return(true, nil).Once()

			_, err := svc.Create(ctx, params)
			Expect(err).To(MatchError(request.ErrDuplicate))
		})

		It("lets an album that exists unmonitored be requested", func() {
			storeMk.FindActiveRequestByMBID(mock.Anything, "album", rgMBID).
				Return(nil, nil).Once()
			storeMk.IsAlbumMonitoredByMBID(mock.Anything, rgMBID).
				Return(false, nil).Once()
			storeMk.CreateRequest(mock.Anything, mock.Anything).
				Return(&ent.Request{ID: 2}, nil).Once()

			_, err := svc.Create(ctx, params)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects an active request for the same release group", func() {
			storeMk.FindActiveRequestByMBID(mock.Anything, "album", rgMBID).
				Return(&ent.Request{ID: 7}, nil).Once()

			_, err := svc.Create(ctx, params)
			Expect(err).To(MatchError(request.ErrDuplicate))
		})
	})

	Describe("Approve album requests", func() {
		const (
			rgMBID     = "rg-1"
			artistMBID = "artist-1"
		)
		albumReq := &ent.Request{
			ID: 2, MediaType: "album", MediaMbid: rgMBID,
			ArtistMbid: "liar-mbid", ArtistName: "Liar",
		}
		verified := &metadata.ReleaseGroupDetails{
			ArtistMBID: artistMBID, ArtistName: "Nirvana",
		}

		It(
			"adds a missing artist with manual monitoring and keeps only the verified pair",
			func() {
				storeMk.GetRequest(mock.Anything, uint32(2)).
					Return(albumReq, nil).
					Twice()
				metaMk.GetReleaseGroup(mock.Anything, rgMBID).
					Return(verified, nil).
					Once()
				storeMk.FindArtistByMBID(mock.Anything, artistMBID).
					Return(nil, nil).
					Once()
				artistMk.AddArtist(mock.Anything, artistMBID, "manual", "Lossless").
					Return(nil).Once()
				albumMk.MonitorAlbum(mock.Anything, artistMBID, rgMBID).
					Return(false, nil).Once()
				storeMk.ApproveAlbumRequest(
					mock.Anything, uint32(2), uint32(9), artistMBID, "Nirvana",
				).Return(nil).Once()

				_, err := svc.Approve(ctx, 2, 9, "Lossless")
				Expect(err).NotTo(HaveOccurred())
			},
		)

		It("ignores the reviewer's profile for an artist already held", func() {
			storeMk.GetRequest(mock.Anything, uint32(2)).
				Return(albumReq, nil).
				Twice()
			metaMk.GetReleaseGroup(mock.Anything, rgMBID).
				Return(verified, nil).
				Once()
			storeMk.FindArtistByMBID(mock.Anything, artistMBID).
				Return(&ent.Artist{ID: 4}, nil).Once()
			albumMk.MonitorAlbum(mock.Anything, artistMBID, rgMBID).
				Return(false, nil).Once()
			storeMk.ApproveAlbumRequest(
				mock.Anything, uint32(2), uint32(9), artistMBID, "Nirvana",
			).Return(nil).Once()

			_, err := svc.Approve(ctx, 2, 9, "Lossless")
			Expect(err).NotTo(HaveOccurred())
		})

		It(
			"marks the request available when the album is already monitored with files",
			func() {
				storeMk.GetRequest(mock.Anything, uint32(2)).
					Return(albumReq, nil).
					Twice()
				metaMk.GetReleaseGroup(mock.Anything, rgMBID).
					Return(verified, nil).
					Once()
				storeMk.FindArtistByMBID(mock.Anything, artistMBID).
					Return(&ent.Artist{ID: 4}, nil).Once()
				albumMk.MonitorAlbum(mock.Anything, artistMBID, rgMBID).
					Return(true, nil).Once()
				storeMk.ApproveAlbumRequest(
					mock.Anything, uint32(2), uint32(9), artistMBID, "Nirvana",
				).Return(nil).Once()
				storeMk.MarkRequestAvailable(mock.Anything, uint32(2)).
					Return(nil).
					Once()

				_, err := svc.Approve(ctx, 2, 9, "")
				Expect(err).NotTo(HaveOccurred())
			},
		)

		It("leaves the request pending when the album is not on the artist", func() {
			storeMk.GetRequest(mock.Anything, uint32(2)).Return(albumReq, nil).Once()
			metaMk.GetReleaseGroup(mock.Anything, rgMBID).
				Return(verified, nil).
				Once()
			storeMk.FindArtistByMBID(mock.Anything, artistMBID).
				Return(&ent.Artist{ID: 4}, nil).Once()
			albumMk.MonitorAlbum(mock.Anything, artistMBID, rgMBID).
				Return(false, fmt.Errorf("%w: %s", request.ErrAlbumNotFound, rgMBID)).
				Once()

			_, err := svc.Approve(ctx, 2, 9, "")
			Expect(err).To(MatchError(request.ErrAlbumNotFound))
		})

		It(
			"answers album not found for a release group MusicBrainz does not know",
			func() {
				storeMk.GetRequest(mock.Anything, uint32(2)).
					Return(albumReq, nil).
					Once()
				metaMk.GetReleaseGroup(mock.Anything, rgMBID).
					Return(nil, metadata.ErrNotFound).Once()

				_, err := svc.Approve(ctx, 2, 9, "")
				Expect(err).To(MatchError(request.ErrAlbumNotFound))
			},
		)

		It("refuses a profile from another family before any lookup", func() {
			storeMk.GetRequest(mock.Anything, uint32(2)).Return(albumReq, nil).Once()

			_, err := svc.Approve(ctx, 2, 9, "Retail")
			Expect(err).To(MatchError(request.ErrUnknownProfile))
		})
	})

	Describe("Create book requests", func() {
		It("persists a book request", func() {
			storeMk.FindActiveRequest(mock.Anything, "book", uint32(40)).
				Return(nil, nil).Once()
			bookMk.HasBook(mock.Anything, uint32(40)).Return(false, nil).Once()
			storeMk.CreateRequest(mock.Anything, mock.MatchedBy(func(p db.CreateRequestParams) bool {
				return p.MediaType == "book" && p.MediaID == 40
			})).
				Return(&ent.Request{ID: 3}, nil).
				Once()

			_, err := svc.Create(ctx, request.CreateParams{
				MediaType: "book", MediaID: 40, Title: "B", RequesterID: 9,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("persists a series request in its own id space", func() {
			storeMk.FindActiveRequest(mock.Anything, "book_series", uint32(40)).
				Return(nil, nil).Once()
			bookMk.HasSeries(mock.Anything, uint32(40)).Return(false, nil).Once()
			storeMk.CreateRequest(mock.Anything, mock.MatchedBy(func(p db.CreateRequestParams) bool {
				return p.MediaType == "book_series" && p.MediaID == 40
			})).
				Return(&ent.Request{ID: 4}, nil).
				Once()

			_, err := svc.Create(ctx, request.CreateParams{
				MediaType: "book_series", MediaID: 40, Title: "S", RequesterID: 9,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects a duplicate active book request", func() {
			storeMk.FindActiveRequest(mock.Anything, "book", uint32(40)).
				Return(&ent.Request{ID: 7}, nil).Once()

			_, err := svc.Create(ctx, request.CreateParams{
				MediaType: "book", MediaID: 40, Title: "B", RequesterID: 9,
			})
			Expect(err).To(MatchError(request.ErrDuplicate))
		})

		It("rejects a book already in the library", func() {
			storeMk.FindActiveRequest(mock.Anything, "book", uint32(40)).
				Return(nil, nil).Once()
			bookMk.HasBook(mock.Anything, uint32(40)).Return(true, nil).Once()

			_, err := svc.Create(ctx, request.CreateParams{
				MediaType: "book", MediaID: 40, Title: "B", RequesterID: 9,
			})
			Expect(err).To(MatchError(request.ErrDuplicate))
		})

		It("rejects a series already in the library", func() {
			storeMk.FindActiveRequest(mock.Anything, "book_series", uint32(40)).
				Return(nil, nil).Once()
			bookMk.HasSeries(mock.Anything, uint32(40)).Return(true, nil).Once()

			_, err := svc.Create(ctx, request.CreateParams{
				MediaType: "book_series", MediaID: 40, Title: "S", RequesterID: 9,
			})
			Expect(err).To(MatchError(request.ErrDuplicate))
		})
	})

	Describe("Approve book requests", func() {
		bookReq := &ent.Request{ID: 5, MediaType: "book", MediaID: 40}
		seriesReq := &ent.Request{ID: 6, MediaType: "book_series", MediaID: 50}

		It("adds the book monitored both with the reviewer's profile", func() {
			storeMk.GetRequest(mock.Anything, uint32(5)).Return(bookReq, nil).Twice()
			bookMk.AddBook(mock.Anything, uint32(40), "both", "Retail").
				Return(nil).Once()
			storeMk.ApproveRequest(mock.Anything, uint32(5), uint32(9)).
				Return(nil).Once()

			_, err := svc.Approve(ctx, 5, 9, "Retail")
			Expect(err).NotTo(HaveOccurred())
		})

		It("adds the series monitored all", func() {
			storeMk.GetRequest(mock.Anything, uint32(6)).
				Return(seriesReq, nil).
				Twice()
			bookMk.AddSeries(mock.Anything, uint32(50), "all", "").
				Return(nil).Once()
			storeMk.ApproveRequest(mock.Anything, uint32(6), uint32(9)).
				Return(nil).Once()

			_, err := svc.Approve(ctx, 6, 9, "")
			Expect(err).NotTo(HaveOccurred())
		})

		It("marks the request available when the book is already there", func() {
			storeMk.GetRequest(mock.Anything, uint32(5)).Return(bookReq, nil).Twice()
			bookMk.AddBook(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Return(fmt.Errorf("%w: id", request.ErrAlreadyInLibrary)).
				Once()
			storeMk.ApproveRequest(mock.Anything, uint32(5), uint32(9)).
				Return(nil).Once()
			storeMk.MarkRequestAvailable(mock.Anything, uint32(5)).
				Return(nil).Once()

			_, err := svc.Approve(ctx, 5, 9, "")
			Expect(err).NotTo(HaveOccurred())
		})

		It("does not approve when hardcover is not configured", func() {
			storeMk.GetRequest(mock.Anything, uint32(5)).Return(bookReq, nil).Once()
			bookMk.AddBook(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Return(book.ErrNotConfigured).
				Once()

			_, err := svc.Approve(ctx, 5, 9, "")
			Expect(err).To(MatchError(book.ErrNotConfigured))
		})

		It("refuses a music profile on a book before adding anything", func() {
			storeMk.GetRequest(mock.Anything, uint32(5)).Return(bookReq, nil).Once()

			_, err := svc.Approve(ctx, 5, 9, "Lossless")
			Expect(err).To(MatchError(request.ErrUnknownProfile))
		})

		It("refuses a profile the series family does not hold", func() {
			storeMk.GetRequest(mock.Anything, uint32(6)).
				Return(seriesReq, nil).
				Once()

			_, err := svc.Approve(ctx, 6, 9, "ghost")
			Expect(err).To(MatchError(request.ErrUnknownProfile))
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
				reqmocks.NewMockArtistAdder(GinkgoT()),
				reqmocks.NewMockAlbumMonitor(GinkgoT()),
				reqmocks.NewMockBookAdder(GinkgoT()),
				metadatamocks.NewMockMusicProvider(GinkgoT()),
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

		It("refuses a profile outside the video family before adding", func() {
			storeMk.GetRequest(mock.Anything, uint32(1)).
				Return(&ent.Request{ID: 1, MediaType: "movie", MediaID: 5}, nil).
				Once()

			_, err := svc.Approve(ctx, 1, 9, "Lossless")
			Expect(err).To(MatchError(request.ErrUnknownProfile))
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
