package otelx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"syscall"
)

const (
	redactedURLText = "<redacted url>"
	unknownCause    = "transport error"
)

// RedactTransportError replaces err's rendered message with one built entirely
// in this file, leaving err itself reachable through Unwrap.
//
// net/http reports transport failures as *url.Error, whose Error() embeds the
// request URL verbatim — Go strips the userinfo but not the query string.
// Callers here authenticate through that query string (torznab mandates
// `apikey`, Jackett emits `jackett_apikey` on download links, several trackers
// take a passkey), so an unredacted transport error puts a live credential into
// structured logs, span statuses, exception events and REST error bodies.
//
// Guaranteed. The message is composed only of text written in this file plus
// the scheme, host and path of each request URL reachable from err. No text err
// carries reaches it: not a wrapper's own rendering of the URL, and not the
// inner error's — net/http builds one whose message quotes a server-supplied
// Location header verbatim, query string included. Every *url.Error in err's
// tree is redacted, across both Unwrap forms, not only the first one errors.As
// would find. err and everything it wraps stay reachable, so errors.Is/As keep
// working and a sentinel a caller already wrapped around the failure still maps
// to its own HTTP status instead of degrading to a 500.
//
// Not guaranteed. A credential in the URL's host or path survives, because
// scheme/host/path are what make the message worth keeping; this strips the
// query, the userinfo and the fragment, matching what the client span's
// url.full carries. The words describing why the request failed are not err's
// but a classification made here, so an unrecognised failure renders as
// "transport error". An err with no *url.Error anywhere is returned unchanged —
// this redacts the request URLs net/http embeds, not arbitrary text a caller
// composed. And errors.As can still reach the original *url.Error and read its
// raw URL field; code that digs one out by type must redact it itself.
func RedactTransportError(err error) error {
	found := urlErrors(err)
	if len(found) == 0 {
		return err
	}
	return &redactedError{msg: renderURLErrors(found), err: err}
}

// redactedError renders the message RedactTransportError built while keeping
// the error it replaced reachable.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// urlErrors returns the outermost *url.Error on every branch of err's tree.
// The walk is hand-rolled because errors.As stops at the first match, which
// leaves the second half of an errors.Join unredacted. It also stops at each
// match rather than descending: renderURLErrors walks that error's own chain,
// so collecting nested hops here would render them twice.
//
//nolint:errorlint // The single-level assertions are the point; see above.
func urlErrors(err error) []*url.Error {
	var found []*url.Error
	var walk func(error)
	walk = func(e error) {
		switch t := e.(type) {
		case nil:
			return
		case *url.Error:
			found = append(found, t)
		case interface{ Unwrap() error }:
			walk(t.Unwrap())
		case interface{ Unwrap() []error }:
			for _, sub := range t.Unwrap() {
				walk(sub)
			}
		}
	}
	walk(err)
	return found
}

func renderURLErrors(found []*url.Error) string {
	parts := make([]string, len(found))
	for i, uerr := range found {
		parts[i] = fmt.Sprintf(
			"request to %s failed: %s",
			redactRawURL(uerr.URL),
			causeOf(uerr.Err),
		)
	}
	return strings.Join(parts, "; ")
}

// causeOf names why a request failed without quoting the error that reported
// it. A recognised failure gets a word chosen here; everything else collapses
// to a fixed string, because an unrecognised error's text has unknown
// provenance and may quote a URL nobody redacted.
func causeOf(err error) string {
	if err == nil {
		return unknownCause
	}
	if nested := urlErrors(err); len(nested) > 0 {
		return renderURLErrors(nested)
	}
	var (
		netErr  net.Error
		dnsErr  *net.DNSError
		certErr *tls.CertificateVerificationError
	)
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded),
		errors.As(err, &netErr) && netErr.Timeout():
		return "timeout"
	case errors.As(err, &dnsErr):
		if dnsErr.IsNotFound {
			return "host not found"
		}
		return "dns lookup failed"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection reset by peer"
	case errors.As(err, &certErr):
		return "tls certificate verification failed"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "connection closed"
	}
	return unknownCause
}

// redactRawURL renders raw the way RedactURL renders a *url.URL. A string that
// is not an absolute URL is dropped wholesale rather than echoed: it is not the
// request URL this file knows how to rewrite, so printing it would mean
// emitting text of unknown provenance.
func redactRawURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return redactedURLText
	}
	return RedactURL(u)
}
