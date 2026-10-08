package metadata

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Hardcover limits", Label("unit", "metadata"), func() {
	var (
		hc   *Hardcover
		sent int
	)

	BeforeEach(func() {
		hc = newTestHardcover("test-token")
		sent = 0
	})

	answer := func(status int, header http.Header, body string) {
		GinkgoHelper()
		hc.client.Transport = mbRoundTripper(
			func(*http.Request) (*http.Response, error) {
				sent++
				resp := jsonResponse(status, body)
				maps.Copy(resp.Header, header)
				return resp, nil
			},
		)
	}

	rateLimited := func(err error) *RateLimitedError {
		GinkgoHelper()
		Expect(err).To(MatchError(ErrRateLimited))
		var rl *RateLimitedError
		Expect(errors.As(err, &rl)).To(BeTrue())
		return rl
	}

	Describe("429", func() {
		It("reads Retry-After in seconds", func() {
			answer(429, http.Header{"Retry-After": {"17"}}, `{}`)
			_, err := hc.SearchAuthors(context.Background(), "x")
			Expect(rateLimited(err).RetryAfter).To(Equal(17 * time.Second))
		})

		It("reads Retry-After as an HTTP date", func() {
			at := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
			answer(429, http.Header{"Retry-After": {at}}, `{}`)
			_, err := hc.SearchAuthors(context.Background(), "x")
			Expect(rateLimited(err).RetryAfter).To(
				BeNumerically("~", 90*time.Second, 2*time.Second),
			)
		})

		It("falls back to a minute without Retry-After", func() {
			answer(429, nil, `{}`)
			_, err := hc.SearchAuthors(context.Background(), "x")
			Expect(rateLimited(err).RetryAfter).To(Equal(hcDefaultRetryAfter))
		})

		It("leaves the auth warning alone", func() {
			answer(429, nil, `{}`)
			_, err := hc.SearchAuthors(context.Background(), "x")
			Expect(err).To(MatchError(ErrRateLimited))
			Expect(hc.AuthRejected()).To(BeFalse())
		})
	})

	Describe("daily budget", func() {
		It("counts requests that were sent", func() {
			answer(200, nil, `{"data":{"search":{"results":{"hits":[]}}}}`)
			Expect(hc.Remaining()).To(Equal(hardcoverDailyBudget))
			_, err := hc.SearchAuthors(context.Background(), "x")
			Expect(err).NotTo(HaveOccurred())
			Expect(hc.Remaining()).To(Equal(hardcoverDailyBudget - 1))
		})

		It("does not charge a request that never left", func() {
			hc.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return nil, errors.New("connection refused")
				},
			)
			_, err := hc.SearchAuthors(context.Background(), "x")
			Expect(err).To(MatchError(ContainSubstring("connection refused")))
			Expect(hc.Remaining()).To(Equal(hardcoverDailyBudget))
		})

		It("refuses without sending once the day is spent", func() {
			answer(200, nil, `{}`)
			Expect(hc.Remaining()).To(Equal(hardcoverDailyBudget))
			hc.used = hardcoverDailyBudget
			_, err := hc.SearchAuthors(context.Background(), "x")
			rl := rateLimited(err)
			Expect(sent).To(BeZero())
			Expect(rl.RetryAfter).To(BeNumerically(">", 0))
			Expect(rl.RetryAfter).To(BeNumerically("<=", 24*time.Hour))
			Expect(hc.Remaining()).To(BeZero())
		})

		It("resets at the next UTC day", func() {
			Expect(hc.Remaining()).To(Equal(hardcoverDailyBudget))
			hc.used = hardcoverDailyBudget
			hc.budgetDay = hc.budgetDay.Add(-24 * time.Hour)
			Expect(hc.Remaining()).To(Equal(hardcoverDailyBudget))
		})

		It("keeps the scan reserve below the budget", func() {
			Expect(hc.ScanReserve()).To(Equal(hardcoverScanReserve))
			Expect(hardcoverScanReserve).To(BeNumerically("<", hardcoverDailyBudget))
		})
	})
})
