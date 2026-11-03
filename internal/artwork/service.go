package artwork

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"

	"github.com/datahearth/streamline/internal/buildinfo"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/posters"
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/artwork")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/artwork")

	artCache metric.Int64Counter
	artFetch metric.Int64Counter
)

func init() {
	artCache = otelx.Must(meter.Int64Counter(
		"streamline.posters.cache",
		metric.WithDescription("Poster Serve cache hits/misses by kind + outcome"),
	))
	artFetch = otelx.Must(meter.Int64Counter(
		"streamline.posters.fetch",
		metric.WithDescription("Poster source fetches by kind + outcome"),
	))
	ctx := context.Background()
	artCache.Add(ctx, 0)
	artFetch.Add(ctx, 0)
}

const (
	// maxImage caps one fetched image, as posters does for library art.
	maxImage = 20 * 1024 * 1024
	// cacheMaxAge is how long a cached lookup image is served before it is
	// refetched.
	cacheMaxAge = 24 * time.Hour
	// failureMemory is how long a failed resolve is remembered in memory, so
	// a page of 25 hits does not retry 25 times.
	failureMemory = time.Hour
	// maxCacheEntries bounds the on-disk lookup cache; pruneBatch of the
	// oldest entries go on the next write past it.
	maxCacheEntries = 2000
	pruneBatch      = 200
	maxRedirects    = 5
	// fetchRate and fetchBurst bound the outbound image fetches: any session
	// user can ask for unlimited well-formed keys, and each unseen one costs an
	// upstream request.
	fetchRate  = 10
	fetchBurst = 20
)

var (
	mbidKey   = regexp.MustCompile(`^[0-9a-f-]{36}$`)
	numberKey = regexp.MustCompile(`^[0-9]{1,10}$`)

	// allowedImageHosts are matched as the host itself or a subdomain of it.
	// The URL comes from a provider payload the server cached, never from the
	// browser, so the proxy is not an open fetcher; this is the second guard.
	allowedImageHosts = []string{
		"dzcdn.net",
		"coverartarchive.org",
		"archive.org",
		"assets.hardcover.app",
		"commons.wikimedia.org",
		"upload.wikimedia.org",
	}

	errNoArt = errors.New("artwork: no art for the key")
)

// Library answers whether a lookup key is already a library row, so the
// proxy can serve that row's file instead of fetching a second copy.
type Library interface {
	Find(ctx context.Context, kind Kind, key string) (id uint32, ok bool, err error)
}

// ArtistPictures is the Deezer surface the artists kind resolves through.
type ArtistPictures interface {
	ArtistByID(ctx context.Context, id uint32) (*metadata.DeezerArtist, error)
	SearchArtists(ctx context.Context, name string) ([]metadata.DeezerArtist, error)
}

// ArtistLinks is the MusicBrainz surface that tells namesakes apart: what one
// artist links as its picture, and whether another artist links the same
// Deezer page.
type ArtistLinks interface {
	ArtistArt(ctx context.Context, mbid string) (metadata.ArtistArt, error)
	DeezerArtistShared(ctx context.Context, id uint32) (bool, error)
}

// Deps wires a Service. Sources and Client default to the process-wide
// remembered sources and a copy of otelx.HTTPClient; they are fields so a test
// can swap the transport.
type Deps struct {
	DataDir string
	Posters posters.Manager
	Library Library
	Deezer  ArtistPictures
	Links   ArtistLinks
	Sources *Sources
	Client  *http.Client
}

type Service struct {
	dir     string
	posters posters.Manager
	library Library
	deezer  ArtistPictures
	links   ArtistLinks
	sources *Sources
	client  *http.Client

	flight  singleflight.Group
	limiter *rate.Limiter

	mu     sync.Mutex
	failed map[failKey]time.Time
}

// failKey is a failed resolve: the key and the source it was tried with, so
// a source remembered with more to go on (a Deezer link a detail lookup found)
// is tried again at once rather than an hour later.
type failKey struct {
	sourceKey
	src Source
}

