package upload

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/datahearth/streamline/internal/library"
)

// Sweep removes every entry under Root() that is neither being received nor
// needed by a record. The order is load-bearing: entries first, the live set
// next, kept last. An upload finishing in between has its record in kept by
// then, and one starting later is not among the entries.
func Sweep(
	ctx context.Context,
	listKept func(context.Context) ([]string, error),
) error {
	root := Root()
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read upload root: %w", err)
	}
	var candidates []string
	for _, e := range entries {
		if !isLive(e.Name()) {
			candidates = append(candidates, e.Name())
		}
	}
	kept, err := listKept(ctx)
	if err != nil {
		return fmt.Errorf("list kept uploads: %w", err)
	}
	for _, name := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		full := filepath.Join(root, name)
		if keeps(kept, full) {
			continue
		}
		if err := os.RemoveAll(full); err != nil {
			slog.WarnContext(ctx, "remove stale upload", "dir", full, "error", err)
			continue
		}
		slog.InfoContext(ctx, "removed stale upload", "dir", full)
	}
	return nil
}

func keeps(kept []string, dir string) bool {
	for _, k := range kept {
		if library.PathUnderRoot(k, dir) {
			return true
		}
	}
	return false
}
