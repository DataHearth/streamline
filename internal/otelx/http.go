// Package otelx holds low-level OpenTelemetry helpers shared across the
// project. It must stay a leaf package — it has no dependencies on other
// internal packages — so every feature package (auth, download, indexer, ...)
// can import it without creating cycles through internal/observability.
package otelx

import (
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// HTTPClient is a shared *http.Client with OTel instrumentation wired into the
// transport. Every outbound request becomes a child span of the caller's
// context with HTTP semconv attributes, and metrics are recorded via the
// global meter provider. Use for every outbound HTTP call (external APIs,
// indexers, media servers, download clients) so traces, latencies, and
// errors are visible in the backend.
// Timeout is a backstop, not a tuning knob: every caller here does
// request/response API calls, and a provider that accepts the connection but
// never answers (blocked egress, dead VPN sidecar) otherwise hangs the
// handler — and any mutex it holds — for the process lifetime.
//
// spanURLRedactor sits *under* otelhttp, not above it: see its doc comment.
var HTTPClient = &http.Client{
	Transport: otelhttp.NewTransport(spanURLRedactor{base: http.DefaultTransport}),
	Timeout:   30 * time.Second,
}

// spanURLRedactor rewrites the client span's url.full attribute so the request
// query string never leaves the process.
//
// otelhttp records url.full straight from req.URL.String() and strips only the
// userinfo (Transport.RoundTrip -> internal/semconv.HTTPClient.RequestTraceAttrs),
// so the query survives verbatim on every span, successful or not. Two callers
// here authenticate through the query string and cannot do otherwise: torznab
// mandates `apikey`, and Jackett/Prowlarr release-download links carry one
// (Prowlarr's header auth does not extend to them). The rest — Plex, Jellyfin,
// TMDB, TVDB, the torrent clients — authenticate by header or body, so they are
// safe today for a reason no code enforces; redacting here covers them anyway.
//
// It has to run under otelhttp rather than in front of it: stripping the query
// before otelhttp would strip it from the outbound request too. otelhttp instead
// hands its base RoundTripper a request whose context already carries the span
// it annotated, and OTel resolves duplicate attribute keys last-write-wins, so
// overwriting url.full here is the one place the real request keeps its
// credentials while the span does not.
//
// The span in ctx is the client span only because HTTPClient configures no
// otelhttp filters — a filtered request bypasses span creation entirely and
// reaches this transport under the caller's own span.
type spanURLRedactor struct {
	base http.RoundTripper
}

// The guard mirrors everything RedactURL strips, not just the query: gating on
// RawQuery alone let a fragment-bearing URL export otelhttp's own url.full
// verbatim, so the helper's stated invariant was defeated by its only caller.
func (t spanURLRedactor) RoundTrip(req *http.Request) (*http.Response, error) {
	if u := req.URL; u != nil &&
		(u.RawQuery != "" || u.ForceQuery ||
			u.Fragment != "" || u.RawFragment != "" || u.User != nil) {
		trace.SpanFromContext(req.Context()).
			SetAttributes(semconv.URLFull(RedactURL(req.URL)))
	}
	return t.base.RoundTrip(req)
}

// RedactURL renders u with everything that can carry a credential removed,
// keeping the scheme, host and path that make a trace or a log line useful.
// Query parameters go wholesale rather than by name: the credential parameter
// differs per integration (apikey, X-Plex-Token, passkey, token) and a denylist
// would leak the first one nobody thought of.
func RedactURL(u *url.URL) string {
	redacted := *u
	redacted.User = nil
	redacted.RawQuery = ""
	redacted.ForceQuery = false
	redacted.Fragment = ""
	redacted.RawFragment = ""
	return redacted.String()
}
