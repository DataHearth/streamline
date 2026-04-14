package observability

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/trace"
)

// SkipFunc returns true when the request should be excluded from access
// logging. Returns false (or nil SkipFunc) to log all requests.
type SkipFunc func(*http.Request) bool

// Middleware emits one access-log line per non-skipped request, captured on
// the way out of the handler chain so status/bytes/duration reflect the
// final response. When the HTTPLogger is nil (disabled), returns a
// passthrough so callers don't have to branch.
func (l *HTTPLogger) Middleware(skip SkipFunc) func(http.Handler) http.Handler {
	if l == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skip != nil && skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			l.emit(r, ww, time.Since(start))
		})
	}
}

func (l *HTTPLogger) emit(
	r *http.Request,
	ww chimw.WrapResponseWriter,
	dur time.Duration,
) {
	ctx := r.Context()

	attrs := []slog.Attr{
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("route", routePattern(ctx)),
		slog.Int("status", ww.Status()),
		slog.Int64("bytes", int64(ww.BytesWritten())),
		slog.Int64("duration_ms", dur.Milliseconds()),
		slog.String("remote_ip", remoteIP(r)),
		slog.String("user_agent", r.UserAgent()),
		slog.String("referer", r.Referer()),
	}

	if id := chimw.GetReqID(ctx); id != "" {
		attrs = append(attrs, slog.String("request_id", id))
	}
	if claims := auth.ClaimsFromContext(ctx); claims != nil && claims.UserID != 0 {
		attrs = append(attrs,
			slog.Uint64("user.id", uint64(claims.UserID)),
			slog.String("user.email", claims.Email),
		)
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		attrs = append(attrs,
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}

	l.slog.LogAttrs(ctx, slog.LevelInfo, "http_access", attrs...)
}

func routePattern(ctx context.Context) string {
	if rc := chi.RouteContext(ctx); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return ""
}

func remoteIP(r *http.Request) string {
	if ip := chimw.GetClientIP(r.Context()); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// swapHTTPLoggerWriter rebuilds the underlying sink against buf for tests so
// middleware assertions can inspect emitted lines without going through file
// I/O or stderr.
func swapHTTPLoggerWriter(l *HTTPLogger, buf *bytes.Buffer, format string) {
	var sink slog.Handler
	switch format {
	case "json":
		sink = slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	case "combined":
		sink = &combinedHandler{w: buf}
	}
	l.slog = slog.New(multiHandler{sink})
}
