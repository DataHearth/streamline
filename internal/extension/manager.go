package extension

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("github.com/datahearth/streamline/internal/extension")

type State string

const (
	StateRunning       State = "running"
	StateDisabled      State = "disabled"
	StateNeedsApproval State = "needs_approval"
	StateFailed        State = "failed"
)

type Status struct {
	Name     string
	State    State
	Reason   string
	Manifest *Manifest
}

// Grants is what an entry must hold for the module to run.
func (mf Manifest) Grants() config.ExtensionGrants {
	g := config.ExtensionGrants{
		Views: mf.Views, Services: mf.Services, Folders: mf.Folders,
	}
	for _, m := range mf.Mounts {
		g.Mounts = append(g.Mounts, m.Prefix)
	}
	return g
}

// reservedPrefixes are the core's own first path segments, plus every
// first-level SPA route (the spec in manager_test.go fails when a route
// directory is missing here).
var reservedPrefixes = []string{
	"/api", "/static", "/health", "/login", "/register", "/auth", "/posters",
	"/search", "/forbidden",
	"/account", "/activity", "/books", "/calendar", "/imports", "/movies",
	"/music", "/people", "/requests", "/series", "/settings", "/stats",
	"/torrents", "/transcoding",
}

const (
	folderMusic        = "music"
	folderMovies       = "movies"
	folderSeries       = "series"
	folderArtwork      = "artwork"
	folderCache        = "cache"
	serviceFFmpeg      = "ffmpeg"
	serviceLibraryScan = "library_scan"
	serviceHTTP        = "http"
)

var (
	knownFolders = []string{
		folderMusic, folderMovies, folderSeries, folderArtwork, folderCache,
	}
	knownServices = []string{
		serviceServeFile,
		serviceServePoster,
		serviceFFmpeg,
		serviceLibraryScan,
		serviceHTTP,
	}
)

// Running is one live extension. A request holds it between Acquire and
// Release; a replaced or disabled one is closed when the last holder lets go.
type Running struct {
	mgr      *Manager
	name     string
	module   *Module
	store    *Store
	hash     [sha256.Size]byte
	grants   config.ExtensionGrants
	mu       sync.Mutex
	refs     int
	retired  bool
	closed   chan struct{}
	closeErr error

	retiring    chan struct{}
	ffmpegSlots chan struct{}
}

func newRunning(
	mgr *Manager,
	name string,
	m *Module,
	s *Store,
	hash [sha256.Size]byte,
	g config.ExtensionGrants,
) *Running {
	return &Running{
		mgr:    mgr,
		name:   name,
		module: m,
		store:  s,
		hash:   hash,
		grants: g,
		closed: make(chan struct{}),

		retiring:    make(chan struct{}),
		ffmpegSlots: make(chan struct{}, MaxFFmpegProcs),
	}
}

func (r *Running) Name() string          { return r.name }
func (r *Running) Module() *Module       { return r.module }
func (r *Running) Store() *Store         { return r.store }
func (r *Running) Done() <-chan struct{} { return r.closed }

func (r *Running) acquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.retired {
		return false
	}
	r.refs++
	return true
}

func (r *Running) Release() {
	r.mu.Lock()
	r.refs--
	last := r.retired && r.refs == 0
	r.mu.Unlock()
	if last {
		r.close()
	}
}

func (r *Running) retire() {
	r.mu.Lock()
	if !r.retired {
		close(r.retiring)
	}
	r.retired = true
	idle := r.refs == 0
	r.mu.Unlock()
	if idle {
		r.close()
	}
}

func (r *Running) close() {
	ctx := context.Background()
	err := r.module.Close(ctx)
	r.closeErr = errors.Join(err, r.store.Close())
	close(r.closed)
}

type mountEntry struct {
	running *Running
	mount   Mount
}

// Tools is where the host's ffmpeg suite lives; *ffmpeg.CLI satisfies it.
type Tools interface {
	FFmpegPath() string
	// ResolvedPath is ffprobe's.
	ResolvedPath() string
}

type Manager struct {
	dataDir    string
	coreDBPath string
	tools      Tools
	scanner    Scanner

	reconcile sync.Mutex
	running   map[string]*Running
	statuses  []Status
	paths     map[string]string
	table     atomic.Pointer[map[string]mountEntry]
	stopped   atomic.Pointer[map[string]bool]
	state     sync.RWMutex
}

func NewManager(
	dataDir, coreDBPath string,
	tools Tools,
	scanner Scanner,
) *Manager {
	m := &Manager{
		dataDir:    dataDir,
		coreDBPath: coreDBPath,
		tools:      tools,
		scanner:    scanner,
		running:    map[string]*Running{},
		paths:      map[string]string{},
	}
	empty := map[string]mountEntry{}
	m.table.Store(&empty)
	m.stopped.Store(&map[string]bool{})
	return m
}

