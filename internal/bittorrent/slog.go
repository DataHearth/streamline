package bittorrent

import (
	"context"
	"log/slog"
)

// minLevelHandler drops records below min before delegating to inner. The
// embedded engine is chatty at info and below, and its output is interleaved
// with streamline's own; only its errors are worth surfacing.
type minLevelHandler struct {
	inner slog.Handler
	min   slog.Level
}

func (h minLevelHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.min && h.inner.Enabled(ctx, l)
}

func (h minLevelHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.inner.Handle(ctx, r)
}

func (h minLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return minLevelHandler{inner: h.inner.WithAttrs(attrs), min: h.min}
}

func (h minLevelHandler) WithGroup(name string) slog.Handler {
	return minLevelHandler{inner: h.inner.WithGroup(name), min: h.min}
}

// engineSlogger builds the logger handed to anacrolix/torrent. slog.Default is
// resolved on each call rather than captured, so a test that swaps the default
// still sees the engine's records.
func engineSlogger() *slog.Logger {
	return slog.New(minLevelHandler{
		inner: slog.Default().Handler(),
		min:   slog.LevelError,
	})
}
