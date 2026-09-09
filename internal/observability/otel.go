package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/datahearth/streamline/internal/config"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	otelruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
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

// Export budgets, sized for the small self-hosted machine this runs on rather
// than for the SDK's data-centre defaults. Sampling is operator-tunable via
// otel.sample_ratio (or OTEL_TRACES_SAMPLER, which wins); these three are
// compile-time because an operator who wants full fidelity is running a
// collector that can take it.
const (
	// logQueueSize is the batch processor's ring. The SDK default of 2048
	// allocates its whole backing store at startup (~1.5 MB) for a burst that
	// a single-user install does not have.
	logQueueSize = 256
	// logBatchSize bounds one export; smaller batches, sent more evenly.
	logBatchSize = 64
	// logExportInterval is how often the processor wakes. The 1s default is a
	// timer firing all day on an install that logs a handful of lines a
	// minute.
	logExportInterval = 5 * time.Second
)

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

	// log.app.enabled gates the stderr sink and nothing else. It used to
	// return early, so quieting local logs also silently stopped traces and
	// metrics — an instance exporting nothing at all, for a reason nowhere
	// near the OTel config.
	var appCloser io.Closer
	stderrHandler := slog.DiscardHandler
	if cs.Log.App.Enabled {
		var appWriter io.Writer
		appWriter, appCloser = openLogWriter(
			cs.Log.App.Output,
			cs.Log.App.Rotate,
			cfg.StderrWriter,
		)
		stderrHandler = newStderrHandler(
			cs.Log.App.Level,
			cs.Log.App.Format,
			appWriter,
		)
	}

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

	setExportErrorHandler(stderrHandler)

	attrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String(cfg.ServiceName),
		semconv.ServiceVersionKey.String(cfg.ServiceVersion),
	}
	if cs.OTel.Environment != "" {
		attrs = append(
			attrs,
			semconv.DeploymentEnvironmentNameKey.String(cs.OTel.Environment),
		)
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

	// Host and OS come from detectors, not literals: two installs exporting to
	// one collector are otherwise the same series. WithFromEnv also honours
	// OTEL_RESOURCE_ATTRIBUTES / OTEL_SERVICE_NAME, which operators expect to
	// work without us reading them ourselves.
	res, err := resource.New(ctx,
		resource.WithAttributes(attrs...),
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcessRuntimeVersion(),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("build resource: %w", err)
	}

	// Traces
	traceOpts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint)}
	if cs.OTel.Insecure {
		traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
	}
	traceExp, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("otlp trace exporter: %w", err)
	}
	// Sampled, not head-on. The DB driver is instrumented, so an unsampled
	// provider turns every SQL statement into an exported span — on an idle
	// install that is thousands an hour of scheduler bookkeeping, and under
	// load it is the dominant span source by an order of magnitude. Parent-
	// based so a sampled request keeps its whole tree: what gets thinned is
	// which traces start, never a trace with holes in it.
	tpOpts := []trace.TracerProviderOption{
		trace.WithBatcher(traceExp),
		trace.WithResource(res),
	}
	// Passing WithSampler at all makes the SDK skip its own env parsing, so an
	// operator who sets OTEL_TRACES_SAMPLER gets silently overridden. Stand
	// aside when they have: their knob is the more specific one.
	if os.Getenv("OTEL_TRACES_SAMPLER") == "" {
		tpOpts = append(tpOpts, trace.WithSampler(
			trace.ParentBased(trace.TraceIDRatioBased(cs.OTel.SampleRatio)),
		))
	}
	tp := trace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)

	// Metrics
	metricOpts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(endpoint)}
	if cs.OTel.Insecure {
		metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
	}
	metricExp, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("otlp metric exporter: %w", err)
	}
	mp := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExp)),
		metric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	// Goroutine count, GC pause and heap size. On a single self-hosted binary
	// with no other metrics surface, a goroutine leak or an approaching OOM is
	// otherwise only visible once the process is already gone.
	if err := otelruntime.Start(otelruntime.WithMeterProvider(mp)); err != nil {
		return nil, nil, fmt.Errorf("runtime metrics: %w", err)
	}

	// Logs
	logOpts := []otlploghttp.Option{otlploghttp.WithEndpoint(endpoint)}
	if cs.OTel.Insecure {
		logOpts = append(logOpts, otlploghttp.WithInsecure())
	}
	logExp, err := otlploghttp.New(ctx, logOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("otlp log exporter: %w", err)
	}
	// The SDK's default batch processor reserves its whole queue up front —
	// 2048 records of backing store, ~1.5 MB — and wakes once a second whether
	// or not anything was logged. On a 256 MB target that is a real slice of
	// the budget held for a burst that mostly never comes.
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp,
			sdklog.WithMaxQueueSize(logQueueSize),
			sdklog.WithExportMaxBatchSize(logBatchSize),
			sdklog.WithExportInterval(logExportInterval),
		)),
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

// exportErrorInterval throttles the export-failure log. A collector that is
// down fails every batch, and the point of the line is that it is happening at
// all, not how many times a minute.
const exportErrorInterval = time.Minute

// setExportErrorHandler routes SDK-internal export failures — connection
// refused, TLS mismatch, a 4xx from the collector — into the log stream. The
// default handler rate-limits a bare log.Println to stderr, outside slog and
// outside the configured format, so a collector that moved three weeks ago
// produces no structured line anywhere and nothing in the backend either
// (the backend being what is unreachable).
//
// The handler is given the stderr sink alone, never the full handler: feeding
// a failing log exporter its own export errors is a feedback loop.
func setExportErrorHandler(stderr slog.Handler) {
	logger := slog.New(stderr)
	var last atomic.Int64
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		now := time.Now().UnixNano()
		prev := last.Load()
		if now-prev < int64(exportErrorInterval) ||
			!last.CompareAndSwap(prev, now) {
			return
		}
		logger.ErrorContext(
			context.Background(),
			"opentelemetry export failed",
			"error", err,
		)
	}))
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
