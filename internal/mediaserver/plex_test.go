package mediaserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func jsonBytes(v any) []byte {
	GinkgoHelper()
	b, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	return b
}

var _ = Describe("Plex Client", Label("unit", "mediaserver"), func() {
	Describe("RefreshLibrary", func() {
		It("finds matching section and triggers refresh", func() {
			var refreshCalled bool
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.Header.Get("X-Plex-Token")).To(Equal("test-token"))

					switch r.URL.Path {
					case "/library/sections":
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						_, err := w.Write(jsonBytes(map[string]any{
							"MediaContainer": map[string]any{
								"Directory": []map[string]any{
									{
										"key": "1",
										"Location": []map[string]any{
											{"path": "/media/movies"},
										},
									},
								},
							},
						}))
						Expect(err).NotTo(HaveOccurred())
					case "/library/sections/1/refresh":
						refreshCalled = true
						w.WriteHeader(http.StatusOK)
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				}),
			)
			defer ts.Close()

			client := NewPlex(ts.URL, "test-token")
			Expect(
				client.RefreshLibrary(context.Background(), "/media/movies", ""),
			).To(Succeed())
			Expect(refreshCalled).To(BeTrue())
		})

		It("returns error when sections endpoint returns 401", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}),
			)
			defer ts.Close()

			client := NewPlex(ts.URL, "bad-token")
			err := client.RefreshLibrary(context.Background(), "/media/movies", "")
			Expect(
				err,
			).To(MatchError(ContainSubstring("plex sections: unexpected status 401")))
		})

		It("returns error when sections body is malformed", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("not json"))
				}),
			)
			defer ts.Close()

			client := NewPlex(ts.URL, "test-token")
			err := client.RefreshLibrary(context.Background(), "/media/movies", "")
			Expect(err).To(MatchError(ContainSubstring("plex sections decode")))
		})

		It("returns error when no section matches the library path", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(jsonBytes(map[string]any{
						"MediaContainer": map[string]any{
							"Directory": []map[string]any{
								{
									"key": "1",
									"Location": []map[string]any{
										{"path": "/somewhere/else"},
									},
								},
							},
						},
					}))
				}),
			)
			defer ts.Close()

			client := NewPlex(ts.URL, "test-token")
			err := client.RefreshLibrary(context.Background(), "/media/movies", "")
			Expect(
				err,
			).To(MatchError(ContainSubstring("no section found for path /media/movies")))
		})

		It("returns error when refresh endpoint returns 5xx", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/library/sections":
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write(jsonBytes(map[string]any{
							"MediaContainer": map[string]any{
								"Directory": []map[string]any{
									{
										"key": "9",
										"Location": []map[string]any{
											{"path": "/media/movies"},
										},
									},
								},
							},
						}))
					case "/library/sections/9/refresh":
						w.WriteHeader(http.StatusInternalServerError)
					}
				}),
			)
			defer ts.Close()

			client := NewPlex(ts.URL, "test-token")
			err := client.RefreshLibrary(context.Background(), "/media/movies", "")
			Expect(
				err,
			).To(MatchError(ContainSubstring("plex refresh: unexpected status 500")))
		})

		It("returns error when server is unreachable", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			)
			ts.Close()

			client := NewPlex(ts.URL, "test-token")
			err := client.RefreshLibrary(context.Background(), "/media/movies", "")
			Expect(err).To(HaveOccurred())
		})

		It("returns error when ctx is canceled", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			)
			defer ts.Close()

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			client := NewPlex(ts.URL, "test-token")
			err := client.RefreshLibrary(ctx, "/media/movies", "")
			Expect(err).To(HaveOccurred())
		})

		It("uses sectionKey directly when set, skipping section discovery", func() {
			calls := []string{}
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls = append(calls, r.URL.Path)
					w.WriteHeader(http.StatusOK)
				}),
			)
			defer ts.Close()

			client := NewPlex(ts.URL, "tok")
			Expect(
				client.RefreshLibrary(context.Background(), "/ignored", "7"),
			).To(Succeed())
			Expect(calls).To(ConsistOf("/library/sections/7/refresh"))
		})
	})

	Describe("ListSections", func() {
		It("returns sections from the Plex API", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.URL.Path).To(Equal("/library/sections"))
					Expect(r.Header.Get("X-Plex-Token")).To(Equal("tok"))
					w.Header().Set("Content-Type", "application/json")
					_, err := w.Write([]byte(`{"MediaContainer":{"Directory":[
						{"key":"1","title":"Movies","type":"movie","Location":[{"path":"/data/movies"}]},
						{"key":"2","title":"Anime","type":"movie","Location":[{"path":"/data/anime"},{"path":"/extra/anime"}]}
					]}}`))
					Expect(err).NotTo(HaveOccurred())
				}),
			)
			defer ts.Close()

			client := NewPlex(ts.URL, "tok")
			got, err := client.ListSections(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(2))
			Expect(got[0].Key).To(Equal("1"))
			Expect(got[0].Name).To(Equal("Movies"))
			Expect(got[0].Type).To(Equal("movie"))
			Expect(got[0].Locations).To(ConsistOf("/data/movies"))
			Expect(got[1].Locations).To(ConsistOf("/data/anime", "/extra/anime"))
		})

		It("returns error on non-200", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}),
			)
			defer ts.Close()

			client := NewPlex(ts.URL, "bad")
			_, err := client.ListSections(context.Background())
			Expect(err).To(MatchError(ContainSubstring("unexpected status 401")))
		})

		It("returns error on malformed body", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("not json"))
				}),
			)
			defer ts.Close()

			client := NewPlex(ts.URL, "tok")
			_, err := client.ListSections(context.Background())
			Expect(err).To(MatchError(ContainSubstring("plex sections decode")))
		})
	})

	Describe("TestConnection", func() {
		It("succeeds when server responds 200", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte(`{"MediaContainer":{}}`))
					Expect(err).NotTo(HaveOccurred())
				}),
			)
			defer ts.Close()

			client := NewPlex(ts.URL, "test-token")
			Expect(client.TestConnection(context.Background())).To(Succeed())
		})

		It("fails when server returns 4xx", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				}),
			)
			defer ts.Close()

			client := NewPlex(ts.URL, "bad-token")
			err := client.TestConnection(context.Background())
			Expect(
				err,
			).To(MatchError(ContainSubstring("plex: unexpected status 403")))
		})

		It("fails when server is unreachable", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			)
			ts.Close()

			client := NewPlex(ts.URL, "tok")
			err := client.TestConnection(context.Background())
			Expect(err).To(HaveOccurred())
		})
	})
})
