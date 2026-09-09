package metadata

import (
	"context"
	"strings"
	"time"

	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// TVDB backs every TV metadata refresh and had no metrics at all while TMDB
// had two — so TVDB latency, error rate, and the token-expiry re-login path
// were all unalertable. Recorded at the one call both TVDB paths go through,
// rather than per method, so the retry leg is covered too.
var (
	tvdbRequests = otelx.Must(meter.Int64Counter(
		"streamline.metadata.tvdb.requests",
		metric.WithDescription("TVDB API requests by endpoint and outcome"),
	))
	tvdbDuration = otelx.Must(meter.Float64Histogram(
		"streamline.metadata.tvdb.duration",
		metric.WithDescription("TVDB API request duration"),
		metric.WithUnit("s"),
	))
)

// providerEndpoint reduces a request path to its first segment.
//
// The full path carries record ids ("/seasons/402589/translations/fr"), which
// as a metric attribute is one series per season in the library. The segment
// is what an operator groups by anyway.
func providerEndpoint(path string) string {
	trimmed := strings.TrimPrefix(path, "/")
	if i := strings.IndexByte(trimmed, '/'); i >= 0 {
		trimmed = trimmed[:i]
	}
	if trimmed == "" {
		return "root"
	}
	return trimmed
}

// recordProviderStatus puts the upstream status code on the active span and
// returns the outcome label for it. A 429 and a 500 were previously
// distinguishable only in a log line, so "the provider rate-limited us N times
// this hour" could not be graphed at all.
func recordProviderStatus(ctx context.Context, status int) string {
	trace.SpanFromContext(ctx).SetAttributes(
		semconv.HTTPResponseStatusCode(status),
	)
	switch {
	case status == 0:
		return "transport_error"
	case status == 429:
		return "rate_limited"
	case status >= 500:
		return "server_error"
	case status >= 400:
		return "client_error"
	default:
		return "success"
	}
}

// recordTVDBRequest reports one TVDB call. status is 0 for a transport error.
func recordTVDBRequest(
	ctx context.Context,
	path string,
	status int,
	started time.Time,
) {
	endpoint := providerEndpoint(path)
	tvdbDuration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(
		attribute.String("endpoint", endpoint),
	))
	tvdbRequests.Add(ctx, 1, metric.WithAttributes(
		attribute.String("endpoint", endpoint),
		attribute.String("outcome", recordProviderStatus(ctx, status)),
	))
}
