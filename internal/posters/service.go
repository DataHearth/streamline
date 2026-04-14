// Package posters caches artwork on the local filesystem and serves it via
// HTTP. Keeps zero dependencies outside stdlib + otelx so the leaf can be
// imported by any package.
package posters

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/posters")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/posters")

	posterCache metric.Int64Counter
)

func init() {
	posterCache = otelx.Must(meter.Int64Counter(
		"streamline.posters.cache",
		metric.WithDescription("Poster Serve cache hits/misses by kind + outcome"),
	))
	posterCache.Add(context.Background(), 0)
}

// validKinds doubles as a path-traversal guard: any kind outside this set
// is rejected before touching the filesystem or the HTTP response.
var validKinds = map[string]struct{}{
	"movies":  {},
	"tvshows": {},
}

// Manager is the consumer-facing surface for the poster cache: fetch on
// movie add, serve on HTTP request, resolve cache paths.
type Manager interface {
	Fetch(ctx context.Context, kind string, id uint32, src string) error
	Serve(w http.ResponseWriter, r *http.Request, kind string, id uint32)
	Path(kind string, id uint32) string
}

type posters struct {
	dataDir string
	client  *http.Client
}

func New(dataDir string) (Manager, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "posters"), 0o755); err != nil {
		return nil, fmt.Errorf("create posters dir: %w", err)
	}
	return &posters{dataDir: dataDir, client: otelx.HTTPClient}, nil
}

func (p *posters) Path(kind string, id uint32) string {
	return filepath.Join(
		p.dataDir,
		"posters",
		kind,
		strconv.FormatUint(uint64(id), 10),
		"poster.jpg",
	)
}

// Fetch is idempotent: returns nil without a network call when dst already
// exists with non-zero size.
func (p *posters) Fetch(
	ctx context.Context,
	kind string,
	id uint32,
	src string,
) error {
	if _, ok := validKinds[kind]; !ok {
		return fmt.Errorf("posters: invalid kind %q", kind)
	}
	ctx, span := tracer.Start(ctx, "posters.fetch")
	defer span.End()
	span.SetAttributes(
		attribute.String("poster.kind", kind),
		attribute.Int64("poster.id", int64(id)),
		attribute.String("poster.src", src),
	)

	dst := p.Path(kind, id)
	if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("mkdir poster dir: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("poster source status %d", resp.StatusCode),
		)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), "poster-*.tmp")
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("create temp: %w", err))
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return otelx.RecordSpanError(span, fmt.Errorf("copy body: %w", err))
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return otelx.RecordSpanError(span, err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return otelx.RecordSpanError(span, fmt.Errorf("rename: %w", err))
	}
	slog.InfoContext(
		ctx,
		"poster fetched",
		"poster.kind",
		kind,
		"poster.id",
		id,
		"source.url",
		src,
		"cache.path",
		dst,
	)
	return nil
}

func (p *posters) Serve(
	w http.ResponseWriter,
	r *http.Request,
	kind string,
	id uint32,
) {
	ctx := r.Context()
	record := func(outcome string) {
		posterCache.Add(ctx, 1, metric.WithAttributes(
			attribute.String("kind", kind),
			attribute.String("outcome", outcome),
		))
	}
	if _, ok := validKinds[kind]; !ok {
		record("invalid_kind")
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(p.Path(kind, id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			record("miss")
			http.NotFound(w, r)
			return
		}
		record("error")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		record("error")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	record("hit")
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, "poster.jpg", st.ModTime(), f)
}
