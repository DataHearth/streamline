package observability

import (
	"bytes"
	"context"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("combinedHandler", Label("unit", "observability"), func() {
	It("renders a record with RFC3339 timestamp and structured tail", func() {
		var buf bytes.Buffer
		h := &combinedHandler{w: &buf}

		ts := time.Date(2026, 5, 14, 10, 15, 30, 0, time.UTC)
		rec := slog.NewRecord(ts, slog.LevelInfo, "http_access", 0)
		rec.AddAttrs(
			slog.String("remote_ip", "1.2.3.4"),
			slog.String("user.email", "alice@example.com"),
			slog.String("method", "GET"),
			slog.String("path", "/api/v1/movies"),
			slog.Int("status", 200),
			slog.Int64("bytes", 1234),
			slog.String("referer", "https://ref"),
			slog.String("user_agent", "curl/8"),
			slog.Int64("duration_ms", 42),
			slog.String("route", "/api/v1/movies"),
			slog.String("request_id", "req-abc"),
		)

		Expect(h.Handle(context.Background(), rec)).To(Succeed())

		got := buf.String()
		Expect(
			got,
		).To(MatchRegexp(`^1\.2\.3\.4 - alice@example\.com \[2026-05-14T10:15:30Z\] "GET /api/v1/movies HTTP/1.1" 200 1234 "https://ref" "curl/8" 42ms route=/api/v1/movies request_id=req-abc\n$`))
	})

	It("renders \"-\" for missing user email", func() {
		var buf bytes.Buffer
		h := &combinedHandler{w: &buf}

		rec := slog.NewRecord(time.Now(), slog.LevelInfo, "http_access", 0)
		rec.AddAttrs(
			slog.String("remote_ip", "1.2.3.4"),
			slog.String("method", "GET"),
			slog.String("path", "/health"),
			slog.Int("status", 200),
			slog.Int64("bytes", 0),
			slog.String("referer", ""),
			slog.String("user_agent", ""),
			slog.Int64("duration_ms", 1),
			slog.String("route", "/health"),
			slog.String("request_id", ""),
		)

		Expect(h.Handle(context.Background(), rec)).To(Succeed())
		Expect(buf.String()).To(MatchRegexp(` - - \[`))
	})
})
