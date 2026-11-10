package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cassette proxy", Label("unit", "demo"), func() {
	var (
		dir     string
		started = time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	)

	// keep writes one exchange the way a recording run would have, on day.
	keep := func(p *proxy, rawURL string, day time.Time, body string) {
		GinkgoHelper()
		r := httptest.NewRequest(http.MethodGet, rawURL, nil)
		key, shown := p.key(r.Method, r.URL, nil)
		ex := &exchange{
			Method:     r.Method,
			URL:        shown,
			Status:     http.StatusOK,
			Header:     map[string]string{"Content-Type": "application/json"},
			RecordedAt: day,
		}
		file := filepath.Join(dir, safeHost(r.URL.Host), key+".json")
		Expect(p.save(file, ex, []byte(body))).To(Succeed())
	}

	get := func(p *proxy, rawURL string) *httptest.ResponseRecorder {
		GinkgoHelper()
		w := httptest.NewRecorder()
		p.handle(w, httptest.NewRequest(http.MethodGet, rawURL, nil), "https")
		return w
	}

	newReplay := func(record bool) *proxy {
		GinkgoHelper()
		p, err := newProxy(dir, record, nil)
		Expect(err).NotTo(HaveOccurred())
		return p
	}

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		raw, err := json.Marshal(cassetteMeta{RecordedAt: started})
		Expect(err).NotTo(HaveOccurred())
		Expect(
			os.WriteFile(filepath.Join(dir, "cassette.json"), raw, 0o644),
		).To(Succeed())
	})

	Describe("date shifting", func() {
		It(
			"shifts an exchange from the day it was kept, not the day the cassette was started",
			func() {
				p := newReplay(false)
				filled := started.AddDate(0, 0, 63) // a fill-in, nine weeks on
				keep(
					p,
					"https://api.example.test/old",
					time.Time{},
					`{"aired":"2026-11-05"}`,
				)
				keep(
					p,
					"https://api.example.test/new",
					filled,
					`{"aired":"2027-01-07"}`,
				)

				// Replayed two weeks after the fill-in, eleven after the start.
				p.now = func() time.Time { return filled.AddDate(0, 0, 14) }

				Expect(get(p, "https://api.example.test/old").Body.String()).
					To(ContainSubstring(`"2027-01-21"`), "eleven weeks on from cassette.json's day")
				Expect(get(p, "https://api.example.test/new").Body.String()).
					To(ContainSubstring(`"2027-01-21"`), "two weeks on from its own day")
			},
		)

		It("leaves a date more than two years from the recording alone", func() {
			p := newReplay(false)
			keep(
				p,
				"https://api.example.test/film",
				time.Time{},
				`{"released":"1945-11-30"}`,
			)
			p.now = func() time.Time { return started.AddDate(0, 0, 70) }

			Expect(get(p, "https://api.example.test/film").Body.String()).
				To(ContainSubstring(`"1945-11-30"`))
		})

		It("dates what it keeps by the UTC day", func() {
			p := newReplay(false)
			p.now = func() time.Time { return started.AddDate(0, 0, 63).Add(5 * time.Hour) }
			Expect(p.today()).To(Equal(started.AddDate(0, 0, 63)))
		})
	})

	Describe("a call that is no provider's", func() {
		DescribeTable("is refused without being counted or kept",
			func(record bool, rawURL string) {
				p := newReplay(record)
				// Recording, a miss goes upstream: fail the spec if this one does.
				p.upstream.Transport = roundTripFunc(
					func(*http.Request) (*http.Response, error) {
						defer GinkgoRecover()
						Fail("sent upstream")
						return nil, nil
					},
				)

				w := get(p, rawURL)

				Expect(w.Code).To(Equal(http.StatusBadGateway))
				Expect(w.Header().Get("X-Demo-Cassette")).To(Equal("off"))
				Expect(
					p.report(),
				).To(Equal("cassette: 0 answered, 0 recorded, 0 missing"))
				entries, err := os.ReadDir(dir)
				Expect(err).NotTo(HaveOccurred())
				Expect(entries).To(HaveLen(1), "cassette.json only")
			},
			Entry("replaying, the definitions' commit lookup", false,
				"https://api.github.com/repos/Prowlarr/Indexers/commits/master"),
			Entry("recording, the definitions' commit lookup", true,
				"https://api.github.com/repos/Prowlarr/Indexers/commits/master"),
			Entry("recording, the definitions' tarball", true,
				"https://codeload.github.com/Prowlarr/Indexers/tar.gz/abc123"),
		)

		It("still counts a provider call the cassette lacks", func() {
			p := newReplay(false)
			w := get(p, "https://api.example.test/absent")
			Expect(w.Code).To(Equal(http.StatusBadGateway))
			Expect(w.Header().Get("X-Demo-Cassette")).To(Equal("miss"))
			Expect(p.report()).To(ContainSubstring("1 missing"))
		})
	})
})

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(
	r *http.Request,
) (*http.Response, error) {
	return f(r)
}