func New(d Deps) (*Service, error) {
	dir := filepath.Join(d.DataDir, "posters", "lookup")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create lookup art dir: %w", err)
	}
	s := &Service{
		dir:     dir,
		posters: d.Posters,
		library: d.Library,
		deezer:  d.Deezer,
		links:   d.Links,
		sources: d.Sources,
		failed:  make(map[failKey]time.Time),
		limiter: rate.NewLimiter(fetchRate, fetchBurst),
	}
	if s.sources == nil {
		s.sources = defaultSources
	}
	c := *otelx.HTTPClient
	if d.Client != nil {
		c = *d.Client
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errors.New("artwork: too many redirects")
		}
		if !imageURLAllowed(req.URL) {
			return fmt.Errorf("artwork: redirect to %s refused", req.URL.Host)
		}
		return nil
	}
	s.client = &c
	return s, nil
}

func validKey(kind Kind, key string) bool {
	switch kind {
	case KindArtists, KindAlbums:
		return mbidKey.MatchString(key)
	case KindBooks:
		return numberKey.MatchString(key)
	}
	return false
}

func imageURLAllowed(u *url.URL) bool {
	if u == nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return slices.ContainsFunc(allowedImageHosts, func(h string) bool {
		return host == h || strings.HasSuffix(host, "."+h)
	})
}

func (s *Service) cachePath(kind Kind, key string) string {
	return filepath.Join(s.dir, string(kind), key, "poster.jpg")
}

func metricKind(kind Kind) string { return "lookup-" + string(kind) }

// Serve answers GET /posters/lookup/{kind}/{key}/poster.jpg: the library row's
// file when the key is already in the library, else the cached lookup image,
// else a fresh fetch. 404 when there is no art, which the SPA renders as its
// placeholder.
func (s *Service) Serve(w http.ResponseWriter, r *http.Request, kind, key string) {
	ctx := r.Context()
	k := Kind(kind)
	record := func(outcome string) {
		artCache.Add(ctx, 1, metric.WithAttributes(
			attribute.String("kind", metricKind(k)),
			attribute.String("outcome", outcome),
		))
	}
	if !validKey(k, key) {
		record("invalid_kind")
		http.NotFound(w, r)
		return
	}

	if s.serveLibrary(w, r, k, key) {
		record("library")
		return
	}
	path := s.cachePath(k, key)
	if st, err := os.Stat(
		path,
	); err == nil &&
		time.Since(st.ModTime()) < cacheMaxAge {
		record("hit")
		serveFile(w, r, path)
		return
	}

	err := s.resolve(ctx, k, key)
	if err == nil {
		record("miss")
		serveFile(w, r, path)
		return
	}
	// A refetch that fails, or a key remembered as failed, still has the
	// stale copy to show.
	if _, statErr := os.Stat(path); statErr == nil {
		record("stale")
		serveFile(w, r, path)
		return
	}
	if errors.Is(err, errNoArt) {
		record("none")
	} else {
		record("error")
	}
	http.NotFound(w, r)
}

func (s *Service) serveLibrary(
	w http.ResponseWriter,
	r *http.Request,
	kind Kind,
	key string,
) bool {
	if s.library == nil {
		return false
	}
	id, ok, err := s.library.Find(r.Context(), kind, key)
	if err != nil {
		slog.WarnContext(r.Context(), "lookup art: library check failed",
			"poster.kind", string(kind), "error", err)
		return false
	}
	if !ok {
		return false
	}
	if st, err := os.Stat(
		s.posters.Path(string(kind), id),
	); err != nil ||
		st.Size() == 0 {
		return false
	}
	s.posters.Serve(w, r, string(kind), id)
	return true
}

func serveFile(w http.ResponseWriter, r *http.Request, path string) {
	//nolint:gosec // path is built from a kind and key validated against fixed patterns
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, "poster.jpg", st.ModTime(), f)
}

// resolve fetches and caches the image of (kind, key). Concurrent requests for
// one key collapse into one fetch.
func (s *Service) resolve(ctx context.Context, kind Kind, key string) error {
	sk := failKey{sourceKey: sourceKey{kind, key}}
	sk.src, _ = s.sources.Lookup(kind, key)
	if s.recentlyFailed(sk) {
		return errNoArt
	}
	_, err, _ := s.flight.Do(string(kind)+"/"+key, func() (any, error) {
		return nil, s.fetch(ctx, kind, key)
	})
	// A provider asking to be left alone has said nothing about the art: the
	// next request tries again rather than an hour later.
	if err != nil && !errors.Is(err, context.Canceled) &&
		!errors.Is(err, metadata.ErrRateLimited) {
		s.markFailed(sk)
	}
	return err
}

