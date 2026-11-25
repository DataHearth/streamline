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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/datahearth/streamline/internal/buildinfo"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/posters")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/posters")

	posterCache metric.Int64Counter
	posterFetch metric.Int64Counter
)

func init() {
	posterCache = otelx.Must(meter.Int64Counter(
		"streamline.posters.cache",
		metric.WithDescription("Poster Serve cache hits/misses by kind + outcome"),
	))
	posterFetch = otelx.Must(meter.Int64Counter(
		"streamline.posters.fetch",
		metric.WithDescription("Poster source fetches by kind + outcome"),
	))
	ctx := context.Background()
	posterCache.Add(ctx, 0)
	posterFetch.Add(ctx, 0)
}

// maxPosterSize caps the fetched artwork. The TV path forwards whatever
// TVDBArtworkURL returns — an arbitrary host — so a hostile or compromised
// metadata provider must not be able to fill the data volume.
const maxPosterSize = 20 * 1024 * 1024

var (
	errTooLarge = fmt.Errorf("poster exceeds %d byte cap", maxPosterSize)
	errNotImage = errors.New("image source did not answer an image")
)

// validKinds doubles as a path-traversal guard: any kind outside this set
// is rejected before touching the filesystem or the HTTP response.
var validKinds = map[string]struct{}{
	"movies":  {},
	"tvshows": {},
	"artists": {},
	"albums":  {},
	"authors": {},
	"books":   {},
	"people":  {},
}

// Store caches one image per row: its poster, or its logo.
type Store interface {
	Fetch(ctx context.Context, kind string, id uint32, src string) error
	// Follow keeps the cached image on the addresses its provider offers: one
	// that came from any of srcs stays, and anything else is replaced by the
	// first. With none the row has no image, and the cached one goes.
	Follow(ctx context.Context, kind string, id uint32, srcs ...string) error
	// Put stores bytes the caller already holds, replacing any cached image.
	Put(ctx context.Context, kind string, id uint32, r io.Reader) error
	Serve(w http.ResponseWriter, r *http.Request, kind string, id uint32)
	Path(kind string, id uint32) string
	// Has reports whether the row's image is cached.
	Has(kind string, id uint32) bool
}

// Manager is the consumer-facing surface of the image cache: one Store per
// image a row can have, all in the row's folder.
type Manager interface {
	// Posters caches what stands for a row in a list: a title's poster, an
	// album's or a book's cover, a person's photo.
	Posters() Store
	// Logos caches the titles' transparent logos.
	Logos() Store
	// Remove drops every image cached for the row.
	Remove(kind string, id uint32) error
	// Refresh brings a title's poster and logo in line with what its provider
	// offers now: the poster follows its address, or stays when there is none,
	// and the logo follows logos as Store.Follow does. The row counts as being
	// fetched from this call until the returned func, which does the work and
	// is meant for a goroutine, has run.
	Refresh(
		kind string,
		id uint32,
		poster string,
		logos []string,
	) func(ctx context.Context)
	// HasLogo reports whether the row's logo is cached, once a Refresh of the
	// row still in flight has run: the request that follows an add or a
	// re-identify would otherwise read the cache before the logo lands. It
	// waits logoWait at most.
	HasLogo(ctx context.Context, kind string, id uint32) bool
}

// logoWait bounds how long HasLogo waits for a Refresh in flight.
const logoWait = 2 * time.Second

// asset is what tells one Store's files from another's in a row's folder.
type asset struct {
	file string
	// source holds the address the cached file came from, for Follow.
	source string
	// imageOnly refuses a body that does not sniff as an image. A logo source
	// can answer SVG, which Serve would hand out as image/jpeg.
	imageOnly bool
}

var (
	posterAsset = asset{file: "poster.jpg", source: "source"}
	logoAsset   = asset{file: "logo.png", source: "logo.source", imageOnly: true}
)

// cache is what the Stores share: the folder the images live in and the client
// that fetches them. The folder is under data_dir, which only the config file
// sets, so it holds for the life of the process.
type cache struct {
	dir    string
	client *http.Client
}

