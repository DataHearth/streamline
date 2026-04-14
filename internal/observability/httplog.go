package observability

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/datahearth/streamline/internal/config"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// HTTPLogger emits one access-log line per non-skipped HTTP request to a
// dedicated sink (stderr or a rotating file) and, when an OTel logger
// provider is registered, to the OTel logs pipeline. Returned nil when http
// access logging is disabled by config.
type HTTPLogger struct {
	slog   *slog.Logger
	closer io.Closer
}

// NewHTTPLogger constructs an HTTPLogger from cfg. Returns nil, nil when
// cfg.Enabled is false. Errors when output path is relative or format is
// not one of "json" or "combined".
func NewHTTPLogger(cfg config.HTTPLog) (*HTTPLogger, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.Format != "json" && cfg.Format != "combined" {
		return nil, fmt.Errorf(
			"log.http.format must be \"json\" or \"combined\", got %q",
			cfg.Format,
		)
	}

	writer, closer := openLogWriter(cfg.Output, cfg.Rotate, nil)

	var sink slog.Handler
	switch cfg.Format {
	case "json":
		sink = slog.NewJSONHandler(
			writer,
			&slog.HandlerOptions{Level: slog.LevelInfo},
		)
	case "combined":
		sink = &combinedHandler{w: writer}
	}
	handler := multiHandler{
		sink,
		otelslog.NewHandler(
			"github.com/datahearth/streamline/internal/observability/httplog",
		),
	}

	return &HTTPLogger{
		slog:   slog.New(handler),
		closer: closer,
	}, nil
}

// Close flushes and closes the underlying writer. No-op for stderr.
func (l *HTTPLogger) Close() error {
	if l == nil || l.closer == nil {
		return nil
	}
	return l.closer.Close()
}
