package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

// combinedHandler renders slog records as single-line space-separated access
// log entries with a kv tail of route / request_id. Timestamps are RFC3339.
//
// Expected attributes on each record: remote_ip, user.email (optional),
// method, path, status, bytes, referer, user_agent, duration_ms, route,
// request_id.
type combinedHandler struct {
	mu sync.Mutex
	w  io.Writer
}

func (h *combinedHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *combinedHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := collectAttrs(r)

	ts := r.Time.UTC().Format(time.RFC3339)
	email := attrs["user.email"]
	if email == "" {
		email = "-"
	}

	line := fmt.Sprintf(
		"%s - %s [%s] %q %s %s %q %q %sms route=%s request_id=%s\n",
		stringOrDash(attrs["remote_ip"]),
		email,
		ts,
		fmt.Sprintf("%s %s HTTP/1.1", attrs["method"], attrs["path"]),
		stringOrDash(attrs["status"]),
		stringOrDash(attrs["bytes"]),
		attrs["referer"],
		attrs["user_agent"],
		stringOrDash(attrs["duration_ms"]),
		stringOrDash(attrs["route"]),
		stringOrDash(attrs["request_id"]),
	)

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, line)
	return err
}

func (h *combinedHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *combinedHandler) WithGroup(string) slog.Handler      { return h }

func collectAttrs(r slog.Record) map[string]string {
	m := make(map[string]string, 12)
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.String()
		return true
	})
	return m
}

func stringOrDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}