// rowDir is the folder holding every image of one row, whichever Store wrote
// it.
func (c *cache) rowDir(kind string, id uint32) string {
	return filepath.Join(c.dir, kind, strconv.FormatUint(uint64(id), 10))
}

type store struct {
	*cache
	asset asset
}

type manager struct {
	*cache
	posters *store
	logos   *store

	mu      sync.Mutex
	flights map[string]*flight
}

// flight is the fetches in progress for one row's folder.
type flight struct {
	n    int
	done chan struct{}
}

func New(dataDir string) (Manager, error) {
	c := &cache{
		dir:    filepath.Join(dataDir, "posters"),
		client: otelx.HTTPClient,
	}
	if err := os.MkdirAll(c.dir, 0o750); err != nil {
		return nil, fmt.Errorf("create posters dir: %w", err)
	}
	return &manager{
		cache:   c,
		posters: &store{cache: c, asset: posterAsset},
		logos:   &store{cache: c, asset: logoAsset},
		flights: map[string]*flight{},
	}, nil
}

func (m *manager) Refresh(
	kind string,
	id uint32,
	poster string,
	logos []string,
) func(context.Context) {
	done := m.fetching(kind, id)
	logos = slices.DeleteFunc(slices.Clone(logos), func(src string) bool {
		return src == ""
	})
	return func(ctx context.Context) {
		defer done()
		if poster != "" {
			if err := m.posters.Follow(ctx, kind, id, poster); err != nil {
				slog.WarnContext(ctx, "poster fetch failed",
					"poster.kind", kind, "poster.id", id, "error", err)
			}
		}
		if err := m.logos.Follow(ctx, kind, id, logos...); err != nil {
			slog.WarnContext(ctx, "logo fetch failed",
				"poster.kind", kind, "poster.id", id, "error", err)
		}
	}
}

func (m *manager) HasLogo(ctx context.Context, kind string, id uint32) bool {
	ctx, cancel := context.WithTimeout(ctx, logoWait)
	defer cancel()
	m.settled(ctx, kind, id)
	return m.logos.Has(kind, id)
}

// fetching marks the row's images as being fetched until the returned func is
// called, and settled waits until none is, or ctx ends.
func (m *manager) fetching(kind string, id uint32) func() {
	key := m.rowDir(kind, id)
	m.mu.Lock()
	f := m.flights[key]
	if f == nil {
		f = &flight{done: make(chan struct{})}
		m.flights[key] = f
	}
	f.n++
	m.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if f.n--; f.n == 0 {
				delete(m.flights, key)
				close(f.done)
			}
		})
	}
}

func (m *manager) settled(ctx context.Context, kind string, id uint32) {
	key := m.rowDir(kind, id)
	for {
		m.mu.Lock()
		f := m.flights[key]
		m.mu.Unlock()
		if f == nil {
			return
		}
		select {
		case <-f.done:
		case <-ctx.Done():
			return
		}
	}
}

func (m *manager) Posters() Store {
	return m.posters
}

func (m *manager) Logos() Store {
	return m.logos
}

func (s *store) Path(kind string, id uint32) string {
	return s.file(kind, id, s.asset.file)
}

func (s *store) sourcePath(kind string, id uint32) string {
	return s.file(kind, id, s.asset.source)
}

// refusedPath holds the last address an image-only Store refused, so a
// refresh does not download it again.
func (s *store) refusedPath(kind string, id uint32) string {
	return s.file(kind, id, s.asset.file+".refused")
}

func (s *store) file(kind string, id uint32, name string) string {
	return filepath.Join(s.rowDir(kind, id), name)
}

func (s *store) Has(kind string, id uint32) bool {
	st, err := os.Stat(s.Path(kind, id))
	return err == nil && st.Size() > 0
}

