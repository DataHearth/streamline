package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/datahearth/streamline/internal/config"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	logglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

// Config carries service identity + stderr log sink. OTLP endpoint + log
// level/format are read from the config singleton at Setup time — they're
// operator-tunable. Service identity comes from -ldflags (version, commit,
// build date) so it's always a direct arg.
type Config struct {
	ServiceName      string
	ServiceVersion   string
	ServiceCommit    string
	ServiceBuildDate string

	// StderrWriter receives human-readable log output. Defaults to os.Stderr.
	// Tests can override with GinkgoWriter.
	StderrWriter io.Writer
}

// Shutdown flushes and shuts down all OTel providers (tracer, meter, logger).
type Shutdown func(ctx context.Context) error

// Setup initializes the full observability pipeline and returns the slog
// handler the caller must install via slog.SetDefault. The handler fans log
// records out to:
//   - a stderr text/json handler (always), for local visibility and crashes.
//   - the OTel logs bridge (when Endpoint is set), for centralized collection.
//
// Traces and metrics providers are also registered as OTel globals when the
// endpoint is set. All three signals share one resource.
func Setup(ctx context.Context, cfg Config) (slog.Handler, Shutdown, error) {
	cs := config.Get()

	if !cs.Log.App.Enabled {
		return slog.DiscardHandler,
			func(context.Context) error { return nil }, nil
	}

	appWriter, appCloser := openLogWriter(
		cs.Log.App.Output,
		cs.Log.App.Rotate,
		cfg.StderrWriter,
	)

	stderrHandler := newStderrHandler(cs.Log.App.Level, cs.Log.App.Format, appWriter)

	endpoint := cs.OTel.Endpoint
	if endpoint == "" {
		shutdown := func(context.Context) error {
			if appCloser != nil {
				return appCloser.Close()
			}
			return nil
		}
		return NewContextEnrichingHandler(stderrHandler), shutdown, nil
	}

	attrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String(cfg.ServiceName),
		semconv.ServiceVersionKey.String(cfg.ServiceVersion),
	}
	if cfg.ServiceCommit != "" {
		attrs = append(attrs, attribute.String("service.commit", cfg.ServiceCommit))
	}
	if cfg.ServiceBuildDate != "" {
		attrs = append(
			attrs,
			attribute.String("service.build_date", cfg.ServiceBuildDate),
		)
	}

	res, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		return nil, nil, fmt.Errorf("build resource: %w", err)
	}

	// Traces
	traceExp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(endpoint))
	if err != nil {
		return nil, nil, fmt.Errorf("otlp trace exporter: %w", err)
	}
	tp := trace.NewTracerProvider(
		trace.WithBatcher(traceExp),
		trace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// Metrics
	metricExp, err := otlpmetrichttp.New(
		ctx,
		otlpmetrichttp.WithEndpoint(endpoint),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("otlp metric exporter: %w", err)
	}
	mp := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExp)),
		metric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	// Logs
	logExp, err := otlploghttp.New(ctx, otlploghttp.WithEndpoint(endpoint))
	if err != nil {
		return nil, nil, fmt.Errorf("otlp log exporter: %w", err)
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		sdklog.WithResource(res),
	)
	logglobal.SetLoggerProvider(lp)

	otelHandler := otelslog.NewHandler(cfg.ServiceName)

	// contextEnrichingHandler sits outermost so every downstream handler
	// (stderr + OTel bridge) sees the same ctx-derived attrs.
	handler := NewContextEnrichingHandler(multiHandler{stderrHandler, otelHandler})

	shutdown := func(ctx context.Context) error {
		errs := []error{
			tp.Shutdown(ctx),
			mp.Shutdown(ctx),
			lp.Shutdown(ctx),
		}
		if appCloser != nil {
			errs = append(errs, appCloser.Close())
		}
		return errors.Join(errs...)
	}

	return handler, shutdown, nil
}

// LevelCritical is emitted for unrecoverable conditions (panics, data
// corruption, failed invariants). Sits above slog.LevelError so it surfaces
// above normal error noise in log aggregators. Value mirrors the OTel
// SeverityFatal range so the otelslog bridge maps it correctly.
const LevelCritical slog.Level = slog.LevelError + 4

func newStderrHandler(level, format string, w io.Writer) slog.Handler {
	if w == nil {
		w = os.Stderr
	}

	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:       lvl,
		ReplaceAttr: replaceLevelAttr,
	}

	switch strings.ToLower(format) {
	case "json":
		return slog.NewJSONHandler(w, opts)
	default:
		return slog.NewTextHandler(w, opts)
	}
}

// replaceLevelAttr renders custom levels with meaningful names. Without this,
// slog prints LevelCritical as "ERROR+4", which is both ugly and un-greppable.
func replaceLevelAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key != slog.LevelKey {
		return a
	}
	lvl, ok := a.Value.Any().(slog.Level)
	if !ok {
		return a
	}
	if lvl >= LevelCritical {
		a.Value = slog.StringValue("CRITICAL")
	}
	return a
}
