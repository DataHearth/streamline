package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/auth"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	moviemocks "github.com/datahearth/streamline/internal/media/movie/mocks"
	"github.com/datahearth/streamline/internal/metadata"
	metadatamocks "github.com/datahearth/streamline/internal/metadata/mocks"
)

var _ = Describe("Full Vertical Slice", Label("unit", "server", "movies"), func() {
	var (
		ts     *httptest.Server
		movies *moviemocks.MockManager
		md     *metadatamocks.MockProvider
		store  *dbmocks.MockStore
	)

	BeforeEach(func() {
		t := GinkgoT()
		movies = moviemocks.NewMockManager(t)
		md = metadatamocks.NewMockProvider(t)
		store = dbmocks.NewMockStore(t)

		srv := New(Config{
			DB:       store,
			Movies:   movies,
			Metadata: md,
		})

		// Inject an admin identity so requireAdmin-gated handlers run (the
		// server's real auth middleware is not mounted in this slice).
		inner := srv.Router()
		handler := http.HandlerFunc(
			func(w http.ResponseWriter, req *http.Request) {
				inner.ServeHTTP(w, req.WithContext(auth.ContextWithClaims(
					req.Context(), &auth.Claims{
						UserID: 1, Email: "admin@test.com",
						Role: "admin", JTI: "admin-jti",
					}),
				))
			},
		)
		ts = httptest.NewServer(handler)
		DeferCleanup(ts.Close)
	})

	It("should add a movie, verify wanted status, and search TMDB", func() {
		fightClub := &ent.Movie{
			ID:     1,
			Title:  "Fight Club",
			Year:   1999,
			TmdbID: 550,
			Status: movie.StatusWanted,
		}
		movies.EXPECT().
			Add(mock.Anything, uint32(550), "").
			Return(fightClub, "", nil).
			Once()
		movies.EXPECT().
			Get(mock.Anything, uint32(1)).
			Return(fightClub, nil).
			Once()
		store.EXPECT().
			ListMediaFilesByMovieID(mock.Anything, uint32(1)).
			Return(nil, nil).
			Once()
		md.EXPECT().
			GetMovie(mock.Anything, uint32(550)).
			Return(&metadata.MovieDetails{}, nil).
			Once()
		md.EXPECT().
			SearchMovie(mock.Anything, "Fight Club", uint16(0)).
			Return([]metadata.MovieResult{
				{
					TMDBID:   550,
					Title:    "Fight Club",
					Year:     1999,
					Overview: "An insomniac office worker...",
				},
			}, nil).
			Once()

		By("adding a movie from TMDB")
		body := `{"tmdb_id": 550}`
		resp, err := http.Post(
			ts.URL+"/api/v1/movies",
			"application/json",
			strings.NewReader(body),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		var created map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&created)).To(Succeed())
		resp.Body.Close()

		movieID := int(created["id"].(float64))
		Expect(created["title"]).To(Equal("Fight Club"))
		Expect(created["status"]).To(Equal("wanted"))

		By("verifying movie is in wanted status via GET")
		resp, err = http.Get(fmt.Sprintf("%s/api/v1/movies/%d", ts.URL, movieID))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var fetched map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&fetched)).To(Succeed())
		resp.Body.Close()

		Expect(fetched["title"]).To(Equal("Fight Club"))
		Expect(fetched["status"]).To(Equal("wanted"))
		Expect(fetched["year"]).To(BeNumerically("==", 1999))

		By("searching TMDB for movies")
		resp, err = http.Get(ts.URL + "/api/v1/search/movie?q=Fight+Club")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var searchResults []map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&searchResults)).To(Succeed())
		resp.Body.Close()

		Expect(searchResults).To(HaveLen(1))
		Expect(searchResults[0]["title"]).To(Equal("Fight Club"))
	})

	It("should return 409 when adding a duplicate movie", func() {
		movies.EXPECT().
			Add(mock.Anything, uint32(550), "").
			Return(&ent.Movie{
				ID: 1, Title: "Fight Club", Year: 1999, TmdbID: 550,
				Status: movie.StatusWanted,
			}, "", nil).
			Once()
		movies.EXPECT().
			Add(mock.Anything, uint32(550), "").
			Return(nil, "", fmt.Errorf("movie already exists")).
			Once()

		body := `{"tmdb_id": 550}`
		resp, err := http.Post(
			ts.URL+"/api/v1/movies",
			"application/json",
			strings.NewReader(body),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		resp.Body.Close()

		resp, err = http.Post(
			ts.URL+"/api/v1/movies",
			"application/json",
			strings.NewReader(body),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		resp.Body.Close()
	})

	It("full CRUD: add, list, get, delete", func() {
		fightClub := &ent.Movie{
			ID:     1,
			Title:  "Fight Club",
			Year:   1999,
			TmdbID: 550,
			Status: movie.StatusWanted,
		}
		movies.EXPECT().
			Add(mock.Anything, uint32(550), "").
			Return(fightClub, "", nil).
			Once()
		movies.EXPECT().
			List(mock.Anything, uint16(1), uint16(10)).
			Return([]*ent.Movie{fightClub}, uint32(1), nil).
			Once()
		movies.EXPECT().
			Get(mock.Anything, uint32(1)).
			Return(fightClub, nil).
			Once()
		store.EXPECT().
			ListMediaFilesByMovieID(mock.Anything, uint32(1)).
			Return(nil, nil).
			Once()
		md.EXPECT().
			GetMovie(mock.Anything, uint32(550)).
			Return(&metadata.MovieDetails{}, nil).
			Once()
		movies.EXPECT().
			Delete(mock.Anything, uint32(1), mock.AnythingOfType("movie.DeleteOptions")).
			Return(nil).
			Once()
		movies.EXPECT().
			Get(mock.Anything, uint32(1)).
			Return(nil, fmt.Errorf("movie not found")).
			Once()
		movies.EXPECT().
			List(mock.Anything, uint16(1), uint16(10)).
			Return(nil, uint32(0), nil).
			Once()

		By("Adding a movie via POST /api/v1/movies")
		resp, err := http.Post(
			ts.URL+"/api/v1/movies",
			"application/json",
			strings.NewReader(`{"tmdb_id": 550}`),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		var created map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&created)).To(Succeed())
		resp.Body.Close()
		movieID := int(created["id"].(float64))

		By("Listing movies — should contain new movie")
		resp, err = http.Get(ts.URL + "/api/v1/movies?page=1&limit=10")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var listed struct {
			Items []map[string]any `json:"items"`
			Total int              `json:"total"`
		}
		Expect(json.NewDecoder(resp.Body).Decode(&listed)).To(Succeed())
		resp.Body.Close()
		Expect(listed.Total).To(Equal(1))
		Expect(listed.Items).To(HaveLen(1))
		Expect(listed.Items[0]["title"]).To(Equal("Fight Club"))

		By("Getting movie by ID")
		resp, err = http.Get(fmt.Sprintf("%s/api/v1/movies/%d", ts.URL, movieID))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var fetched map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&fetched)).To(Succeed())
		resp.Body.Close()
		Expect(fetched["title"]).To(Equal("Fight Club"))

		By("Deleting movie")
		req, err := http.NewRequest(
			http.MethodDelete,
			fmt.Sprintf("%s/api/v1/movies/%d", ts.URL, movieID),
			nil,
		)
		Expect(err).NotTo(HaveOccurred())
		resp, err = http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		resp.Body.Close()

		By("Verifying movie is gone via GET")
		resp, err = http.Get(fmt.Sprintf("%s/api/v1/movies/%d", ts.URL, movieID))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		resp.Body.Close()

		By("Listing movies — should be empty")
		resp, err = http.Get(ts.URL + "/api/v1/movies?page=1&limit=10")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var emptyList struct {
			Items []map[string]any `json:"items"`
			Total int              `json:"total"`
		}
		Expect(json.NewDecoder(resp.Body).Decode(&emptyList)).To(Succeed())
		resp.Body.Close()
		Expect(emptyList.Total).To(Equal(0))
	})
})