// Remove drops the row's folder, so every Store's image goes with the addresses
// kept beside them. SQLite reuses the rowid of a deleted record, so a cache
// entry left behind outlives its media and is served — verbatim, because Fetch
// short-circuits on an existing file — as the artwork of the next movie or
// show that lands on the same id.
func (m *manager) Remove(kind string, id uint32) error {
	if _, ok := validKinds[kind]; !ok {
		return fmt.Errorf("posters: invalid kind %q", kind)
	}
	if err := os.RemoveAll(m.rowDir(kind, id)); err != nil {
		return fmt.Errorf("remove image cache: %w", err)
	}
	return nil
}

// Fetch is idempotent: returns nil without a network call when dst already
// exists with non-zero size.
func (s *store) Fetch(
	ctx context.Context,
	kind string,
	id uint32,
	src string,
) error {
	return s.fetch(ctx, kind, id, []string{src}, false)
}

// Follow records the address next to the image, not in the database. An
// image cached before addresses were kept is taken as the first address's:
// comparing would refetch the whole library at once. Keeping an image that
// came from any of srcs is what stops a logo from changing each time its
// provider reorders the ones it offers.
func (s *store) Follow(
	ctx context.Context,
	kind string,
	id uint32,
	srcs ...string,
) error {
	return s.fetch(ctx, kind, id, srcs, true)
}

func (s *store) fetch(
	ctx context.Context,
	kind string,
	id uint32,
	srcs []string,
	follow bool,
) error {
	if _, ok := validKinds[kind]; !ok {
		return fmt.Errorf("posters: invalid kind %q", kind)
	}
	ctx, span := tracer.Start(ctx, "posters.fetch")
	defer span.End()
	span.SetAttributes(
		attribute.String("poster.kind", kind),
		attribute.Int64("poster.id", int64(id)),
		attribute.StringSlice("poster.src", srcs),
		attribute.String("poster.asset", s.asset.file),
	)

	// Serve counts cache hits and misses, and the outbound fetch behind a miss
	// is counted too: with only a span, an artwork CDN degrading — timeouts,
	// 403s, oversized bodies — could be seen in a trace and thresholded nowhere.
	outcome, err := s.sync(ctx, kind, id, srcs, follow)
	if err != nil && outcome == "" {
		outcome = "error"
	}
	posterFetch.Add(ctx, 1, metric.WithAttributes(
		attribute.String("poster.kind", kind),
		attribute.String("poster.asset", s.asset.file),
		attribute.String("outcome", outcome),
	))
	return otelx.RecordSpanError(span, err)
}

