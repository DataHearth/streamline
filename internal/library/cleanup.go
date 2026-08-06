package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RemoveMediaFile deletes a media file together with its sidecars — the
// non-media files beside it that share its basename, such as `<stem>.nfo`,
// `<stem>.en.srt` or `<stem>-thumb.jpg` — then prunes the directories the
// deletion left empty, stopping below root so a library root survives even
// when it holds nothing else.
func RemoveMediaFile(path, root string) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name != base && !isSidecar(name, stem) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil &&
			!os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", name, err)
		}
	}
	pruneEmptyDirs(dir, root)
	return nil
}

// isSidecar reports whether name is companion metadata for stem: the same
// basename plus a suffix opening a new segment. Media files are excluded no
// matter how the names line up — "S01E01 - Title" is a prefix of
// "S01E01 - Title - Part 2.mkv", and deleting one episode must never take
// another episode's video with it.
func isSidecar(name, stem string) bool {
	if MediaExts[filepath.Ext(name)] {
		return false
	}
	rest, ok := strings.CutPrefix(name, stem)
	if !ok || rest == "" {
		return false
	}
	return rest[0] == '.' || rest[0] == '-'
}

// pruneEmptyDirs walks up from dir removing each directory until one refuses.
// A non-empty directory is exactly what should stop the walk, and os.Remove
// reporting ENOTEMPTY is how that shows up — the error is the exit condition,
// not a failure.
func pruneEmptyDirs(dir, root string) {
	root = filepath.Clean(root)
	prefix := root + string(filepath.Separator)
	for dir = filepath.Clean(dir); strings.HasPrefix(dir, prefix); dir = filepath.Dir(dir) {
		if err := os.Remove(dir); err != nil {
			return
		}
	}
}