func (s *Service) recentlyFailed(k failKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.failed[k]
	if !ok {
		return false
	}
	if time.Since(at) > failureMemory {
		delete(s.failed, k)
		return false
	}
	return true
}

func (s *Service) markFailed(k failKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.failed) >= maxCacheEntries {
		for key, at := range s.failed {
			if time.Since(at) > failureMemory {
				delete(s.failed, key)
			}
		}
	}
	for len(s.failed) >= maxCacheEntries {
		var oldest failKey
		var oldestAt time.Time
		for key, at := range s.failed {
			if oldestAt.IsZero() || at.Before(oldestAt) {
				oldest, oldestAt = key, at
			}
		}
		delete(s.failed, oldest)
	}
	s.failed[k] = time.Now()
}

func (s *Service) fetch(ctx context.Context, kind Kind, key string) error {
	ctx, span := tracer.Start(ctx, "artwork.resolve")
	defer span.End()
	span.SetAttributes(attribute.String("poster.kind", metricKind(kind)))
	span.SetAttributes(attribute.String("artwork.key", key))

	outcome := "ok"
	defer func() {
		artFetch.Add(ctx, 1, metric.WithAttributes(
			attribute.String("poster.kind", metricKind(kind)),
			attribute.String("outcome", outcome),
		))
	}()

	if err := s.limiter.Wait(ctx); err != nil {
		outcome = "error"
		return otelx.RecordSpanError(span, err)
	}

	src, err := s.imageURL(ctx, kind, key)
	if err != nil {
		if errors.Is(err, errNoArt) {
			outcome = "none"
			return err
		}
		outcome = "error"
		return otelx.RecordSpanError(span, err)
	}
	if err := s.download(ctx, src, s.cachePath(kind, key)); err != nil {
		outcome = "error"
		return otelx.RecordSpanError(span, err)
	}
	s.prune(ctx)
	return nil
}

// imageURL resolves the image URL of (kind, key) from what the lookup
// services remembered; albums need no memory, the key is the release-group id.
func (s *Service) imageURL(
	ctx context.Context,
	kind Kind,
	key string,
) (string, error) {
	switch kind {
	case KindAlbums:
		return metadata.CoverArtURL(key), nil
	case KindBooks:
		src, ok := s.sources.Lookup(kind, key)
		if !ok || src.URL == "" {
			return "", errNoArt
		}
		return src.URL, nil
	case KindArtists:
		src, ok := s.sources.Lookup(kind, key)
		if !ok {
			return "", errNoArt
		}
		return s.artistPicture(ctx, key, src)
	}
	return "", errNoArt
}

// artistPicture settles a name Deezer knows exactly one artist by without
// asking MusicBrainz; a namesake makes that answer a guess, and a wrong face is
// worse than none. Otherwise it reads what MusicBrainz links for this very
// artist, at prefetch priority, behind any request a person is waiting on: its
// Deezer page unless another MusicBrainz artist links the same one (Deezer
// files namesakes under one page, whose picture is only ever one of them),
// else its own Commons image. A search result never carries those links, and
// without them a common name ("Nirvana") left every hit faceless, the famous
// one included.
func (s *Service) artistPicture(
	ctx context.Context,
	mbid string,
	src Source,
) (string, error) {
	if src.DeezerID == 0 && src.Name != "" && s.deezer != nil {
		hits, err := s.deezer.SearchArtists(ctx, src.Name)
		if err != nil {
			return "", err
		}
		if match := soleNameMatch(hits, src.Name); match != nil {
			if match.PictureURL == "" {
				return "", errNoArt
			}
			return match.PictureURL, nil
		}
	}
	if s.links == nil {
		return s.deezerPicture(ctx, src.DeezerID)
	}

	mbCtx := metadata.Prefetch(ctx)
	var art *metadata.ArtistArt
	links := func() (metadata.ArtistArt, error) {
		if art == nil {
			a, err := s.links.ArtistArt(mbCtx, mbid)
			if errors.Is(err, metadata.ErrNotFound) {
				err = errNoArt
			}
			if err != nil {
				return metadata.ArtistArt{}, err
			}
			art = &a
		}
		return *art, nil
	}
	id := src.DeezerID
	if id == 0 {
		a, err := links()
		if err != nil {
			return "", err
		}
		id = a.DeezerID
	}
	if id != 0 {
		shared, err := s.links.DeezerArtistShared(mbCtx, id)
		if err != nil {
			return "", err
		}
		if !shared {
			u, err := s.deezerPicture(ctx, id)
			if !errors.Is(err, errNoArt) {
				return u, err
			}
		}
	}
	a, err := links()
	if err != nil {
		return "", err
	}
	if a.ImageURL == "" {
		return "", errNoArt
	}
	return a.ImageURL, nil
}

