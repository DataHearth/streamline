// Package roothealth answers one question for every path that touches library
// files: is this root there at all? os.Stat cannot tell a volume that is not
// mounted from a file that was deleted, so the answer comes from the files the
// database tracks under the root.
package roothealth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/observability"
)

// Reason says what the configured path looks like while its root is offline.
type Reason string

const (
	ReasonPathMissing  Reason = "path_missing"
	ReasonPathEmpty    Reason = "path_empty"
	ReasonFilesMissing Reason = "files_missing"
)

const maxSample = 20

// Status describes an offline root.
type Status struct {
	Root    db.LibraryRoot
	Path    string
	Tracked int
	Reason  Reason
}

// OfflineError refuses an action on an offline root.
type OfflineError struct {
	Status
}

// Error names the root and not its path: this is the message a member sees, and
// library paths are admin-only.
func (e *OfflineError) Error() string {
	return fmt.Sprintf(
		"the %s library is offline: none of its files can be reached",
		e.Root,
	)
}

// Store is what the check reads.
type Store interface {
	CountRootFiles(ctx context.Context, root db.LibraryRoot) (int, error)
	RootFilePathAt(
		ctx context.Context,
		root db.LibraryRoot,
		offset int,
	) (string, error)
	CountLiveRootFiles(ctx context.Context, root db.LibraryRoot) (int, error)
	LiveRootFilePathAt(
		ctx context.Context,
		root db.LibraryRoot,
		offset int,
	) (string, error)
}

type checker struct {
	store   Store
	mu      sync.Mutex
	offline map[db.LibraryRoot]bool
}

var current atomic.Pointer[checker]

// Register installs the store the package reads. Until then, and after
// Register(nil), every root reports online.
func Register(store Store) {
	if store == nil {
		current.Store(nil)
		return
	}
	current.Store(&checker{store: store, offline: map[db.LibraryRoot]bool{}})
}

// Offline returns the status of a root that looks gone, or nil when it is online.
func Offline(ctx context.Context, root db.LibraryRoot) (*Status, error) {
	c := current.Load()
	if c == nil {
		return nil, nil
	}
	status, err := c.check(ctx, root)
	if err != nil {
		return nil, err
	}
	c.logTransition(ctx, root, status)
	return status, nil
}

// AllOffline returns the status of every offline root.
func AllOffline(ctx context.Context) ([]Status, error) {
	var out []Status
	for _, root := range db.LibraryRoots {
		status, err := Offline(ctx, root)
		if err != nil {
			return nil, err
		}
		if status != nil {
			out = append(out, *status)
		}
	}
	return out, nil
}

// Require returns an *OfflineError naming the first offline root of roots.
func Require(ctx context.Context, roots ...db.LibraryRoot) error {
	for _, root := range roots {
		status, err := Offline(ctx, root)
		if err != nil {
			return err
		}
		if status != nil {
			return &OfflineError{Status: *status}
		}
	}
	return nil
}

func (c *checker) check(ctx context.Context, root db.LibraryRoot) (*Status, error) {
	// Rows confirmed missing are left out of the sample, or a root with enough of
	// them would read as an outage. When they are all that is left they still
	// decide: an unmounted volume must not let their wait run out.
	reachable, tracked, err := sample(
		ctx, root, c.store.LiveRootFilePathAt, c.store.CountLiveRootFiles,
	)
	if err == nil && !reachable && tracked == 0 {
		reachable, tracked, err = sample(
			ctx, root, c.store.RootFilePathAt, c.store.CountRootFiles,
		)
	}
	if err != nil {
		return nil, err
	}
	if reachable || tracked == 0 {
		return nil, nil
	}
	path := rootPath(root)
	return &Status{
		Root:    root,
		Path:    path,
		Tracked: tracked,
		Reason:  reasonFor(path),
	}, nil
}

// sample reports whether any sampled file of a root exists and, when none does,
// how many rows were sampled from.
func sample(
	ctx context.Context,
	root db.LibraryRoot,
	pathAt func(context.Context, db.LibraryRoot, int) (string, error),
	countRows func(context.Context, db.LibraryRoot) (int, error),
) (bool, int, error) {
	first, err := pathAt(ctx, root, 0)
	if err != nil {
		return false, 0, err
	}
	if first == "" {
		return false, 0, nil
	}
	if exists(first) {
		return true, 0, nil
	}
	count, err := countRows(ctx, root)
	if err != nil {
		return false, 0, err
	}
	samples := min(count, maxSample)
	for i := 1; i < samples; i++ {
		// Spread over the whole id range, newest row included: a batch of old
		// files deleted by hand is one cluster, and must not read as an outage.
		path, err := pathAt(ctx, root, i*(count-1)/(samples-1))
		if err != nil {
			return false, 0, err
		}
		if path != "" && exists(path) {
			return true, 0, nil
		}
	}
	return false, count, nil
}

func (c *checker) logTransition(
	ctx context.Context,
	root db.LibraryRoot,
	status *Status,
) {
	c.mu.Lock()
	was := c.offline[root]
	c.offline[root] = status != nil
	c.mu.Unlock()

	switch {
	case status != nil && !was:
		//nolint:sloglint // LogAttrs takes slog.Attr by API design
		slog.LogAttrs(
			ctx,
			observability.LevelCritical,
			"library root looks gone; holding its files",
			slog.String("root", string(root)),
			slog.String("path", status.Path),
			slog.Int("tracked", status.Tracked),
			slog.String("reason", string(status.Reason)),
		)
	case status == nil && was:
		slog.InfoContext(ctx, "library root is back", "root", string(root))
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// rootPath reads the configuration on every call, since a path migration moves
// a root while the server runs.
func rootPath(root db.LibraryRoot) string {
	lib := config.Get().Library
	switch root {
	case db.RootMovies:
		return lib.MoviePath
	case db.RootSeries:
		return lib.SeriesPath
	case db.RootMusic:
		return lib.MusicPath
	case db.RootEbooks:
		return lib.EbookPath
	case db.RootAudiobooks:
		return lib.AudiobookPath
	}
	return ""
}

func reasonFor(path string) Reason {
	if _, err := os.Stat(path); err != nil {
		return ReasonPathMissing
	}
	//nolint:gosec // the configured library root
	dir, err := os.Open(path)
	if err != nil {
		return ReasonFilesMissing
	}
	defer dir.Close()
	if _, err := dir.Readdirnames(1); errors.Is(err, io.EOF) {
		return ReasonPathEmpty
	}
	return ReasonFilesMissing
}
