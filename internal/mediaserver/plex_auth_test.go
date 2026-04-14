package mediaserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Plex PIN Auth", Label("unit", "mediaserver"), func() {
	creds := PlexClientCreds{
		ClientID: "test-uuid",
		Product:  "Streamline",
		Version:  "0.0.0-test",
	}

	Describe("beginPlexPin", func() {
		It("creates a PIN and constructs the auth URL", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.Method).To(Equal(http.MethodPost))
					Expect(r.URL.Path).To(Equal("/pins"))
					Expect(r.URL.Query().Get("strong")).To(Equal("true"))
					Expect(r.Header.Get("X-Plex-Product")).To(Equal("Streamline"))
					Expect(
						r.Header.Get("X-Plex-Client-Identifier"),
					).To(Equal("test-uuid"))
					Expect(r.Header.Get("X-Plex-Version")).To(Equal("0.0.0-test"))
					Expect(r.Header.Get("Accept")).To(Equal("application/json"))

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"id": 12345, "code": "ABCD"}`))
				}),
			)
			defer ts.Close()

			pin, err := beginPlexPin(context.Background(), ts.URL, creds)
			Expect(err).NotTo(HaveOccurred())
			Expect(pin.ID).To(Equal(uint64(12345)))
			Expect(pin.Code).To(Equal("ABCD"))
			Expect(pin.AuthURL).To(HavePrefix("https://app.plex.tv/auth#?"))

			frag := strings.TrimPrefix(pin.AuthURL, "https://app.plex.tv/auth#?")
			vals, err := url.ParseQuery(frag)
			Expect(err).NotTo(HaveOccurred())
			Expect(vals.Get("clientID")).To(Equal("test-uuid"))
			Expect(vals.Get("code")).To(Equal("ABCD"))
			Expect(vals.Get("context[device][product]")).To(Equal("Streamline"))
		})

		It("returns error on non-2xx", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusBadGateway)
				}),
			)
			defer ts.Close()
			_, err := beginPlexPin(context.Background(), ts.URL, creds)
			Expect(err).To(MatchError(ContainSubstring("unexpected status 502")))
		})

		It("returns error on malformed body", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte("not json"))
				}),
			)
			defer ts.Close()
			_, err := beginPlexPin(context.Background(), ts.URL, creds)
			Expect(err).To(MatchError(ContainSubstring("plex pin decode")))
		})
	})

	Describe("pollPlexPin", func() {
		It("returns empty AuthToken when still pending", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.URL.Path).To(Equal("/pins/123"))
					Expect(
						r.Header.Get("X-Plex-Client-Identifier"),
					).To(Equal("test-uuid"))
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"authToken": null}`))
				}),
			)
			defer ts.Close()

			res, err := pollPlexPin(context.Background(), ts.URL, creds, 123)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.AuthToken).To(BeEmpty())
			Expect(res.Expired).To(BeFalse())
		})

		It("returns AuthToken once Plex sets it", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"authToken": "xtoken"}`))
				}),
			)
			defer ts.Close()

			res, err := pollPlexPin(context.Background(), ts.URL, creds, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.AuthToken).To(Equal("xtoken"))
			Expect(res.Expired).To(BeFalse())
		})

		It("returns Expired=true on 404", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}),
			)
			defer ts.Close()

			res, err := pollPlexPin(context.Background(), ts.URL, creds, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Expired).To(BeTrue())
		})

		It("returns error on 5xx", func() {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}),
			)
			defer ts.Close()
			_, err := pollPlexPin(context.Background(), ts.URL, creds, 1)
			Expect(err).To(MatchError(ContainSubstring("unexpected status 500")))
		})
	})
})
