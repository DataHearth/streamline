package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe(
	"a site behind a browser challenge",
	Label("unit", "cardigann"),
	func() {
		var (
			trk    *tracker
			solves []solverRequest
			solver *Solver
		)

		BeforeEach(func() {
			solves = nil
			trk = newTracker(func(w http.ResponseWriter, r *http.Request) {
				c, err := r.Cookie("cf_clearance")
				if err != nil || c.Value != "ok" || r.UserAgent() != "SolvedUA" {
					w.Header().Set("Server", "cloudflare")
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write(
						[]byte(`<html><title>Just a moment...</title></html>`),
					)
					return
				}
				_, _ = w.Write([]byte(htmlResults))
			})
			srv := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.URL.Path).To(Equal("/v1"))
					var req solverRequest
					Expect(json.NewDecoder(r.Body).Decode(&req)).To(Succeed())
					solves = append(solves, req)
					_, _ = w.Write(
						[]byte(`{"status":"ok","solution":{"userAgent":"SolvedUA",
"cookies":[{"name":"cf_clearance","value":"ok"}]}}`),
					)
				}),
			)
			DeferCleanup(srv.Close)
			solver = &Solver{
				URL:     srv.URL,
				Timeout: time.Minute,
				Client:  http.DefaultClient,
			}
		})

		newEngine := func(s *Solver) *Engine {
			GinkgoHelper()
			eng, err := New(convertFixture(htmlDefinition), Options{
				Name:   "challenge-" + CurrentSpecReport().LeafNodeText,
				Client: trk.client(),
				Solver: s,
			})
			Expect(err).NotTo(HaveOccurred())
			return eng
		}

		It("gets through with the solver and keeps its clearance", func() {
			eng := newEngine(solver)
			releases, err := eng.Search(context.Background(), Query{Term: "dune"})
			Expect(err).NotTo(HaveOccurred())
			Expect(releases).NotTo(BeEmpty())
			Expect(solves).To(HaveLen(1))
			Expect(solves[0].Cmd).To(Equal("request.get"))
			Expect(solves[0].MaxTimeout).To(Equal(int64(60000)))
			Expect(eng.Session().UserAgent).To(Equal("SolvedUA"))
			Expect(eng.Session().Cookies).To(HaveKeyWithValue("cf_clearance", "ok"))

			// The clearance carries over; no second solve.
			_, err = eng.Search(context.Background(), Query{Term: "dune"})
			Expect(err).NotTo(HaveOccurred())
			Expect(solves).To(HaveLen(1))
		})

		It("names the setting when no solver is configured", func() {
			_, err := newEngine(
				nil,
			).Search(context.Background(), Query{Term: "dune"})
			Expect(err).To(MatchError(ErrChallenge))
			Expect(err).To(MatchError(ContainSubstring("cardigann.solver_url")))
		})

		It("is not fooled by a 503 from another server", func() {
			resp := &response{
				status: 503,
				header: http.Header{},
				body:   "<title>Just a moment...</title>",
			}
			Expect(isChallenge(resp)).To(BeFalse())
			resp.header.Set("Server", "ddos-guard")
			resp.body = "<title>DDOS-GUARD</title>"
			Expect(isChallenge(resp)).To(BeTrue())
		})
	},
)