func (m *Manager) sourcesDir() string {
	return filepath.Join(m.dataDir, "extensions", "_sources")
}

func (m *Manager) dataPath(name string) string {
	return filepath.Join(m.dataDir, "extensions", name, "data.db")
}

func (m *Manager) Inspect(
	ctx context.Context,
	e config.ExtensionEntry,
) (Manifest, error) {
	wasm, err := resolveSource(ctx, e, m.sourcesDir())
	if err != nil {
		return Manifest{}, err
	}
	mod, err := Compile(ctx, wasm)
	if err != nil {
		return Manifest{}, err
	}
	mf := mod.Manifest()
	if err := mod.Close(ctx); err != nil {
		return Manifest{}, err
	}
	return mf, nil
}

func (m *Manager) Reconcile(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "extension.reconcile")
	defer span.End()

	m.reconcile.Lock()
	defer m.reconcile.Unlock()

	cfg := config.Get()
	if cfg == nil {
		return nil
	}

	next := map[string]*Running{}
	table := map[string]mountEntry{}
	statuses := make([]Status, 0, len(cfg.Extensions))
	paths := make(map[string]string, len(cfg.Extensions))

	for _, e := range cfg.Extensions {
		if err := ctx.Err(); err != nil {
			return otelx.RecordSpanError(span, err)
		}
		paths[e.Name] = m.dataPath(e.Name)
		st, run := m.reconcileOne(ctx, e, table)
		if run != nil {
			next[e.Name] = run
		}
		if st.State == StateFailed {
			slog.WarnContext(
				ctx,
				"Extension did not start",
				"extension",
				e.Name,
				"reason",
				st.Reason,
			)
		}
		statuses = append(statuses, st)
	}

	stopped := map[string]bool{}
	for i, e := range cfg.Extensions {
		claimed := e.Grants.Mounts
		if mf := statuses[i].Manifest; mf != nil {
			claimed = append(slices.Clone(claimed), mf.Grants().Mounts...)
		}
		for _, p := range claimed {
			_, live := table[p]
			if !live && mountable(p) == nil {
				stopped[p] = true
			}
		}
	}

	old := m.running
	m.state.Lock()
	m.running = next
	m.statuses = statuses
	m.paths = paths
	m.table.Store(&table)
	m.stopped.Store(&stopped)
	m.state.Unlock()

	for name, r := range old {
		if next[name] != r {
			r.retire()
		}
	}
	return nil
}

func (m *Manager) reconcileOne(
	ctx context.Context,
	e config.ExtensionEntry,
	table map[string]mountEntry,
) (Status, *Running) {
	st := Status{Name: e.Name}
	fail := func(format string, a ...any) (Status, *Running) {
		st.State = StateFailed
		st.Reason = fmt.Sprintf(format, a...)
		return st, nil
	}

	if !e.Enabled {
		st.State = StateDisabled
		return st, nil
	}
	if err := checkGrants(e.Grants); err != nil {
		return fail("%s", err)
	}
	wasm, err := resolveSource(ctx, e, m.sourcesDir())
	if err != nil {
		return fail("%s", err)
	}
	hash := sha256.Sum256(wasm)

	prev := m.running[e.Name]
	var (
		mod   *Module
		reuse bool
	)
	if prev != nil && prev.hash == hash && sameGrants(prev.grants, e.Grants) {
		mod, reuse = prev.module, true
	} else if mod, err = Compile(ctx, wasm); err != nil {
		return fail("%s", err)
	}
	mf := mod.Manifest()
	st.Manifest = &mf
	discard := func() {
		if !reuse {
			_ = mod.Close(ctx)
		}
	}

	if mf.Name != e.Name {
		discard()
		return fail("the module names itself %q, not %q", mf.Name, e.Name)
	}
	if !covers(e.Grants, mf.Grants()) {
		discard()
		st.State = StateNeedsApproval
		st.Reason = "the module asks for more than it was granted"
		return st, nil
	}
	if err := checkMounts(mf, table); err != nil {
		discard()
		return fail("%s", err)
	}

	run := prev
	if !reuse {
		store, err := OpenStore(
			ctx,
			m.dataPath(e.Name),
			m.coreDBPath,
			grantedViews(e),
		)
		if err != nil {
			discard()
			if errors.Is(err, ErrCoreInMemory) {
				return fail("extensions need an on-disk core database")
			}
			return fail("open the extension database: %s", err)
		}
		run = newRunning(m, e.Name, mod, store, hash, e.Grants)
	}
	for _, mt := range mf.Mounts {
		table[mt.Prefix] = mountEntry{running: run, mount: mt}
	}
	st.State = StateRunning
	return st, run
}

