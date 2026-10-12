// Package definitions serves the Cardigann definitions streamline runs: the
// snapshot embedded at build time by `task cardigann:sync`, superseded by a
// newer one the runtime update wrote to the data directory, overridden file
// by file by the operator's own YAML. Nothing under this directory but this
// file is hand-written: data/, index.json and NOTICE are regenerated
// wholesale.
//
// Under a data directory's cardigann dir:
//
//	current/  the runtime update's snapshot (data/, index.json, NOTICE)
//	pending/  updates held for approval (internal/cardigann/update)
//	custom/   <file>.yml, converted on load, winning over both snapshots
package definitions

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/convert"
)

// ErrNotFound means no snapshot carries a definition under that name.
var ErrNotFound = errors.New("cardigann definition not found")

//go:embed index.json NOTICE data
var files embed.FS

var embedded = sync.OnceValues(func() (cardigann.Catalog, error) {
	var c cardigann.Catalog
	b, err := files.ReadFile("index.json")
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("embedded cardigann index: %w", err)
	}
	return c, nil
})

// runtimeSets caches, per directory, the runtime snapshot's index when it is
// the one in force; Invalidate drops it after the update rewrites it.
var runtimeSets sync.Map // dir → *cardigann.Catalog (nil: not in force)

// Invalidate forgets what was read of dir's runtime snapshot.
func Invalidate(dir string) { runtimeSets.Delete(dir) }

// Current is where the runtime update's snapshot lives under dir.
func Current(dir string) string { return filepath.Join(dir, "current") }

// Custom is where an operator's own definitions live under dir.
func Custom(dir string) string { return filepath.Join(dir, "custom") }

// runtime returns dir's runtime snapshot index when it is in force: newer
// upstream than the embedded one, written by this converter. A binary
// upgraded past it, or onto a new converter, falls back to its own embedded
// copy until the next update rewrites the runtime one.
func runtime(dir string) (*cardigann.Catalog, error) {
	if dir == "" {
		return nil, nil
	}
	if c, ok := runtimeSets.Load(dir); ok {
		return c.(*cardigann.Catalog), nil
	}
	b, err := os.ReadFile(filepath.Join(Current(dir), "index.json"))
	if errors.Is(err, fs.ErrNotExist) {
		runtimeSets.Store(dir, (*cardigann.Catalog)(nil))
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rt cardigann.Catalog
	if err := json.Unmarshal(b, &rt); err != nil {
		return nil, fmt.Errorf("cardigann runtime index: %w", err)
	}
	emb, err := embedded()
	if err != nil {
		return nil, err
	}
	var inForce *cardigann.Catalog
	if rt.Converter == convert.Version && rt.Schema == cardigann.SchemaVersion &&
		newer(rt.Upstream.Date, emb.Upstream.Date) {
		inForce = &rt
	}
	runtimeSets.Store(dir, inForce)
	return inForce, nil
}

func newer(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339, a)
	tb, errB := time.Parse(time.RFC3339, b)
	return errA == nil && errB == nil && ta.After(tb)
}

// Catalog returns the index of the definitions in force under dir — the
// runtime snapshot's or the embedded one, with the custom ones folded in —
// or the embedded one alone when dir is empty. The result is the caller's
// to filter, sort or edit.
func Catalog(dir string) (cardigann.Catalog, error) {
	c, err := Snapshot(dir)
	if err != nil || dir == "" {
		return c, err
	}
	entries, err := os.ReadDir(Custom(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	for _, e := range entries {
		name := e.Name()
		file, ok := strings.CutSuffix(name, ".yml")
		if !ok || e.IsDir() || !validFile(file) {
			continue
		}
		d, err := loadCustom(dir, file)
		if err != nil {
			c.Skipped = append(c.Skipped, cardigann.Skipped{
				File: "custom/" + name, Reason: err.Error(),
			})
			continue
		}
		s := cardigann.Summarize(file, d)
		s.Custom = true
		if i := slices.IndexFunc(c.Definitions, func(x cardigann.Summary) bool {
			return x.File == file
		}); i >= 0 {
			c.Definitions[i] = s
		} else {
			c.Definitions = append(c.Definitions, s)
		}
	}
	slices.SortFunc(c.Definitions, func(a, b cardigann.Summary) int {
		return strings.Compare(a.File, b.File)
	})
	return c, nil
}

// Snapshot is Catalog without the custom definitions: the upstream set in
// force, which a runtime update compares itself against.
func Snapshot(dir string) (cardigann.Catalog, error) {
	rt, err := runtime(dir)
	if err != nil {
		return cardigann.Catalog{}, err
	}
	var c cardigann.Catalog
	if rt != nil {
		c = *rt
	} else if c, err = embedded(); err != nil {
		return c, err
	}
	c.Definitions = slices.Clone(c.Definitions)
	for i := range c.Definitions {
		s := &c.Definitions[i]
		s.Links = slices.Clone(s.Links)
		s.Replaces = slices.Clone(s.Replaces)
		s.Categories = slices.Clone(s.Categories)
		s.MovieSearch = slices.Clone(s.MovieSearch)
		s.TVSearch = slices.Clone(s.TVSearch)
	}
	c.Skipped = slices.Clone(c.Skipped)
	c.Held = slices.Clone(c.Held)
	return c, nil
}

func validFile(file string) bool {
	return file != "" && !strings.ContainsAny(file, `/\`) && fs.ValidPath(file)
}

// Load decodes the definition in force under dir by its file name
// (Summary.File) — not its id, which a few upstream files spell differently:
// the custom one, else the runtime snapshot's, else the embedded one. An
// empty dir reads the embedded snapshot alone.
func Load(dir, file string) (*cardigann.Definition, error) {
	if !validFile(file) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, file)
	}
	if dir != "" {
		d, err := loadCustom(dir, file)
		if !errors.Is(err, fs.ErrNotExist) {
			return d, err
		}
	}
	b, err := Raw(dir, file)
	if err != nil {
		return nil, err
	}
	var d cardigann.Definition
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("cardigann definition %q: %w", file, err)
	}
	return &d, nil
}

func loadCustom(dir, file string) (*cardigann.Definition, error) {
	path := filepath.Join(Custom(dir), file+".yml")
	b, err := os.ReadFile(path) //nolint:gosec // G304: validFile admits no separator
	if err != nil {
		return nil, err
	}
	d, err := convert.Convert("custom/"+file+".yml", b)
	if err != nil {
		return nil, fmt.Errorf("custom definition %s: %w", path, err)
	}
	return d, nil
}

// Raw is the encoded definition of the upstream snapshot in force under
// dir, custom definitions aside.
func Raw(dir, file string) ([]byte, error) {
	if !validFile(file) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, file)
	}
	rt, err := runtime(dir)
	if err != nil {
		return nil, err
	}
	var b []byte
	if rt != nil {
		//nolint:gosec // G304: validFile admits no separator
		b, err = os.ReadFile(filepath.Join(Current(dir), "data", file+".json"))
	} else {
		b, err = files.ReadFile("data/" + file + ".json")
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, file)
	}
	return b, err
}

// Notice is the attribution and licence notice shipped with the embedded
// snapshot. Anything that distributes or displays the definitions carries
// it.
func Notice() string {
	b, _ := files.ReadFile("NOTICE")
	return string(b)
}
