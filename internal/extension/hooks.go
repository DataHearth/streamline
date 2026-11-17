package extension

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/datahearth/streamline/internal/observability"
)

// HookLibrary is the hook a manifest lists to be told when a movie, a series
// or an episode changed.
const HookLibrary = "library"

var knownHooks = []string{HookLibrary}

const (
	hookLibraryPath = "/hooks/library"
	// libraryWindow is how long changes are collected before one call carries
	// them: an import, a season pack or the daily metadata refresh touch many
	// rows one after the other.
	libraryWindow = 5 * time.Second
	// maxHookIDs is where a kind's ids give way to a comparison of the whole
	// library, which costs the guest less than a list that long.
	maxHookIDs = 500
)

// LibraryChange names what changed. An id may be of a row that no longer
// exists: the guest reads its views and drops what it kept for it.
type LibraryChange struct {
	Full     []string `json:"full,omitempty"`
	Movies   []uint32 `json:"movies,omitempty"`
	Series   []uint32 `json:"series,omitempty"`
	Episodes []uint32 `json:"episodes,omitempty"`
}

func (c LibraryChange) empty() bool {
	return len(c.Full) == 0 && len(c.Movies) == 0 && len(c.Series) == 0 &&
		len(c.Episodes) == 0
}

// libraryHooks collects changes over a window and hands them to every running
// extension that listed the hook.
type libraryHooks struct {
	window time.Duration

	mu      sync.Mutex
	pending LibraryChange
	timer   *time.Timer
}

// LibraryChanged tells the extensions that listed the library hook what
// changed. It returns at once: the call is made after the window, off the
// caller's path, and a guest that fails is logged and not retried, since its
// own scan catches up.
func (m *Manager) LibraryChanged(c LibraryChange) {
	if c.empty() {
		return
	}
	h := &m.library
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending = mergeChanges(h.pending, c)
	if h.window <= 0 {
		pending := h.pending
		h.pending = LibraryChange{}
		m.sendLibraryChange(context.Background(), pending)
		return
	}
	if h.timer == nil {
		h.timer = time.AfterFunc(h.window, func() {
			ctx := context.Background()
			defer observability.RecoverPanic(ctx, "extension.library_hook", nil)
			m.FlushLibraryChanges(ctx)
		})
	}
}

// FlushLibraryChanges sends what is collected now.
func (m *Manager) FlushLibraryChanges(ctx context.Context) {
	h := &m.library
	h.mu.Lock()
	if h.timer != nil {
		h.timer.Stop()
		h.timer = nil
	}
	pending := h.pending
	h.pending = LibraryChange{}
	h.mu.Unlock()
	m.sendLibraryChange(ctx, pending)
}

func mergeChanges(a, b LibraryChange) LibraryChange {
	union := func(x, y []uint32) []uint32 {
		return slices.Compact(slices.Sorted(slices.Values(append(x, y...))))
	}
	out := LibraryChange{
		Full: slices.Compact(
			slices.Sorted(slices.Values(append(a.Full, b.Full...))),
		),
		Movies:   union(a.Movies, b.Movies),
		Series:   union(a.Series, b.Series),
		Episodes: union(a.Episodes, b.Episodes),
	}
	full := func(folder string) {
		if !slices.Contains(out.Full, folder) {
			out.Full = append(out.Full, folder)
			slices.Sort(out.Full)
		}
	}
	if len(out.Movies) > maxHookIDs {
		out.Movies = nil
		full(folderMovies)
	}
	if len(out.Series)+len(out.Episodes) > maxHookIDs {
		out.Series, out.Episodes = nil, nil
		full(folderSeries)
	}
	return out
}

func (m *Manager) sendLibraryChange(ctx context.Context, c LibraryChange) {
	if c.empty() {
		return
	}
	body, err := json.Marshal(c)
	if err != nil {
		return
	}
	m.state.RLock()
	var names []string
	for name, r := range m.running {
		if slices.Contains(r.module.Manifest().Hooks, HookLibrary) {
			names = append(names, name)
		}
	}
	m.state.RUnlock()
	for _, name := range names {
		resp, running, err := m.Call(ctx, name, Request{
			Method: http.MethodPost,
			Path:   hookLibraryPath,
			Body:   body,
		})
		switch {
		case err != nil:
			slog.WarnContext(ctx, "Extension library hook failed",
				"extension", name, "error", err)
		case running && resp.Status >= http.StatusBadRequest:
			slog.WarnContext(ctx, "Extension refused the library hook",
				"extension", name, "http.response.status_code", resp.Status)
		}
	}
}
