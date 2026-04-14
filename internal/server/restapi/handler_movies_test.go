package restapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/indexer"
	moviesvc "github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe(
	"Handler: Movies",
	Label("unit", "server", "movies"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			app = newAPIKeyApp()
		})

		Describe("ListMovies", func() {
			It("returns paginated list when movies exist", func() {
				app.movies.EXPECT().
					List(mock.Anything, uint16(1), uint16(10)).
					Return([]*ent.Movie{
						{
							ID:     1,
							Title:  "Movie A",
							Year:   2020,
							TmdbID: 100,
							Status: movie.StatusWanted,
						},
						{
							ID:     2,
							Title:  "Movie B",
							Year:   2021,
							TmdbID: 101,
							Status: movie.StatusWanted,
						},
					}, uint32(2), nil).
					Once()

				resp, err := http.Get(app.srv.URL + "/api/v1/movies?page=1&limit=10")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var body struct {
					Items []map[string]any `json:"items"`
					Total int              `json:"total"`
				}
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Items).To(HaveLen(2))
				Expect(body.Total).To(Equal(2))
			})

			It("returns empty page when no movies exist", func() {
				app.movies.EXPECT().
					List(mock.Anything, uint16(1), uint16(10)).
					Return(nil, uint32(0), nil).
					Once()

				resp, err := http.Get(app.srv.URL + "/api/v1/movies?page=1&limit=10")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var body struct {
					Items []map[string]any `json:"items"`
					Total int              `json:"total"`
				}
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Items).To(BeEmpty())
				Expect(body.Total).To(Equal(0))
			})
		})

		Describe("DeleteMovie", func() {
			It("deletes existing movie and returns 204", func() {
				app.movies.EXPECT().
					Delete(mock.Anything, uint32(1), mock.AnythingOfType("movie.DeleteOptions")).
					Return(nil).
					Once()

				req, err := http.NewRequest(
					http.MethodDelete,
					fmt.Sprintf("%s/api/v1/movies/%d", app.srv.URL, 1),
					nil,
				)
				Expect(err).NotTo(HaveOccurred())

				resp, err := http.DefaultClient.Do(req)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			})

			It("returns 404 for nonexistent movie", func() {
				app.movies.EXPECT().
					Delete(mock.Anything, uint32(999), mock.AnythingOfType("movie.DeleteOptions")).
					Return(fmt.Errorf("movie not found")).
					Once()

				req, err := http.NewRequest(
					http.MethodDelete,
					app.srv.URL+"/api/v1/movies/999",
					nil,
				)
				Expect(err).NotTo(HaveOccurred())

				resp, err := http.DefaultClient.Do(req)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})

		Describe("SearchMovie (indexer search)", func() {
			It("returns 404 when movie does not exist", func() {
				app.movies.EXPECT().
					Get(mock.Anything, uint32(999)).
					Return(nil, fmt.Errorf("not found")).
					Once()

				req, err := http.NewRequest(
					http.MethodPost,
					app.srv.URL+"/api/v1/movies/999/search",
					nil,
				)
				Expect(err).NotTo(HaveOccurred())

				resp, err := http.DefaultClient.Do(req)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})

			It("returns search results from indexer", func() {
				app.movies.EXPECT().
					Get(mock.Anything, uint32(5)).
					Return(&ent.Movie{
						ID:     5,
						Title:  "Fight Club",
						Year:   1999,
						TmdbID: 550,
						Status: movie.StatusWanted,
					}, nil).
					Once()
				app.indexers.EXPECT().
					SearchMovie(mock.Anything, []string{"Fight Club", ""}, uint32(550)).
					Return([]indexer.SearchResult{
						{
							Title:    "Fight.Club.1999.1080p.BluRay.x264",
							Download: "magnet:?xt=urn:btih:abc123",
							Size:     5_000_000_000,
							Seeders:  50,
							Leechers: 10,
						},
					}, nil).
					Once()

				req, err := http.NewRequest(
					http.MethodPost,
					fmt.Sprintf("%s/api/v1/movies/%d/search", app.srv.URL, 5),
					nil,
				)
				Expect(err).NotTo(HaveOccurred())

				resp, err := http.DefaultClient.Do(req)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var results []SearchResult
				Expect(json.NewDecoder(resp.Body).Decode(&results)).To(Succeed())
				Expect(results).To(HaveLen(1))
				Expect(results[0].Seeders).To(Equal(uint32(50)))
				Expect(results[0].Leechers).NotTo(BeNil())
				Expect(*results[0].Leechers).To(Equal(uint32(10)))
			})
		})

		Describe("GetMovie", func() {
			It("returns media_files for the movie", func() {
				const movieID uint32 = 42
				app.movies.EXPECT().
					Get(mock.Anything, movieID).
					Return(&ent.Movie{
						ID:     movieID,
						Title:  "Some Movie",
						Year:   2020,
						TmdbID: 42,
						Status: movie.StatusAvailable,
					}, nil).
					Once()
				app.store.EXPECT().
					ListMediaFilesByMovieID(mock.Anything, movieID).
					Return([]*ent.MediaFile{
						{
							ID:           1,
							Path:         "/data/movies/Some Movie (2020)/Some.Movie.2020.1080p.Remux.x264-GROUP.mkv",
							Size:         8_400_000_000,
							ReleaseGroup: "GROUP",
						},
					}, nil).
					Once()
				app.metadata.EXPECT().
					GetMovie(mock.Anything, uint32(42)).
					Return(&metadata.MovieDetails{}, nil).
					Once()

				resp, err := http.Get(
					fmt.Sprintf("%s/api/v1/movies/%d", app.srv.URL, movieID),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var body map[string]any
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				files, ok := body["media_files"].([]any)
				Expect(ok).To(BeTrue())
				Expect(files).To(HaveLen(1))
				f := files[0].(map[string]any)
				Expect(
					f["path"],
				).To(Equal("/data/movies/Some Movie (2020)/Some.Movie.2020.1080p.Remux.x264-GROUP.mkv"))
				Expect(f["parsed_source"]).To(Equal("Remux"))
				Expect(f["parsed_resolution"]).To(Equal("1080p"))
				Expect(f["release_group"]).To(Equal("GROUP"))
			})

			It("omits media_files when the movie has none", func() {
				const movieID uint32 = 43
				app.movies.EXPECT().
					Get(mock.Anything, movieID).
					Return(&ent.Movie{
						ID:     movieID,
						Title:  "Empty",
						Year:   2021,
						TmdbID: 43,
						Status: movie.StatusWanted,
					}, nil).
					Once()
				app.store.EXPECT().
					ListMediaFilesByMovieID(mock.Anything, movieID).
					Return(nil, nil).
					Once()
				app.metadata.EXPECT().
					GetMovie(mock.Anything, uint32(43)).
					Return(&metadata.MovieDetails{}, nil).
					Once()

				resp, err := http.Get(
					fmt.Sprintf("%s/api/v1/movies/%d", app.srv.URL, movieID),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				var body map[string]any
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				_, present := body["media_files"]
				Expect(present).To(BeFalse())
			})
		})

		Describe("GetMovie metadata", func() {
			It("includes genres and rating from TMDB", func() {
				const movieID uint32 = 44
				app.movies.EXPECT().
					Get(mock.Anything, movieID).
					Return(&ent.Movie{
						ID:     movieID,
						Title:  "Rated",
						Year:   2022,
						TmdbID: 77,
						Status: movie.StatusAvailable,
					}, nil).
					Once()
				app.store.EXPECT().
					ListMediaFilesByMovieID(mock.Anything, movieID).
					Return(nil, nil).
					Once()
				app.metadata.EXPECT().
					GetMovie(mock.Anything, uint32(77)).
					Return(&metadata.MovieDetails{
						Genres: []string{"Thriller", "Mystery"},
						Rating: 8.2,
					}, nil).
					Once()

				resp, err := http.Get(
					fmt.Sprintf("%s/api/v1/movies/%d", app.srv.URL, movieID),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var body map[string]any
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body["genres"]).To(ConsistOf("Thriller", "Mystery"))
				Expect(body["rating"]).To(BeNumerically("~", 8.2, 0.001))
			})
		})

		Describe("GetMovieRecommendations", func() {
			It("returns mapped TMDB recommendations", func() {
				const movieID uint32 = 42
				app.movies.EXPECT().
					Get(mock.Anything, movieID).
					Return(&ent.Movie{
						ID:     movieID,
						Title:  "Some Movie",
						Year:   2020,
						TmdbID: 99,
						Status: movie.StatusAvailable,
					}, nil).
					Once()
				app.metadata.EXPECT().
					Recommendations(mock.Anything, uint32(99)).
					Return([]metadata.MovieResult{
						{
							TMDBID:     27205,
							Title:      "Inception",
							Year:       2010,
							Overview:   "A thief.",
							PosterPath: "/inception.jpg",
						},
					}, nil).
					Once()

				resp, err := http.Get(fmt.Sprintf(
					"%s/api/v1/movies/%d/recommendations",
					app.srv.URL, movieID,
				))
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var body map[string]any
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				items, ok := body["items"].([]any)
				Expect(ok).To(BeTrue())
				Expect(items).To(HaveLen(1))
				it := items[0].(map[string]any)
				Expect(it["tmdb_id"]).To(BeEquivalentTo(27205))
				Expect(it["title"]).To(Equal("Inception"))
				Expect(it["poster_url"]).To(
					Equal("https://image.tmdb.org/t/p/w342/inception.jpg"),
				)
			})

			It("returns 404 when the movie does not exist", func() {
				const movieID uint32 = 7
				app.movies.EXPECT().
					Get(mock.Anything, movieID).
					Return(nil, errors.New("not found")).
					Once()

				resp, err := http.Get(fmt.Sprintf(
					"%s/api/v1/movies/%d/recommendations",
					app.srv.URL, movieID,
				))
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})

			It("returns 500 when TMDB lookup fails", func() {
				const movieID uint32 = 8
				app.movies.EXPECT().
					Get(mock.Anything, movieID).
					Return(&ent.Movie{
						ID:     movieID,
						Title:  "X",
						Year:   2020,
						TmdbID: 12,
						Status: movie.StatusWanted,
					}, nil).
					Once()
				app.metadata.EXPECT().
					Recommendations(mock.Anything, uint32(12)).
					Return(nil, errors.New("tmdb down")).
					Once()

				resp, err := http.Get(fmt.Sprintf(
					"%s/api/v1/movies/%d/recommendations",
					app.srv.URL, movieID,
				))
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(
					Equal(http.StatusInternalServerError),
				)
			})
		})

		Describe("PatchMovie", func() {
			It("updates status and returns 200", func() {
				const movieID uint32 = 7
				app.movies.EXPECT().
					Update(mock.Anything, movieID, mock.AnythingOfType("movie.UpdateParams")).
					Return(&ent.Movie{
						ID:     movieID,
						Title:  "Fight Club",
						Year:   1999,
						TmdbID: 550,
						Status: movie.StatusAvailable,
					}, nil).
					Once()

				body := `{"status": "available"}`
				req, err := http.NewRequest(
					http.MethodPatch,
					fmt.Sprintf("%s/api/v1/movies/%d", app.srv.URL, movieID),
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				req.Header.Set("Content-Type", "application/json")

				resp, err := http.DefaultClient.Do(req)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var updated Movie
				Expect(json.NewDecoder(resp.Body).Decode(&updated)).To(Succeed())
				Expect(string(updated.Status)).To(Equal("available"))
			})

			It("returns 404 for nonexistent movie", func() {
				app.movies.EXPECT().
					Update(mock.Anything, uint32(999), mock.AnythingOfType("movie.UpdateParams")).
					Return(nil, fmt.Errorf("movie not found")).
					Once()

				body := `{"status": "wanted"}`
				req, err := http.NewRequest(
					http.MethodPatch,
					app.srv.URL+"/api/v1/movies/999",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				req.Header.Set("Content-Type", "application/json")

				resp, err := http.DefaultClient.Do(req)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})

		Describe("DeleteMovieFile", func() {
			It("deletes a movie file and returns 204", func() {
				app.movies.EXPECT().
					DeleteFile(mock.Anything, uint32(3), uint32(7),
						moviesvc.DeleteFileOptions{RemoveTorrent: true}).
					Return(nil).Once()

				req := app.req(http.MethodDelete, "/api/v1/movies/3/files/7",
					app.adminKey, strings.NewReader(`{"remove_torrent":true}`))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			})

			It(
				"defaults remove_torrent to false with no body and maps errors to 404",
				func() {
					app.movies.EXPECT().
						DeleteFile(mock.Anything, uint32(3), uint32(9),
							moviesvc.DeleteFileOptions{RemoveTorrent: false}).
						Return(errors.New("media file 9 not found")).Once()

					req := app.req(http.MethodDelete, "/api/v1/movies/3/files/9",
						app.adminKey, nil)
					resp := app.do(req)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				},
			)
		})
	},
)
