package mediaserver

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Jellyfin Client", Label("unit", "mediaserver"), func() {
	Describe("RefreshLibrary", func() {
		It("triggers library refresh on 204", func() {
			var refreshCalled bool
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.Header.Get("X-Emby-Token")).To(Equal("test-key"))
					Expect(r.URL.Path).To(Equal("/Library/Refresh"))
					Expect(r.Method).To(Equal(http.MethodPost))
					refreshCalled = true
					w.WriteHeader(http.StatusNoContent)
				}),
			)
			defer ts.Close()

			client := NewJellyfin(ts.URL, "test-key")
			Expect(
				client.RefreshLibrary(context.Background(), "/media/movies", ""),
			).To(Succeed())
			Expect(refreshCalled).To(BeTrue())
		})

		It("accepts 200 OK as success", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			)
			defer ts.Close()

			client := NewJellyfin(ts.URL, "test-key")
			Expect(
				client.RefreshLibrary(context.Background(), "/x", ""),
			).To(Succeed())
		})

		It("returns error on 401", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}),
			)
			defer ts.Close()

			client := NewJellyfin(ts.URL, "bad-key")
			err := client.RefreshLibrary(context.Background(), "/x", "")
			Expect(
				err,
			).To(MatchError(ContainSubstring("jellyfin refresh: unexpected status 401")))
		})

		It("returns error on 5xx", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusBadGateway)
				}),
			)
			defer ts.Close()

			client := NewJellyfin(ts.URL, "tok")
			err := client.RefreshLibrary(context.Background(), "/x", "")
			Expect(
				err,
			).To(MatchError(ContainSubstring("jellyfin refresh: unexpected status 502")))
		})

		It("returns error when server unreachable", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			)
			ts.Close()

			client := NewJellyfin(ts.URL, "tok")
			err := client.RefreshLibrary(context.Background(), "/x", "")
			Expect(err).To(HaveOccurred())
		})

		It("returns error when ctx is canceled", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				}),
			)
			defer ts.Close()

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			client := NewJellyfin(ts.URL, "tok")
			Expect(client.RefreshLibrary(ctx, "/x", "")).To(HaveOccurred())
		})
	})

	Describe("TestConnection", func() {
		It("succeeds when server responds 200", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			)
			defer ts.Close()

			client := NewJellyfin(ts.URL, "test-key")
			Expect(client.TestConnection(context.Background())).To(Succeed())
		})

		It("fails on 401", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}),
			)
			defer ts.Close()

			client := NewJellyfin(ts.URL, "bad")
			err := client.TestConnection(context.Background())
			Expect(
				err,
			).To(MatchError(ContainSubstring("jellyfin: unexpected status 401")))
		})

		It("fails when server unreachable", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			)
			ts.Close()

			client := NewJellyfin(ts.URL, "tok")
			err := client.TestConnection(context.Background())
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("MovieDeepLink", func() {
		// Jellyfin silently ignores AnyProviderIdEquals and returns the first
		// library item, so an item whose ProviderIds don't carry the requested
		// TMDB id must be rejected rather than deep-linked to the wrong movie.
		It(
			"rejects the wrong item returned when the provider filter is ignored",
			func() {
				ts := httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Query().Get("AnyProviderIdEquals") != "" {
							_, _ = w.Write([]byte(
								`{"Items":[{"Id":"wrongid","Type":"Movie","ProviderIds":{"Tmdb":"999"}}]}`,
							))
							return
						}
						_, _ = w.Write([]byte(`{"Items":[]}`))
					}),
				)
				defer ts.Close()

				_, err := NewJellyfin(ts.URL, "tok").
					MovieDeepLink(context.Background(), "", 123, "Title", 2024)
				Expect(err).To(MatchError(ErrMovieNotFound))
			},
		)

		It("deep-links the item whose ProviderIds match the TMDB id", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(
						`{"Items":[{"Id":"rightid","Type":"Movie","ProviderIds":{"Tmdb":"123"}}]}`,
					))
				}),
			)
			defer ts.Close()

			link, err := NewJellyfin(ts.URL, "tok").
				MovieDeepLink(context.Background(), "", 123, "Title", 2024)
			Expect(err).NotTo(HaveOccurred())
			Expect(link).To(ContainSubstring("id=rightid"))
		})

		It("falls back to title search when no provider id matches", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Query().Get("SearchTerm") != "" {
						_, _ = w.Write([]byte(
							`{"Items":[{"Id":"titlehit","Type":"Movie"}]}`,
						))
						return
					}
					_, _ = w.Write([]byte(`{"Items":[]}`))
				}),
			)
			defer ts.Close()

			link, err := NewJellyfin(ts.URL, "tok").
				MovieDeepLink(context.Background(), "", 0, "Title", 2024)
			Expect(err).NotTo(HaveOccurred())
			Expect(link).To(ContainSubstring("id=titlehit"))
		})
	})
})
