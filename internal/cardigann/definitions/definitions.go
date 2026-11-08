// Package definitions holds the catalog of upstream Cardigann definitions a
// fetch converted (internal/cardigann/snapshot), in memory only — what an
// administrator picks a tracker from — and the versions configured indexers
// are pinned to (an entry names a file and an upstream commit), kept under
// the data directory so a restart runs them without reaching upstream.
package definitions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/convert"
	"github.com/datahearth/streamline/internal/cardigann/snapshot"
)

var (
	// ErrNotFound means the catalog carries no definition under that name.
	ErrNotFound = errors.New("cardigann definition not found")
	// ErrNotFetched means upstream has not been read since the process
	// started.
	ErrNotFetched = errors.New("cardigann definitions not fetched yet")
	// ErrNotKept means a pinned version is neither the catalog's nor kept
	// under the data directory; Resolve fetches it.
	ErrNotKept = errors.New("cardigann definition version not fetched yet")
)

// Set is one fetch of upstream: its catalog and each definition encoded.
type Set struct {
	catalog cardigann.Catalog
	defs    map[string][]byte
}

// NewSet wraps what snapshot.Build returned.
func NewSet(cat cardigann.Catalog, defs map[string][]byte) *Set {
	return &Set{catalog: cat, defs: defs}
}

var current atomic.Pointer[Set]

// Install puts s in force and returns the set it replaced, nil included.
func Install(s *Set) *Set { return current.Swap(s) }

// Current is the set in force, nil before the first fetch.
func Current() *Set { return current.Load() }

// Catalog returns the catalog in force, for the caller to filter or sort.
func Catalog() (cardigann.Catalog, error) {
	s := Current()
	if s == nil {
		return cardigann.Catalog{}, ErrNotFetched
	}
	c := s.catalog
	c.Definitions = slices.Clone(c.Definitions)
	for i := range c.Definitions {
		d := &c.Definitions[i]
		d.Links = slices.Clone(d.Links)
		d.Replaces = slices.Clone(d.Replaces)
		d.Categories = slices.Clone(d.Categories)
		d.MovieSearch = slices.Clone(d.MovieSearch)
		d.TVSearch = slices.Clone(d.TVSearch)
	}
	c.Skipped = slices.Clone(c.Skipped)
	return c, nil
}

// Upstream names the revision the set in force was read from.
func (s *Set) Upstream() cardigann.Upstream { return s.catalog.Upstream }

// Raw is the encoded definition by its file name (Summary.File) — not its
// id, which a few upstream files spell differently.
func (s *Set) Raw(file string) ([]byte, error) {
	b, ok := s.defs[file]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, file)
	}
	return b, nil
}

// Raw is the encoded definition by file name in the set in force.
func Raw(file string) ([]byte, error) {
	s := Current()
	if s == nil {
		return nil, ErrNotFetched
	}
	return s.Raw(file)
}

// Decode reads an encoded definition.
func Decode(b []byte) (*cardigann.Definition, error) {
	var d cardigann.Definition
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("cardigann definition: %w", err)
	}
	return &d, nil
}

var (
	commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	fileRE   = regexp.MustCompile(`^[0-9a-z][0-9a-z._-]*$`)
)

// root holds one directory per upstream commit, under the converter version
// that wrote it: a converter change starts over, re-converting each pinned
// commit, since what the engine reads is the converted form.
func root(dataDir string) string {
	return filepath.Join(
		dataDir, "cardigann", "v"+strconv.Itoa(convert.Version),
	)
}

// kept names file at commit inside root.
func kept(file, commit string) (string, error) {
	if commit == "" {
		return "", errors.New(
			"cardigann.commit is empty: the cardigann-update job sets it",
		)
	}
	if !commitRE.MatchString(commit) {
		return "", fmt.Errorf(
			"cardigann.commit %q is not an upstream commit", commit,
		)
	}
	if !fileRE.MatchString(file) {
		return "", fmt.Errorf("%w: %q", ErrNotFound, file)
	}
	return filepath.Join(commit, file+".json"), nil
}

func readKept(dataDir, file, commit string) ([]byte, error) {
	name, err := kept(file, commit)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(root(dataDir))
	if err == nil {
		defer r.Close()
		var b []byte
		if b, err = r.ReadFile(name); err == nil {
			return b, nil
		}
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s at %.7s", ErrNotKept, file, commit)
	}
	return nil, err
}

// Pinned is file as converted from upstream commit: the catalog's when it
// was read at that commit, else the copy kept under dataDir. It never
// reaches upstream; Resolve does.
func Pinned(dataDir, file, commit string) ([]byte, error) {
	if s := Current(); s != nil && s.Upstream().Commit == commit {
		if b, ok := s.defs[file]; ok {
			return b, nil
		}
	}
	return readKept(dataDir, file, commit)
}

// Keep stores b as file at commit under dataDir.
func Keep(dataDir, file, commit string, b []byte) error {
	name, err := kept(file, commit)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root(dataDir), 0o750); err != nil {
		return err
	}
	r, err := os.OpenRoot(root(dataDir))
	if err != nil {
		return err
	}
	defer r.Close()
	if err := r.MkdirAll(commit, 0o750); err != nil {
		return err
	}
	if err := r.WriteFile(name+".partial", b, 0o600); err != nil {
		return err
	}
	return r.Rename(name+".partial", name)
}

var (
	resolveMu sync.Mutex
	// other is the last commit fetched apart from the catalog, so the
	// entries pinned to it are read from one download.
	other *Set
)

// Resolve is the kept copy of file at commit, writing it from the catalog,
// or fetching that commit from upstream, when it is not kept yet.
func Resolve(
	ctx context.Context,
	client *http.Client,
	dataDir, file, commit string,
) ([]byte, error) {
	b, err := readKept(dataDir, file, commit)
	if !errors.Is(err, ErrNotKept) {
		return b, err
	}
	resolveMu.Lock()
	defer resolveMu.Unlock()
	s := Current()
	if s == nil || s.Upstream().Commit != commit {
		if other == nil || other.Upstream().Commit != commit {
			cat, defs, _, err := snapshot.Fetch(
				ctx, client, cardigann.Upstream{Commit: commit},
			)
			if err != nil {
				return nil, err
			}
			other = NewSet(cat, defs)
		}
		s = other
	}
	if b, err = s.Raw(file); err != nil {
		return nil, err
	}
	return b, Keep(dataDir, file, commit, b)
}

// Prune removes every kept copy outside keep (a set of commits), and those
// an older converter wrote.
func Prune(dataDir string, keep map[string]bool) error {
	dir := filepath.Dir(root(dataDir))
	versions, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	cur := filepath.Base(root(dataDir))
	var errs []error
	for _, v := range versions {
		if v.Name() != cur {
			errs = append(errs, os.RemoveAll(filepath.Join(dir, v.Name())))
			continue
		}
		commits, err := os.ReadDir(filepath.Join(dir, cur))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, c := range commits {
			if !keep[c.Name()] {
				errs = append(errs, os.RemoveAll(filepath.Join(dir, cur, c.Name())))
			}
		}
	}
	return errors.Join(errs...)
}
