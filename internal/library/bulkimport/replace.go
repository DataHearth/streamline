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

// sameFile reports whether a tracked path and a scanned one name one file on
// disk. The scanner stores symlink-resolved paths while a tracked row keeps
// the path it was imported under, so a library root reached through a symlink
// gives one file two spellings, and a hardlink of a tracked file is that file
// too. Read as a replacement, either would delete the very file being adopted.
//
// "Different" is an answer only a successful comparison gives. A scanned file
// that is gone or unreadable is an error, since replacing a tracked file with
// it would leave the title with none; so is a tracked path that cannot be
// read for any reason but its absence, which leaves nothing to protect.
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

// setAside renames each tracked file out of the way beside itself and returns
// the paths it moved. The replacement is placed and recorded before anything
// is deleted, and renaming frees the destination when old and new render to
// the same path. A file already gone is skipped; a failed rename puts back
// what had moved. A set-aside name already taken is the original of a
// replacement that never completed, possibly the only intact copy, and is
// never renamed over.
func setAside(ctx context.Context, paths ...string) ([]string, error) {
	var moved []string
	for _, p := range paths {
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		err := os.ErrExist
		if _, sErr := os.Lstat(p + replacedSuffix); errors.Is(sErr, fs.ErrNotExist) {
			err = os.Rename(p, p+replacedSuffix)
		}
		if err != nil {
			putBack(ctx, moved, "")
			return nil, fmt.Errorf(
				"set aside %s as %s: %w", p, p+replacedSuffix, err,
			)
		}
		moved = append(moved, p)
	}
	return moved, nil
}

// putBack restores files setAside moved, after the replacement failed.
//
// partialOf names the scanned file when it is its import that failed.
// Whatever then sits at a set-aside file's path is what the failed transfer
// left there, a truncated copy on a full disk for one, so it is removed and
// the original takes its path back, provided the scanned file is still where
// it was and nothing is lost with the removal.
//
// With an empty partialOf the import succeeded and a later step failed. A
// path that is taken then holds the new file, which a move import has no
// other copy of, so the original stays under its set-aside name for a person
// to recover.
func putBack(ctx context.Context, moved []string, partialOf string) {
	for _, p := range moved {
		if _, err := os.Lstat(p); err == nil &&
			!discardPartial(ctx, p, partialOf) {
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

// discardPartial removes what a failed import of scanned left at path and
// reports whether the path is free. It refuses when the scanned file cannot
// be found at its own path: what sits at path may then be all that is left
// of it.
func discardPartial(ctx context.Context, path, scanned string) bool {
	if scanned == "" || scanned == path {
		return false
	}
	if _, err := os.Lstat(scanned); err != nil {
		return false
	}
	if err := os.Remove(path); err != nil {
		slog.WarnContext(ctx, "replace: remove a failed import's leftover failed",
			"path", path, "error", err)
		return false
	}
	return true
}

// dropAside deletes a file setAside moved, once the replacement is recorded
// and the file's own row is gone.
func dropAside(ctx context.Context, path string) {
	if err := os.Remove(path + replacedSuffix); err != nil &&
		!errors.Is(err, fs.ErrNotExist) {
		slog.WarnContext(ctx, "replace: remove the replaced file failed",
			"path", path+replacedSuffix, "error", err)
	}
}

// removeReplaced deletes a tracked file that was replaced without being set
// aside, once the replacement's row exists and its own is gone. It deletes
// only what it can tell apart from the replacement: the replacement must be
// on disk, as another file.
func removeReplaced(ctx context.Context, path, newPath string) {
	if same, err := sameFile(path, newPath); err != nil || same {
		slog.WarnContext(ctx, "bulk import: replaced file left in place",
			"path", path, "new_path", newPath, "error", err)
		return
	}
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
