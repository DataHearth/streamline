package mediaserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Manager", Label("unit", "mediaserver"), func() {
	var mgr Manager

	BeforeEach(func() {
		mgr = New()
	})

	Describe("Test", func() {
		It("returns nil when client TestConnection succeeds", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"MediaContainer":{}}`))
				}),
			)
			defer ts.Close()

			Expect(mgr.Test(context.Background(), TestParams{
				ServerType: "plex",
				Host:       ts.URL,
				APIKey:     "k",
			})).To(Succeed())
		})

		It("returns wrapped ErrTestFailed when client errors", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}),
			)
			defer ts.Close()

			err := mgr.Test(context.Background(), TestParams{
				ServerType: "jellyfin",
				Host:       ts.URL,
				APIKey:     "k",
			})
			Expect(errors.Is(err, ErrTestFailed)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("401"))
		})

		It("rejects unknown server_type", func() {
			err := mgr.Test(context.Background(), TestParams{
				ServerType: "zzz",
				Host:       "http://x",
				APIKey:     "k",
			})
			Expect(errors.Is(err, ErrInvalidServerType)).To(BeTrue())
		})
	})

	Describe("DiscoverSections", func() {
		It("returns sections from Plex", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[
					{"key":"1","title":"Movies","type":"movie","Location":[{"path":"/m"}]}
				]}}`))
				}),
			)
			defer ts.Close()

			got, err := mgr.DiscoverSections(context.Background(), TestParams{
				ServerType: "plex",
				Host:       ts.URL,
				APIKey:     "k",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(1))
			Expect(got[0].Key).To(Equal("1"))
		})

		It("returns nil for Jellyfin/Emby (no sections concept)", func() {
			got, err := mgr.DiscoverSections(context.Background(), TestParams{
				ServerType: "jellyfin",
				Host:       "http://jf",
				APIKey:     "k",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil())
		})
	})

	Describe("TestByName", func() {
		It("loads the entry and calls TestConnection", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"MediaContainer":{}}`))
				}),
			)
			defer ts.Close()

			configtest.Setup(mediaServerConfig(map[string]any{
				"name": "home", "server_type": "plex",
				"host": ts.URL, "api_key": "k", "enabled": true,
			}))

			Expect(mgr.TestByName(context.Background(), "home")).To(Succeed())
		})

		It("returns ErrServerNotFound when entry missing", func() {
			configtest.Setup()
			err := mgr.TestByName(context.Background(), "ghost")
			Expect(errors.Is(err, ErrServerNotFound)).To(BeTrue())
		})

		It("wraps client errors as ErrTestFailed", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				}),
			)
			defer ts.Close()

			configtest.Setup(mediaServerConfig(map[string]any{
				"name": "jelly", "server_type": "jellyfin",
				"host": ts.URL, "api_key": "k", "enabled": true,
			}))

			err := mgr.TestByName(context.Background(), "jelly")
			Expect(errors.Is(err, ErrTestFailed)).To(BeTrue())
		})
	})

	Describe("DiscoverSectionsByName", func() {
		It("discovers against the entry's stored token", func() {
			var gotToken string
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotToken = r.Header.Get("X-Plex-Token")
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(
						`{"MediaContainer":{"Directory":[` +
							`{"key":"1","title":"Movies","type":"movie"}]}}`,
					))
				}),
			)
			defer ts.Close()

			configtest.Setup(mediaServerConfig(map[string]any{
				"name": "home", "server_type": "plex",
				"host": ts.URL, "api_key": "stored-token", "enabled": true,
			}))

			got, err := mgr.DiscoverSectionsByName(context.Background(), "home")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(1))
			Expect(got[0].Key).To(Equal("1"))
			// The browser never receives a saved server's token, so the stored one
			// is the only thing that can authenticate this call.
			Expect(gotToken).To(Equal("stored-token"))
		})

		It("returns ErrServerNotFound when entry missing", func() {
			configtest.Setup()
			_, err := mgr.DiscoverSectionsByName(context.Background(), "ghost")
			Expect(errors.Is(err, ErrServerNotFound)).To(BeTrue())
		})

		It("returns nothing for a non-plex entry", func() {
			configtest.Setup(mediaServerConfig(map[string]any{
				"name": "jelly", "server_type": "jellyfin",
				"host": "http://jf", "api_key": "k", "enabled": true,
			}))

			got, err := mgr.DiscoverSectionsByName(context.Background(), "jelly")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil())
		})
	})

	Describe("TestByName errors", func() {
		It("wraps client errors as ErrTestFailed", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				}),
			)
			defer ts.Close()

			configtest.Setup(mediaServerConfig(map[string]any{
				"name": "jelly", "server_type": "jellyfin",
				"host": ts.URL, "api_key": "k", "enabled": true,
			}))

			err := mgr.TestByName(context.Background(), "jelly")
			Expect(errors.Is(err, ErrTestFailed)).To(BeTrue())
		})
	})
})
