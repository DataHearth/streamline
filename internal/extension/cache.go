package extension

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// SweepCaches keeps every extension's cache folder under CacheBytes by
// deleting the files written longest ago.
func (m *Manager) SweepCaches(ctx context.Context) error {
	dirs, err := filepath.Glob(filepath.Join(m.dataDir, "extensions", "*", "cache"))
	if err != nil {
		return err
	}
	var errs []error
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		if err := sweepDir(ctx, dir, CacheBytes); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type cacheFile struct {
	path    string
	size    int64
	modTime time.Time
}

func sweepDir(ctx context.Context, dir string, limit int64) error {
	var (
		files []cacheFile
		total int64
	)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, cacheFile{p, info.Size(), info.ModTime()})
		total += info.Size()
		return nil
	})
	if err != nil || total <= limit {
		return err
	}

	slices.SortFunc(files, func(a, b cacheFile) int {
		return a.modTime.Compare(b.modTime)
	})
	for _, f := range files {
		if total <= limit {
			break
		}
		if err := os.Remove(f.path); err != nil {
			slog.DebugContext(ctx, "Extension cache file not removed",
				"path", f.path, "error", err)
			continue
		}
		total -= f.size
	}
	return nil
}
