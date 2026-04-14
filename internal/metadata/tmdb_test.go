package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func jsonBytes(v any) []byte {
	GinkgoHelper()
	b, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	return b
}

func newTestTMDB(url string) *TMDB {
	client := NewTMDB()
	client.BaseURL = url
	return client
}

var _ = Describe("TMDB Client", Label("unit", "metadata"), func() {
	var (
		ts     *httptest.Server
		client Provider
	)

	Describe("SearchMovie", func() {
		BeforeEach(func() {
			ts = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.URL.Path).To(Equal("/3/search/movie"))
					Expect(r.URL.Query().Get("query")).To(Equal("interstellar"))
					Expect(
						r.Header.Get("Authorization"),
					).To(Equal("Bearer test-key"))

					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(jsonBytes(map[string]any{
						"results": []map[string]any{
							{
								"id":           157336,
								"title":        "Interstellar",
								"release_date": "2014-11-05",
								"overview":     "A team of explorers travel through a wormhole.",
								"poster_path":  "/abc.jpg",
							},
						},
					}))
				}),
			)
			client = newTestTMDB(ts.URL)
			DeferCleanup(func() { ts.Close() })
		})

		It("should parse search results correctly", func() {
			results, err := client.SearchMovie(
				context.Background(),
				"interstellar",
				0,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].TMDBID).To(Equal(uint32(157336)))
			Expect(results[0].Title).To(Equal("Interstellar"))
			Expect(results[0].Year).To(Equal(uint16(2014)))
			Expect(results[0].PosterPath).To(Equal("/abc.jpg"))
		})

		It("should pass year filter when provided", func() {
			ts.Close()
			ts = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.URL.Query().Get("year")).To(Equal("2014"))
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(jsonBytes(map[string]any{"results": []any{}}))
				}),
			)
			client = newTestTMDB(ts.URL)

			_, err := client.SearchMovie(context.Background(), "interstellar", 2014)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("with a non-default language", func() {
			var langCalls int

			BeforeEach(func() {
				configtest.Setup(map[string]any{
					"metadata": map[string]any{
						"tmdb_api_key": "test-key",
						"language":     "fr",
					},
				})
				langCalls = 0
				ts.Close()
				ts = httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						langCalls++
						Expect(r.URL.Path).To(Equal("/3/search/movie"))
						Expect(r.URL.Query().Get("language")).To(Equal("fr"))
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write(jsonBytes(map[string]any{
							"results": []map[string]any{
								{
									"id":           157336,
									"title":        "Interstellar",
									"release_date": "2014-11-05",
									"overview":     "Une équipe d'explorateurs.",
									"poster_path":  "/abc.jpg",
								},
							},
						}))
					}),
				)
				client = newTestTMDB(ts.URL)
			})

			It(
				"sends language=fr and makes a single round trip when all rows are populated",
				func() {
					results, err := client.SearchMovie(
						context.Background(),
						"interstellar",
						0,
					)
					Expect(err).NotTo(HaveOccurred())
					Expect(results).To(HaveLen(1))
					Expect(
						results[0].Overview,
					).To(Equal("Une équipe d'explorateurs."))
					Expect(langCalls).To(Equal(1))
				},
			)

			It(
				"falls back to original_title when primary title is empty and does not issue a second call",
				func() {
					ts.Close()
					calls := 0
					ts = httptest.NewServer(
						http.HandlerFunc(
							func(w http.ResponseWriter, r *http.Request) {
								calls++
								Expect(r.URL.Query().Get("language")).To(Equal("fr"))
								w.Header().Set("Content-Type", "application/json")
								_, _ = w.Write(jsonBytes(map[string]any{
									"results": []map[string]any{
										{
											"id":             157336,
											"title":          "",
											"original_title": "Interstellar",
											"release_date":   "2014-11-05",
											"overview":       "",
											"poster_path":    "/abc.jpg",
										},
									},
								}))
							},
						),
					)
					client = newTestTMDB(ts.URL)

					results, err := client.SearchMovie(
						context.Background(),
						"interstellar",
						0,
					)
					Expect(err).NotTo(HaveOccurred())
					Expect(calls).To(Equal(1))
					Expect(results).To(HaveLen(1))
					Expect(results[0].Title).To(Equal("Interstellar"))
					Expect(results[0].Overview).To(Equal(""))
				},
			)
		})

		Context("with language set to en", func() {
			var calls int

			BeforeEach(func() {
				configtest.Setup(map[string]any{
					"metadata": map[string]any{
						"tmdb_api_key": "test-key",
						"language":     "en",
					},
				})
				calls = 0
				ts.Close()
				ts = httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls++
						Expect(r.URL.Query().Get("language")).To(Equal("en"))
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write(jsonBytes(map[string]any{
							"results": []map[string]any{
								{
									"id":           157336,
									"title":        "Interstellar",
									"release_date": "2014-11-05",
									"overview":     "",
									"poster_path":  "/abc.jpg",
								},
							},
						}))
					}),
				)
				client = newTestTMDB(ts.URL)
			})

			It("never issues a fallback even when fields are empty", func() {
				results, err := client.SearchMovie(
					context.Background(),
					"interstellar",
					0,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(calls).To(Equal(1))
				Expect(results[0].Overview).To(Equal(""))
			})
		})

		Context("with language explicitly empty", func() {
			var calls int

			BeforeEach(func() {
				configtest.Setup(map[string]any{
					"metadata": map[string]any{
						"tmdb_api_key": "test-key",
						"language":     "",
					},
				})
				calls = 0
				ts.Close()
				ts = httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls++
						_, exists := r.URL.Query()["language"]
						Expect(exists).To(BeFalse())
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write(jsonBytes(map[string]any{
							"results": []map[string]any{
								{
									"id":           157336,
									"title":        "",
									"release_date": "2014-11-05",
									"overview":     "",
									"poster_path":  "/abc.jpg",
								},
							},
						}))
					}),
				)
				client = newTestTMDB(ts.URL)
			})

			It("omits the language param and does not invoke the fallback", func() {
				results, err := client.SearchMovie(
					context.Background(),
					"interstellar",
					0,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(calls).To(Equal(1))
				Expect(results).To(HaveLen(1))
			})
		})

		Context("OriginalTitle field", func() {
			It("populates OriginalTitle from the TMDB response", func() {
				ts.Close()
				ts = httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						Expect(r.URL.Query().Get("query")).To(Equal("asterix"))
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write(jsonBytes(map[string]any{
							"results": []map[string]any{
								{
									"id":             638974,
									"title":          "Asterix & Obelix: The Middle Kingdom",
									"original_title": "Astérix et Obélix : L'Empire du Milieu",
									"release_date":   "2023-02-01",
								},
							},
						}))
					}),
				)
				client = newTestTMDB(ts.URL)

				results, err := client.SearchMovie(
					context.Background(),
					"asterix",
					0,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(results).To(HaveLen(1))
				Expect(
					results[0].Title,
				).To(Equal("Asterix & Obelix: The Middle Kingdom"))
				Expect(
					results[0].OriginalTitle,
				).To(Equal("Astérix et Obélix : L'Empire du Milieu"))
			})

			It(
				"falls back to localized Title when TMDB returns empty original_title",
				func() {
					ts.Close()
					ts = httptest.NewServer(
						http.HandlerFunc(
							func(w http.ResponseWriter, r *http.Request) {
								w.Header().Set("Content-Type", "application/json")
								_, _ = w.Write(jsonBytes(map[string]any{
									"results": []map[string]any{
										{
											"id":             1,
											"title":          "Local Only",
											"original_title": "",
											"release_date":   "2024-01-01",
										},
									},
								}))
							},
						),
					)
					client = newTestTMDB(ts.URL)

					results, err := client.SearchMovie(
						context.Background(),
						"local",
						0,
					)
					Expect(err).NotTo(HaveOccurred())
					Expect(results).To(HaveLen(1))
					Expect(results[0].Title).To(Equal("Local Only"))
					Expect(results[0].OriginalTitle).To(Equal("Local Only"))
				},
			)
		})
	})

	Describe("GetMovie", func() {
		BeforeEach(func() {
			ts = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.URL.Path).To(Equal("/3/movie/157336"))

					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(jsonBytes(map[string]any{
						"id":           157336,
						"title":        "Interstellar",
						"release_date": "2014-11-05",
						"overview":     "A team of explorers travel through a wormhole.",
						"poster_path":  "/abc.jpg",
						"genres": []map[string]any{
							{"id": 12, "name": "Adventure"},
							{"id": 18, "name": "Drama"},
						},
						"runtime":      169,
						"vote_average": 8.4,
					}))
				}),
			)
			client = newTestTMDB(ts.URL)
			DeferCleanup(func() { ts.Close() })
		})

		It("should return full movie details", func() {
			details, err := client.GetMovie(context.Background(), 157336)
			Expect(err).NotTo(HaveOccurred())
			Expect(details.Title).To(Equal("Interstellar"))
			Expect(details.Year).To(Equal(uint16(2014)))
			Expect(details.PosterPath).To(Equal("/abc.jpg"))
			Expect(details.Genres).To(ConsistOf("Adventure", "Drama"))
			Expect(details.Runtime).To(Equal(uint16(169)))
			Expect(details.Rating).To(BeNumerically("~", 8.4, 0.001))
		})

		Context("with a non-default language", func() {
			BeforeEach(func() {
				configtest.Setup(map[string]any{
					"metadata": map[string]any{
						"tmdb_api_key": "test-key",
						"language":     "fr",
					},
				})
				ts.Close()
				ts = httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						Expect(r.URL.Path).To(Equal("/3/movie/157336"))
						Expect(r.URL.Query().Get("language")).To(Equal("fr"))
						Expect(
							r.URL.Query().Get("append_to_response"),
						).To(Equal("translations,credits"))

						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write(jsonBytes(map[string]any{
							"id":             157336,
							"title":          "Interstellaire",
							"original_title": "Interstellar",
							"release_date":   "2014-11-05",
							"overview":       "Une équipe d'explorateurs.",
							"poster_path":    "/abc.jpg",
							"genres": []map[string]any{
								{"id": 12, "name": "Aventure"},
							},
							"runtime": 169,
							"translations": map[string]any{
								"translations": []map[string]any{},
							},
						}))
					}),
				)
				client = newTestTMDB(ts.URL)
			})

			It(
				"forwards language=fr and append_to_response=translations,credits",
				func() {
					details, err := client.GetMovie(context.Background(), 157336)
					Expect(err).NotTo(HaveOccurred())
					Expect(details.Title).To(Equal("Interstellaire"))
				},
			)

			It(
				"uses the original_language translation when the primary title is empty",
				func() {
					ts.Close()
					ts = httptest.NewServer(
						http.HandlerFunc(
							func(w http.ResponseWriter, r *http.Request) {
								w.Header().Set("Content-Type", "application/json")
								_, _ = w.Write(jsonBytes(map[string]any{
									"id":                157336,
									"title":             "",
									"original_title":    "Interstellar",
									"original_language": "en",
									"release_date":      "2014-11-05",
									"overview":          "",
									"poster_path":       "/abc.jpg",
									"genres":            []map[string]any{},
									"runtime":           169,
									"translations": map[string]any{
										"translations": []map[string]any{
											{
												"iso_639_1": "en",
												"data": map[string]any{
													"title":    "Interstellar (EN)",
													"overview": "A team of explorers travel through a wormhole.",
												},
											},
										},
									},
								}))
							},
						),
					)
					client = newTestTMDB(ts.URL)

					details, err := client.GetMovie(context.Background(), 157336)
					Expect(err).NotTo(HaveOccurred())
					Expect(details.Title).To(Equal("Interstellar (EN)"))
					Expect(
						details.Overview,
					).To(Equal("A team of explorers travel through a wormhole."))
				},
			)

			It(
				"falls back to original_title when both primary and original-language translation titles are empty",
				func() {
					ts.Close()
					ts = httptest.NewServer(
						http.HandlerFunc(
							func(w http.ResponseWriter, r *http.Request) {
								w.Header().Set("Content-Type", "application/json")
								_, _ = w.Write(jsonBytes(map[string]any{
									"id":                157336,
									"title":             "",
									"original_title":    "Interstellar",
									"original_language": "en",
									"release_date":      "2014-11-05",
									"overview":          "",
									"poster_path":       "/abc.jpg",
									"genres":            []map[string]any{},
									"runtime":           169,
									"translations": map[string]any{
										"translations": []map[string]any{
											{
												"iso_639_1": "en",
												"data": map[string]any{
													"title":    "",
													"overview": "",
												},
											},
										},
									},
								}))
							},
						),
					)
					client = newTestTMDB(ts.URL)

					details, err := client.GetMovie(context.Background(), 157336)
					Expect(err).NotTo(HaveOccurred())
					Expect(details.Title).To(Equal("Interstellar"))
					Expect(details.Overview).To(Equal(""))
				},
			)
		})
	})

	Describe("Recommendations", func() {
		It("maps TMDB recommendations and sends path, auth and language", func() {
			ts = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.URL.Path).
						To(Equal("/3/movie/157336/recommendations"))
					Expect(r.Header.Get("Authorization")).
						To(Equal("Bearer test-key"))
					Expect(r.URL.Query().Get("language")).To(Equal("en"))
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(jsonBytes(map[string]any{
						"results": []map[string]any{
							{
								"id":           27205,
								"title":        "Inception",
								"release_date": "2010-07-16",
								"overview":     "A thief who steals corporate secrets.",
								"poster_path":  "/inception.jpg",
							},
							{
								"id":             49026,
								"title":          "",
								"original_title": "The Dark Knight Rises",
								"release_date":   "2012-07-20",
								"poster_path":    "/tdkr.jpg",
							},
						},
					}))
				}),
			)
			DeferCleanup(func() { ts.Close() })
			client = newTestTMDB(ts.URL)

			results, err := client.Recommendations(
				context.Background(),
				157336,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(2))
			Expect(results[0].TMDBID).To(Equal(uint32(27205)))
			Expect(results[0].Title).To(Equal("Inception"))
			Expect(results[0].Year).To(Equal(uint16(2010)))
			Expect(results[0].PosterPath).To(Equal("/inception.jpg"))
			// Falls back to original_title when the localized title is empty.
			Expect(results[1].Title).To(Equal("The Dark Knight Rises"))
		})
	})

	Describe("error paths", func() {
		It("returns error on 401 from search endpoint", func() {
			ts = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}),
			)
			DeferCleanup(func() { ts.Close() })
			client = newTestTMDB(ts.URL)
			_, err := client.SearchMovie(context.Background(), "x", 0)
			Expect(err).To(MatchError(ContainSubstring("tmdb search")))
			Expect(err).To(MatchError(ContainSubstring("unexpected status 401")))
		})

		It("returns error on 404 from get movie endpoint", func() {
			ts = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}),
			)
			DeferCleanup(func() { ts.Close() })
			client = newTestTMDB(ts.URL)
			_, err := client.GetMovie(context.Background(), 1)
			Expect(err).To(MatchError(ContainSubstring("tmdb get movie")))
			Expect(err).To(MatchError(ContainSubstring("unexpected status 404")))
		})

		It("returns error on 5xx from recommendations endpoint", func() {
			ts = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}),
			)
			DeferCleanup(func() { ts.Close() })
			client = newTestTMDB(ts.URL)
			_, err := client.Recommendations(context.Background(), 1)
			Expect(err).To(MatchError(ContainSubstring("tmdb recommendations")))
			Expect(err).To(MatchError(ContainSubstring("unexpected status 500")))
		})

		It("returns error on 5xx from search endpoint", func() {
			ts = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}),
			)
			DeferCleanup(func() { ts.Close() })
			client = newTestTMDB(ts.URL)
			_, err := client.SearchMovie(context.Background(), "x", 0)
			Expect(err).To(MatchError(ContainSubstring("unexpected status 500")))
		})

		It("returns error on malformed JSON", func() {
			ts = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("not json"))
				}),
			)
			DeferCleanup(func() { ts.Close() })
			client = newTestTMDB(ts.URL)
			_, err := client.SearchMovie(context.Background(), "x", 0)
			Expect(err).To(HaveOccurred())
		})

		It("returns error when server unreachable", func() {
			ts = httptest.NewServer(
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			)
			ts.Close()
			client = newTestTMDB(ts.URL)
			_, err := client.SearchMovie(context.Background(), "x", 0)
			Expect(err).To(HaveOccurred())
		})

		It("returns error when ctx is canceled", func() {
			ts = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			)
			DeferCleanup(func() { ts.Close() })
			client = newTestTMDB(ts.URL)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := client.SearchMovie(ctx, "x", 0)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("FetchDigitalRelease", func() {
		It(
			"returns the earliest digital-type release date for the requested region",
			func() {
				ts = httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						Expect(r.URL.Path).To(Equal("/3/movie/550/release_dates"))
						Expect(
							r.Header.Get("Authorization"),
						).To(Equal("Bearer test-key"))
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write(jsonBytes(map[string]any{
							"results": []map[string]any{
								{
									"iso_3166_1": "FR",
									"release_dates": []map[string]any{
										{
											"type":         3,
											"release_date": "2024-09-01T00:00:00.000Z",
										},
									},
								},
								{
									"iso_3166_1": "US",
									"release_dates": []map[string]any{
										{
											"type":         3,
											"release_date": "2024-09-15T00:00:00.000Z",
										},
										{
											"type":         4,
											"release_date": "2024-12-15T00:00:00.000Z",
										},
										{
											"type":         4,
											"release_date": "2024-12-01T00:00:00.000Z",
										},
									},
								},
							},
						}))
					}),
				)
				DeferCleanup(func() { ts.Close() })
				client = newTestTMDB(ts.URL)

				date, err := client.FetchDigitalRelease(
					context.Background(),
					550,
					"US",
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(date).NotTo(BeNil())
				Expect(
					date.UTC(),
				).To(Equal(time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)))
			},
		)

		It("returns nil when region has no digital entry", func() {
			ts = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(jsonBytes(map[string]any{
						"results": []map[string]any{
							{
								"iso_3166_1": "US",
								"release_dates": []map[string]any{
									{
										"type":         3,
										"release_date": "2024-09-15T00:00:00.000Z",
									},
								},
							},
						},
					}))
				}),
			)
			DeferCleanup(func() { ts.Close() })
			client = newTestTMDB(ts.URL)

			date, err := client.FetchDigitalRelease(
				context.Background(),
				1,
				"US",
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(date).To(BeNil())
		})

		It("returns nil when region is not present in the response", func() {
			ts = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(jsonBytes(map[string]any{
						"results": []map[string]any{
							{
								"iso_3166_1": "FR",
								"release_dates": []map[string]any{
									{
										"type":         4,
										"release_date": "2024-12-01T00:00:00.000Z",
									},
								},
							},
						},
					}))
				}),
			)
			DeferCleanup(func() { ts.Close() })
			client = newTestTMDB(ts.URL)

			date, err := client.FetchDigitalRelease(
				context.Background(),
				1,
				"US",
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(date).To(BeNil())
		})

		It("returns an error on HTTP 5xx", func() {
			ts = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}),
			)
			DeferCleanup(func() { ts.Close() })
			client = newTestTMDB(ts.URL)

			_, err := client.FetchDigitalRelease(
				context.Background(),
				1,
				"US",
			)
			Expect(err).To(MatchError(ContainSubstring("tmdb release_dates")))
			Expect(err).To(MatchError(ContainSubstring("unexpected status 500")))
		})

		It("matches the region case-insensitively", func() {
			ts = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(jsonBytes(map[string]any{
						"results": []map[string]any{
							{
								"iso_3166_1": "us",
								"release_dates": []map[string]any{
									{
										"type":         4,
										"release_date": "2024-12-01T00:00:00.000Z",
									},
								},
							},
						},
					}))
				}),
			)
			DeferCleanup(func() { ts.Close() })
			client = newTestTMDB(ts.URL)

			date, err := client.FetchDigitalRelease(
				context.Background(),
				1,
				"US",
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(date).NotTo(BeNil())
		})
	})

	Describe("PosterURL", func() {
		It("returns empty when path is empty", func() {
			Expect(PosterURL("", "w185")).To(Equal(""))
		})
		It("prefixes the CDN base and size", func() {
			Expect(
				PosterURL("/abc.jpg", "w185"),
			).To(Equal("https://image.tmdb.org/t/p/w185/abc.jpg"))
		})
	})
})