// sync brings the cached image in line with srcs and names what it did for the
// fetch counter. A failure it has no name for comes back with an empty one.
func (s *store) sync(
	ctx context.Context,
	kind string,
	id uint32,
	srcs []string,
	follow bool,
) (string, error) {
	if len(srcs) == 0 {
		return s.drop(kind, id)
	}
	src := srcs[0]
	dst := s.Path(kind, id)
	if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
		if !follow {
			return "cached", nil
		}
		known, err := os.ReadFile(s.sourcePath(kind, id))
		switch {
		case err == nil && slices.Contains(srcs, string(known)):
			return "cached", nil
		case errors.Is(err, os.ErrNotExist):
			err := os.WriteFile(s.sourcePath(kind, id), []byte(src), 0o600)
			if err != nil {
				return "", err
			}
			return "cached", nil
		case err != nil:
			return "", err
		}
	}
	if follow && s.asset.imageOnly {
		refused, err := os.ReadFile(s.refusedPath(kind, id))
		if err == nil && string(refused) == src {
			return "refused", nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return "", err
	}
	// Wikimedia, where an artist photo can come from, refuses a generic
	// client; the header follows every redirect.
	version := buildinfo.Version
	if version == "" {
		version = "dev"
	}
	req.Header.Set("User-Agent",
		"streamline/"+version+" (https://github.com/datahearth/streamline)")
	resp, err := s.client.Do(req)
	if err != nil {
		return "transport_error", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "http_error", fmt.Errorf("poster source status %d", resp.StatusCode)
	}

	if err := s.write(dst, resp.Body, follow); err != nil {
		switch {
		case errors.Is(err, errTooLarge):
			return "too_large", err
		case errors.Is(err, errNotImage):
			if follow {
				_ = os.WriteFile(s.refusedPath(kind, id), []byte(src), 0o600)
			}
			return "not_image", err
		}
		return "", err
	}
	if follow {
		err := os.WriteFile(s.sourcePath(kind, id), []byte(src), 0o600)
		if err != nil {
			return "", err
		}
		_ = os.Remove(s.refusedPath(kind, id))
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
	return "ok", nil
}

// drop removes the row's image and what is kept beside it.
func (s *store) drop(kind string, id uint32) (string, error) {
	outcome := "dropped"
	if err := os.Remove(s.Path(kind, id)); errors.Is(err, os.ErrNotExist) {
		outcome = "none"
	} else if err != nil {
		return "", err
	}
	for _, beside := range []string{
		s.sourcePath(kind, id),
		s.refusedPath(kind, id),
	} {
		err := os.Remove(beside)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return outcome, nil
}

// Put replaces the cached image, unlike Fetch, which keeps one that exists:
// the caller holds the bytes, so there is no network call to save, and a
// picture embedded in the album's own files outranks a downloaded one.
func (s *store) Put(
	ctx context.Context,
	kind string,
	id uint32,
	r io.Reader,
) error {
	if _, ok := validKinds[kind]; !ok {
		return fmt.Errorf("posters: invalid kind %q", kind)
	}
	_, span := tracer.Start(ctx, "posters.put")
	defer span.End()
	span.SetAttributes(
		attribute.String("poster.kind", kind),
		attribute.Int64("poster.id", int64(id)),
	)
	return otelx.RecordSpanError(span, s.write(s.Path(kind, id), r, true))
}

// write copies r to dst through a temp file in the same directory, so a
// failed, oversized or refused copy never leaves a partial image behind nor
// replaces a good one. Without replace, an image that appeared while r was
// being read wins: an album's own art stored by Put must not be undone by a
// slower remote fetch.
func (s *store) write(dst string, r io.Reader, replace bool) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("mkdir poster dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "poster-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	n, err := io.Copy(tmp, io.LimitReader(r, maxPosterSize+1))
	if err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("copy body: %w", err)
	}
	if n > maxPosterSize {
		tmp.Close()
		_ = os.Remove(tmpName)
		return errTooLarge
	}
	if s.asset.imageOnly && !isImage(tmp) {
		tmp.Close()
		_ = os.Remove(tmpName)
		return errNotImage
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if !replace {
		if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
			return os.Remove(tmpName)
		}
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// sniffImageType reports the cached file's real image type: sources hand back
// PNG, WebP or GIF as readily as JPEG, whatever the cached file is named.
// Anything that does not sniff as an image keeps image/jpeg, so a hostile body
// is never served as a type a browser would render as a page.
func sniffImageType(f *os.File) string {
	t, ok := sniff(f)
	if !ok {
		return "image/jpeg"
	}
	return t
}

func isImage(f *os.File) bool {
	_, ok := sniff(f)
	return ok
}

func sniff(f *os.File) (string, bool) {
	head := make([]byte, 512)
	n, err := f.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", false
	}
	t := http.DetectContentType(head[:n])
	return t, strings.HasPrefix(t, "image/")
}

func (s *store) Serve(
	w http.ResponseWriter,
	r *http.Request,
	kind string,
	id uint32,
) {
	ctx := r.Context()
	// The kind comes from the URL: one the cache does not know is counted
	// under a single name, never as itself.
	label := kind
	if _, ok := validKinds[kind]; !ok {
		label = "invalid"
	}
	record := func(outcome string) {
		posterCache.Add(ctx, 1, metric.WithAttributes(
			attribute.String("kind", label),
			attribute.String("asset", s.asset.file),
			attribute.String("outcome", outcome),
		))
	}
	if _, ok := validKinds[kind]; !ok {
		record("invalid_kind")
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(s.Path(kind, id))
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
	w.Header().Set("Content-Type", sniffImageType(f))
	// One URL holds another picture after a refresh or once a deleted title's
	// id is reused, so the browser revalidates every time (a 304 when unchanged).
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, s.asset.file, st.ModTime(), f)
}
