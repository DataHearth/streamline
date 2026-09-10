package restapi

import (
	"encoding/json"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
)

var _ = Describe(
	"Handler: People",
	Label("unit", "server", "people"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			app = newAPIKeyApp()
		})

		Describe("ListPeople", func() {
			It("rejects a limit outside [1, 100] with a JSON 400", func() {
				resp := app.do(app.req(
					http.MethodGet,
					"/api/v1/people?limit=101",
					app.adminKey,
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(resp.Header.Get("Content-Type")).
					To(HavePrefix("application/json"))
				var body Error
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Message).To(Equal("limit must be between 1 and 100"))
			})

			It("rejects an explicit limit=0 with a JSON 400", func() {
				resp := app.do(app.req(
					http.MethodGet,
					"/api/v1/people?limit=0",
					app.adminKey,
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				var body Error
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Message).To(Equal("limit must be between 1 and 100"))
			})

			It("passes the folded query, limit and offset through", func() {
				app.store.EXPECT().
					ListPeople(mock.Anything, db.ListPeopleParams{
						Query:  "beatrice",
						Limit:  5,
						Offset: 10,
					}).
					Return([]db.Person{{
						ID:         7,
						TMDBID:     10,
						Name:       "Béatrice Dalle",
						ProfileURL: "https://img/bd.jpg",
						Credits:    3,
					}}, 1, nil).
					Once()

				resp := app.do(app.req(
					http.MethodGet,
					"/api/v1/people?query=beatrice&limit=5&offset=10",
					app.adminKey,
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var body PersonList
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Total).To(Equal(uint32(1)))
				Expect(body.Limit).To(Equal(uint16(5)))
				Expect(body.Offset).To(Equal(uint32(10)))
				Expect(body.Items).To(HaveLen(1))
				// id is the persons row, not the provider id: it is what
				// /people/{id} is keyed by.
				Expect(body.Items[0].Id).To(Equal(uint32(7)))
				Expect(body.Items[0].TmdbId).To(Equal(uint32(10)))
				Expect(body.Items[0].Credits).To(Equal(uint32(3)))
				Expect(body.Items[0].ProfileUrl).
					To(HaveValue(Equal("https://img/bd.jpg")))
			})

			It("keys a TVDB-only person by their row id", func() {
				// The bug this keying replaced: TVDB cast carries tmdb_id 0, so
				// every series actor in the library collapsed into one person.
				app.store.EXPECT().
					ListPeople(mock.Anything, mock.Anything).
					Return([]db.Person{
						{ID: 3, TVDBID: 511, Name: "Nina Meurisse", Credits: 1},
						{
							ID:      4,
							TVDBID:  512,
							Name:    "Zachary Chasseriaud",
							Credits: 1,
						},
					}, 2, nil).
					Once()

				resp := app.do(app.req(
					http.MethodGet,
					"/api/v1/people",
					app.adminKey,
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var body PersonList
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Items).To(HaveLen(2))
				Expect(body.Items[0].Id).To(Equal(uint32(3)))
				Expect(body.Items[0].TvdbId).To(Equal(uint32(511)))
				Expect(body.Items[0].TmdbId).To(BeZero())
				Expect(body.Items[1].Id).To(Equal(uint32(4)))
			})

			It("omits profile_url when no credit carries one", func() {
				app.store.EXPECT().
					ListPeople(mock.Anything, mock.Anything).
					Return([]db.Person{{
						TMDBID:  10,
						Name:    "Ada Lovelace",
						Credits: 1,
					}}, 1, nil).
					Once()

				resp := app.do(app.req(
					http.MethodGet,
					"/api/v1/people",
					app.adminKey,
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var raw map[string]any
				Expect(json.NewDecoder(resp.Body).Decode(&raw)).To(Succeed())
				items, _ := raw["items"].([]any)
				Expect(items).To(HaveLen(1))
				Expect(items[0]).NotTo(HaveKey("profile_url"))
			})
		})

		Describe("GetPerson", func() {
			It("renders movie and series credits with their characters", func() {
				// The path segment is the persons row id, so it is 7 that has
				// to reach the store — not the 10 the person's tmdb id is.
				app.store.EXPECT().
					PersonCredits(mock.Anything, uint32(7)).
					Return(&db.PersonCredits{
						ID:         7,
						TMDBID:     10,
						TVDBID:     511,
						Name:       "Ada Lovelace",
						ProfileURL: "https://img/ada.jpg",
						Movies: []db.MovieCredit{{
							Movie: &ent.Movie{
								ID: 1, Title: "Alpha", Year: 2020,
							},
							Character: "Herself",
						}},
						Series: []db.SeriesCredit{{
							Series: &ent.TVShow{
								ID: 2, Title: "Gamma", Year: 2021,
							},
							Character: "The Analyst",
						}},
					}, nil).
					Once()

				resp := app.do(app.req(
					http.MethodGet,
					"/api/v1/people/7",
					app.adminKey,
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var body PersonCredits
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Id).To(Equal(uint32(7)))
				Expect(body.TmdbId).To(Equal(uint32(10)))
				Expect(body.TvdbId).To(Equal(uint32(511)))
				Expect(body.Name).To(Equal("Ada Lovelace"))
				Expect(body.ProfileUrl).To(HaveValue(Equal("https://img/ada.jpg")))
				Expect(body.Movies).To(HaveLen(1))
				Expect(body.Movies[0].Movie.Title).To(Equal("Alpha"))
				Expect(body.Movies[0].Character).To(Equal("Herself"))
				Expect(body.Series).To(HaveLen(1))
				Expect(body.Series[0].Series.Title).To(Equal("Gamma"))
				Expect(body.Series[0].Character).To(Equal("The Analyst"))
			})

			It("404s a row id no person occupies", func() {
				app.store.EXPECT().
					PersonCredits(mock.Anything, uint32(999)).
					Return(nil, db.ErrPersonNotFound).
					Once()

				resp := app.do(app.req(
					http.MethodGet,
					"/api/v1/people/999",
					app.adminKey,
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				var body Error
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Message).To(Equal("person not found"))
			})
		})
	},
)
