package otelx_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/otelx"
)

var _ = Describe("RedactTransportError", Label("unit", "otelx"), func() {
	const (
		key   = "SUPERSECRETKEY"
		other = "OTHERSECRETKEY"
	)

	refused := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: os.NewSyscallError("connect", syscall.ECONNREFUSED),
	}

	It("keeps the endpoint and drops the query", func() {
		err := otelx.RedactTransportError(&url.Error{
			Op:  "Get",
			URL: "http://idx.example:9117/api?t=caps&apikey=" + key,
			Err: refused,
		})

		Expect(err.Error()).NotTo(ContainSubstring(key))
		Expect(err.Error()).NotTo(ContainSubstring("apikey"))
		Expect(err.Error()).To(ContainSubstring("http://idx.example:9117/api"))
		Expect(err.Error()).To(ContainSubstring("connection refused"))
	})

	It("returns errors that carry no url unchanged", func() {
		inner := errors.New("some other failure")
		Expect(otelx.RedactTransportError(inner)).To(BeIdenticalTo(inner))
	})

	// A *url.Error with a nil Err renders as `Get "http://h/api":
	// %!w(<nil>)`. Reading through it without a guard is a nil dereference, in
	// the one helper whose whole job is surviving hostile error shapes.
	It("survives a *url.Error carrying no inner error", func() {
		var err error
		Expect(func() {
			err = otelx.RedactTransportError(&url.Error{
				Op:  "Get",
				URL: "http://idx.example/api?apikey=" + key,
				Err: nil,
			})
		}).NotTo(Panic())

		Expect(err.Error()).NotTo(ContainSubstring(key))
		Expect(err.Error()).To(ContainSubstring("http://idx.example/api"))
	})

	Describe("text it refuses to trust", func() {
		// The wrapper formatted the URL itself, so the *url.Error's own
		// rendering appearing in the message proves nothing about the rest of
		// it.
		It("drops a wrapper's own copy of the url", func() {
			raw := "http://idx.example/api?apikey=" + key
			err := otelx.RedactTransportError(fmt.Errorf(
				"probe of %s failed: %w",
				raw,
				&url.Error{Op: "Get", URL: raw, Err: refused},
			))

			Expect(err.Error()).NotTo(ContainSubstring(key))
			Expect(err.Error()).To(ContainSubstring("http://idx.example/api"))
		})

		// net/http client.go builds uerr(fmt.Errorf("failed to parse Location
		// header %q: %v", loc, err)) — a plain error whose text quotes a
		// server-supplied URL, query string and all.
		It("drops a url quoted by the inner error", func() {
			err := otelx.RedactTransportError(&url.Error{
				Op:  "Get",
				URL: "http://idx.example/api?apikey=" + key,
				Err: fmt.Errorf(
					"failed to parse Location header %q: %s",
					"http://mirror.example/dl?passkey="+other,
					"invalid control character in URL",
				),
			})

			Expect(err.Error()).NotTo(ContainSubstring(key))
			Expect(err.Error()).NotTo(ContainSubstring(other))
			Expect(err.Error()).To(ContainSubstring("transport error"))
		})

		It("drops a url field that is not an absolute url", func() {
			err := otelx.RedactTransportError(&url.Error{
				Op:  "Get",
				URL: "opaque blob apikey=" + key,
				Err: refused,
			})

			Expect(err.Error()).NotTo(ContainSubstring(key))
			Expect(err.Error()).To(ContainSubstring("<redacted url>"))
		})
	})

	Describe("every url in the tree", func() {
		It("redacts both halves of a join", func() {
			err := otelx.RedactTransportError(errors.Join(
				&url.Error{
					Op:  "Get",
					URL: "http://a.example/api?apikey=" + key,
					Err: refused,
				},
				&url.Error{
					Op:  "Get",
					URL: "http://b.example/dl?passkey=" + other,
					Err: refused,
				},
			))

			Expect(err.Error()).NotTo(ContainSubstring(key))
			Expect(err.Error()).NotTo(ContainSubstring(other))
			Expect(err.Error()).To(ContainSubstring("http://a.example/api"))
			Expect(err.Error()).To(ContainSubstring("http://b.example/dl"))
		})

		It("redacts every hop of a redirect chain", func() {
			err := otelx.RedactTransportError(&url.Error{
				Op:  "Get",
				URL: "http://idx.example/api?apikey=" + key,
				Err: &url.Error{
					Op:  "Get",
					URL: "http://mirror.example/api?passkey=" + other,
					Err: errors.New("stopped after 10 redirects"),
				},
			})

			Expect(err.Error()).NotTo(ContainSubstring(key))
			Expect(err.Error()).NotTo(ContainSubstring(other))
			Expect(err.Error()).To(ContainSubstring("http://mirror.example/api"))
		})
	})

	Describe("the chain it preserves", func() {
		It("keeps the error it redacted reachable", func() {
			inner := errors.New("connect: connection refused")
			err := otelx.RedactTransportError(&url.Error{
				Op:  "Get",
				URL: "http://idx.example/api?apikey=" + key,
				Err: inner,
			})

			Expect(errors.Is(err, inner)).To(BeTrue())
		})

		// Call sites redact before wrapping, but nothing stops a future one
		// from redacting a *url.Error a sentinel is already wrapped around. If
		// redaction dropped that sentinel the handler would stop recognising
		// the failure and answer 500 instead of 422. The wrapper's own words
		// are gone — that is the price of not trusting them — but errors.Is
		// still sees it.
		It("keeps a sentinel the caller already wrapped around the failure", func() {
			sentinel := errors.New("indexer unreachable")
			err := otelx.RedactTransportError(fmt.Errorf(
				"%w: %w",
				sentinel,
				&url.Error{
					Op:  "Get",
					URL: "http://idx.example/api?apikey=" + key,
					Err: refused,
				},
			))

			Expect(err).To(MatchError(sentinel))
			Expect(err.Error()).NotTo(ContainSubstring(key))
		})

		It("reaches a *url.Error a multi-error wrapper holds", func() {
			sentinel := errors.New("indexer unreachable")
			err := otelx.RedactTransportError(&reformattingError{
				sentinel: sentinel,
				uerr: &url.Error{
					Op:  "Get",
					URL: "http://idx.example/api?apikey=" + key,
					Err: refused,
				},
			})

			Expect(err).To(MatchError(sentinel))
			Expect(err.Error()).NotTo(ContainSubstring(key))
			Expect(err.Error()).NotTo(ContainSubstring("giving up"))
		})
	})

	DescribeTable("naming the cause in words of its own",
		func(inner error, want string) {
			err := otelx.RedactTransportError(&url.Error{
				Op:  "Get",
				URL: "http://idx.example/api?apikey=" + key,
				Err: inner,
			})
			Expect(err.Error()).To(HaveSuffix(want))
		},
		Entry("refused connection", refused, "connection refused"),
		Entry("cancellation", context.Canceled, "canceled"),
		Entry("deadline", context.DeadlineExceeded, "timeout"),
		Entry(
			"unknown host",
			&net.DNSError{Err: "no such host", IsNotFound: true},
			"host not found",
		),
		Entry(
			"anything unrecognised",
			errors.New("some brand new failure mode"),
			"transport error",
		),
	)
})

// reformattingError stands in for a wrapper that renders the *url.Error it
// holds instead of embedding its Error() verbatim.
type reformattingError struct {
	sentinel error
	uerr     *url.Error
}

func (e *reformattingError) Error() string {
	return fmt.Sprintf("%s: giving up on %s", e.sentinel, e.uerr.URL)
}

func (e *reformattingError) Unwrap() []error { return []error{e.sentinel, e.uerr} }