func checkGrants(g config.ExtensionGrants) error {
	for _, v := range g.Views {
		if _, ok := db.ExtensionViewByName(v); !ok {
			return fmt.Errorf("granted view %q does not exist", v)
		}
	}
	for _, s := range g.Services {
		if !slices.Contains(knownServices, s) {
			return fmt.Errorf("granted service %q does not exist", s)
		}
	}
	for _, f := range g.Folders {
		if !slices.Contains(knownFolders, f) {
			return fmt.Errorf("granted folder %q does not exist", f)
		}
	}
	return nil
}

func covers(held, asked config.ExtensionGrants) bool {
	subset := func(asked, held []string) bool {
		return !slices.ContainsFunc(asked, func(x string) bool {
			return !slices.Contains(held, x)
		})
	}
	return subset(asked.Mounts, held.Mounts) &&
		subset(asked.Views, held.Views) &&
		subset(asked.Services, held.Services) &&
		subset(asked.Folders, held.Folders)
}

func checkMounts(mf Manifest, table map[string]mountEntry) error {
	for _, mt := range mf.Mounts {
		p := mt.Prefix
		if err := mountable(p); err != nil {
			return err
		}
		if _, taken := table[p]; taken {
			return fmt.Errorf("mount %q is already held by another extension", p)
		}
	}
	return nil
}

func mountable(p string) error {
	switch {
	case !strings.HasPrefix(p, "/") || len(p) < 2 || strings.Contains(p[1:], "/"):
		return fmt.Errorf("mount %q must be one path segment led by /", p)
	case slices.Contains(reservedPrefixes, p):
		return fmt.Errorf("mount %q is reserved by Streamline", p)
	}
	return nil
}

func grantedViews(e config.ExtensionEntry) []View {
	views := make([]View, 0, len(e.Grants.Views))
	for _, name := range e.Grants.Views {
		v, _ := db.ExtensionViewByName(name)
		views = append(views, View{Name: v.Name, SQL: v.SQL, Tables: v.Tables})
	}
	return views
}

func sameGrants(a, b config.ExtensionGrants) bool {
	return sameSet(a.Mounts, b.Mounts) &&
		sameSet(a.Views, b.Views) &&
		sameSet(a.Services, b.Services) &&
		sameSet(a.Folders, b.Folders)
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// Acquire returns the running extension holding prefix; the caller must call
// Release on it once its request has finished.
func (m *Manager) Acquire(prefix string) (*Running, Mount, bool) {
	e, ok := (*m.table.Load())[prefix]
	if !ok || !e.running.acquire() {
		return nil, Mount{}, false
	}
	return e.running, e.mount, true
}

// AcquireNamed is Acquire for a request addressed to an extension by name
// rather than by mount; the caller must Release.
func (m *Manager) AcquireNamed(name string) (*Running, bool) {
	m.state.RLock()
	r := m.running[name]
	m.state.RUnlock()
	if r == nil || !r.acquire() {
		return nil, false
	}
	return r, true
}

func (m *Manager) Statuses() []Status {
	m.state.RLock()
	defer m.state.RUnlock()
	return slices.Clone(m.statuses)
}

// DataPaths maps each configured extension to where its database file is,
// or will be once it has run.
func (m *Manager) DataPaths() map[string]string {
	m.state.RLock()
	defer m.state.RUnlock()
	return maps.Clone(m.paths)
}

func (m *Manager) Close(ctx context.Context) error {
	m.reconcile.Lock()
	defer m.reconcile.Unlock()

	m.state.Lock()
	old := m.running
	m.running = map[string]*Running{}
	empty := map[string]mountEntry{}
	m.table.Store(&empty)
	m.stopped.Store(&map[string]bool{})
	m.state.Unlock()

	var errs []error
	for _, r := range old {
		r.retire()
	}
	for _, r := range old {
		select {
		case <-r.closed:
			errs = append(errs, r.closeErr)
		case <-ctx.Done():
			errs = append(errs, ctx.Err())
		}
	}
	return errors.Join(errs...)
}

// Purge deletes an extension's own directory (its database). The entry must
// already be reconciled away so the store is closed.
func (m *Manager) Purge(name string) error {
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, "_") {
		return fmt.Errorf("extension name %q cannot name a data directory", name)
	}
	return os.RemoveAll(filepath.Join(m.dataDir, "extensions", name))
}