func (s *Service) deezerPicture(ctx context.Context, id uint32) (string, error) {
	if id == 0 || s.deezer == nil {
		return "", errNoArt
	}
	a, err := s.deezer.ArtistByID(ctx, id)
	if err != nil {
		return "", err
	}
	if a == nil || a.PictureURL == "" {
		return "", errNoArt
	}
	return a.PictureURL, nil
}

// soleNameMatch is the one hit whose name folds equal to name, nil when there
// is none or more than one.
func soleNameMatch(
	hits []metadata.DeezerArtist,
	name string,
) *metadata.DeezerArtist {
	var match *metadata.DeezerArtist
	for i := range hits {
		if !library.TitleMatchesStrict(hits[i].Name, name) {
			continue
		}
		if match != nil {
			return nil
		}
		match = &hits[i]
	}
	return match
}

func (s *Service) download(ctx context.Context, src, dst string) error {
	u, err := url.Parse(src)
	if err != nil || !imageURLAllowed(u) {
		return fmt.Errorf("artwork: image host not allowed: %q", src)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return err
	}
	// Wikimedia refuses a generic client; the header follows every redirect.
	version := buildinfo.Version
	if version == "" {
		version = "dev"
	}
	req.Header.Set("User-Agent",
		"streamline/"+version+" (https://github.com/datahearth/streamline)")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errNoArt
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("artwork: image source status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return fmt.Errorf("artwork: image source sent %q", ct)
	}
	return store(ctx, dst, resp.Body)
}

// store writes r to dst through a temp file in the same directory, so a failed
// or oversized copy never leaves a partial image behind.
func store(ctx context.Context, dst string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("mkdir lookup art dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "art-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	name := tmp.Name()
	discard := func() {
		if err := os.Remove(name); err != nil {
			slog.WarnContext(ctx, "lookup art: remove temp file failed",
				"path", name, "error", err)
		}
	}
	n, err := io.Copy(tmp, io.LimitReader(r, maxImage+1))
	if err != nil {
		tmp.Close()
		discard()
		return fmt.Errorf("copy body: %w", err)
	}
	if n > maxImage {
		tmp.Close()
		discard()
		return fmt.Errorf("artwork: image exceeds %d bytes", maxImage)
	}
	if err := tmp.Close(); err != nil {
		discard()
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		discard()
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

type cacheEntry struct {
	dir string
	at  time.Time
}

// prune drops the oldest pruneBatch entries by mtime once the cache holds more
// than maxCacheEntries. It runs after a write and only logs: the image is
// already stored.
func (s *Service) prune(ctx context.Context) {
	var entries []cacheEntry
	for _, kind := range []Kind{KindArtists, KindAlbums, KindBooks} {
		dir := filepath.Join(s.dir, string(kind))
		items, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, it := range items {
			info, err := it.Info()
			if err != nil {
				continue
			}
			entries = append(
				entries,
				cacheEntry{filepath.Join(dir, it.Name()), info.ModTime()},
			)
		}
	}
	if len(entries) <= maxCacheEntries {
		return
	}
	slices.SortFunc(entries, func(a, b cacheEntry) int { return a.at.Compare(b.at) })
	for _, e := range entries[:pruneBatch] {
		if err := os.RemoveAll(e.dir); err != nil {
			slog.WarnContext(
				ctx,
				"lookup art: prune failed",
				"path",
				e.dir,
				"error",
				err,
			)
		}
	}
}
