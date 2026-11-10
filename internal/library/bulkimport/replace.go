package bulkimport

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/datahearth/streamline/internal/library"
)

// A commit that replaces a title's tracked file removes the old file only once
// the new one is placed and its row written. These helpers cover the one case
// where the old file is in the way: a rename commit whose replacement renders
// to the very path the old file occupies, which the importer refuses as
// ErrDestExists.

// setAside renames oldPath to a hidden sibling so a replacement can be placed
// there, returning where it went ("" when nothing moved: the file is gone, or
// it is src's own inode, which the importer treats as already placed).
func setAside(src, oldPath string) (string, error) {
	oldInfo, err := os.Stat(oldPath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if srcInfo, err := os.Stat(src); err == nil && os.SameFile(oldInfo, srcInfo) {
		return "", nil
	}
	aside := filepath.Join(filepath.Dir(oldPath),
		".streamline-replacing-"+filepath.Base(oldPath))
	if err := os.Rename(oldPath, aside); err != nil {
		return "", err
	}
	return aside, nil
}

// restoreAside puts a set-aside old file back at its tracked path.
func restoreAside(ctx context.Context, aside, path string) {
	if aside == "" {
		return
	}
	if err := os.Rename(aside, path); err != nil {
		slog.ErrorContext(ctx, "bulk import: restore replaced file failed",
			"path", path, "aside", aside, "error", err)
	}
}

// undoPlacement takes a file a rename commit placed back out of the library:
// a move goes back to its source, a copy or hardlink is removed.
func undoPlacement(ctx context.Context, src, placed, mode string) {
	var err error
	if mode == "move" {
		err = library.MoveFile(ctx, placed, src)
	} else {
		err = os.Remove(placed)
	}
	if err != nil && !os.IsNotExist(err) {
		slog.WarnContext(ctx, "bulk import: undo placement failed",
			"path", placed, "source", src, "error", err)
	}
}

func samePath(a, b string) bool { return filepath.Clean(a) == filepath.Clean(b) }

// sameFile reports whether a tracked path and a scanned one name one file on
// disk. The scanner stores symlink-resolved paths while a tracked row keeps
// the path it was imported under, so a library root reached through a symlink
// gives one file two spellings, and a hardlink of a tracked file is that file
// too. Read as a replacement, either would have the commit remove the very
// file it is adopting.
//
// "Different" is an answer only a successful comparison gives. A scanned file
// that is gone or is no regular file is an error: taken for a replacement, it
// would cost the title the file it has and leave a row pointing at nothing.
// So is a tracked path that cannot be read for any reason but its absence,
// which leaves nothing to protect.
func sameFile(tracked, scanned string) (bool, error) {
	if tracked == scanned {
		return true, nil
	}
	si, err := os.Stat(scanned)
	if err != nil {
		return false, err
	}
	if !si.Mode().IsRegular() {
		return false, fmt.Errorf("%s is not a regular file", scanned)
	}
	ti, err := os.Stat(tracked)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(ti, si), nil
}
