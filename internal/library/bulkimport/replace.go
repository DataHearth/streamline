package bulkimport

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
)

// replacedSuffix names a tracked library file set aside while the scanned
// file replacing it is placed. It is the importer's suffix, so a file a
// crashed commit left behind reads the same whichever path wrote it.
const replacedSuffix = ".streamline-replaced"

// sameFile reports whether two paths name one file on disk. The scanner
// stores symlink-resolved paths while a tracked row keeps the path it was
// imported under, so a library root reached through a symlink gives one file
// two spellings, and a hardlink of a tracked file is that file too. Read as a
// replacement, either would delete the very file being adopted.
func sameFile(a, b string) bool {
	if a == b {
		return true
	}
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// setAside renames each tracked file out of the way beside itself and returns
// the paths it moved. The replacement is placed and recorded before anything
// is deleted, and renaming frees the destination when old and new render to
// the same path. A file already gone is skipped; a failed rename puts back
// what had moved.
func setAside(ctx context.Context, paths ...string) ([]string, error) {
	var moved []string
	for _, p := range paths {
		err := os.Rename(p, p+replacedSuffix)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			putBack(ctx, moved)
			return nil, fmt.Errorf("set aside %s: %w", p, err)
		}
		moved = append(moved, p)
	}
	return moved, nil
}

// putBack restores files setAside moved, after the replacement failed. A path
// the replacement already occupies is left alone: renaming over it would
// destroy the new file, which a move import has no other copy of, so the
// original stays under its set-aside name for a person to recover.
func putBack(ctx context.Context, moved []string) {
	for _, p := range moved {
		if _, err := os.Lstat(p); err == nil {
			slog.ErrorContext(
				ctx,
				"a library file set aside for a replacement stays set aside: its path is taken",
				"path",
				p,
				"set_aside_as",
				p+replacedSuffix,
			)
			continue
		}
		if err := os.Rename(p+replacedSuffix, p); err != nil {
			slog.ErrorContext(ctx,
				"a library file set aside for a replacement could not be put back",
				"path", p, "set_aside_as", p+replacedSuffix, "error", err)
		}
	}
}

// dropAside deletes files setAside moved, once the replacement is recorded.
func dropAside(ctx context.Context, moved []string) {
	for _, p := range moved {
		if err := os.Remove(p + replacedSuffix); err != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			slog.WarnContext(ctx, "replace: remove the replaced file failed",
				"path", p+replacedSuffix, "error", err)
		}
	}
}

// removeReplaced deletes a tracked file a commit replaced where it lay, once
// the replacement's row exists.
func removeReplaced(ctx context.Context, path, newPath string) {
	err := os.Remove(path)
	switch {
	case err == nil:
		// "Where did my old copy go" is asked about exactly this deletion.
		slog.InfoContext(ctx, "bulk import: replaced an existing file",
			"old_path", path, "new_path", newPath)
	case !errors.Is(err, fs.ErrNotExist):
		slog.WarnContext(ctx, "bulk import: remove replaced file failed",
			"path", path, "error", err)
	}
}
