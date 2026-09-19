package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// RemoveMediaFile deletes a media file together with its sidecars — the
// non-media files beside it that share its basename, such as `<stem>.nfo`,
// `<stem>.en.srt` or `<stem>-thumb.jpg` — then prunes the directories the
// deletion left empty, stopping below root so a library root survives even
// when it holds nothing else.
//
// Takes a context solely so the deletion is auditable. It removes files and
// walks directories away irreversibly, and callers only ever logged the error
// path — so the normal case, the one that actually deleted things, left no
// record of which sidecars went with the file or how far up the prune walked.
//
// Returns an error wrapping fs.ErrNotExist when path itself was not there to
// delete. Sidecars and the prune still run in that case; the error is how a
// caller that promised the operator a file would go can tell that it did not,
// instead of reporting a deletion that never happened.
func RemoveMediaFile(ctx context.Context, path, root string) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))

	// The media file itself goes by path, not by a name matched against the
	// directory listing. A row whose path had drifted from disk matched no
	// entry, fell through every `continue`, and returned success having deleted
	// nothing — which is how "delete with files" kept a 2.7 GB file and still
	// reported the title removed. ENOENT is held back rather than returned
	// here so the sidecars and the prune still run.
	missing := false
	if err := os.Remove(path); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		missing = true
	}

	var sidecars int
	entries, err := os.ReadDir(dir)
	switch {
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("read %s: %w", dir, err)
	case err == nil:
		for _, e := range entries {
			if e.IsDir() || !isSidecar(e.Name(), stem) {
				continue
			}
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil &&
				!errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("remove %s: %w", e.Name(), err)
			}
			sidecars++
		}
	}
	pruned := PruneEmptyDirs(ctx, dir, root)
	slog.InfoContext(ctx, "media file removed",
		"file.path", path,
		"file_was_missing", missing,
		"sidecars_removed", sidecars,
		"dirs_pruned", pruned)
	if missing {
		return fmt.Errorf("media file %s: %w", path, fs.ErrNotExist)
	}
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

// externalMetadata names the media-server metadata files that sit beside a
// library file without carrying its basename. Plex, Jellyfin and the *arr stack
// write them; streamline never does. isSidecar keys on the media file's stem,
// so none of these is a sidecar, and a directory holding one survived every
// prune — on a library migrated from the *arr stack that is nearly every
// directory (629 of 1003 on the homelab), so deleting a title left its folder
// behind forever. Matched case-insensitively against the whole basename.
var externalMetadata = map[string]bool{
	"movie.nfo": true, "tvshow.nfo": true, "season.nfo": true,
	"poster.jpg": true, "poster.png": true,
	"fanart.jpg": true, "fanart.png": true,
	"banner.jpg": true, "banner.png": true,
	"thumb.jpg": true, "thumb.png": true,
	"landscape.jpg": true, "landscape.png": true,
	"backdrop.jpg": true, "folder.jpg": true, "cover.jpg": true,
	"clearlogo.png": true, "clearart.png": true, "logo.png": true,
	"disc.png": true, "discart.png": true,
	".ds_store": true, "thumbs.db": true,
}

// PruneEmptyDirs walks up from dir removing each directory until one refuses.
// A non-empty directory is exactly what should stop the walk, and os.Remove
// reporting ENOTEMPTY is how that shows up — the error is the exit condition,
// not a failure. root is never removed, nor is anything outside it.
//
// A directory holding nothing but externalMetadata counts as empty: its
// contents are descriptions of media that is gone. Anything else in it — a
// second episode, a stray archive, a subdirectory — stops the walk, so the
// season folder of a show that still has episodes keeps its artwork.
//
// Returns how many directories it removed, and names each one: the walk can
// climb several levels, and a root computed slightly wrong takes real
// directories with it. Without the trail there was nothing afterwards to say
// which ones went.
func PruneEmptyDirs(ctx context.Context, dir, root string) int {
	root = filepath.Clean(root)
	prefix := root + string(filepath.Separator)
	var pruned int
	for dir = filepath.Clean(dir); strings.HasPrefix(dir, prefix); dir = filepath.Dir(dir) {
		if !dropMetadataOnly(ctx, dir) {
			return pruned
		}
		if err := os.Remove(dir); err != nil {
			return pruned
		}
		pruned++
		slog.InfoContext(ctx, "pruned empty library directory", "dir.path", dir)
	}
	return pruned
}

// dropMetadataOnly empties dir when every entry left in it is externalMetadata,
// and reports whether dir is now empty. A directory that already is empty, or
// that holds anything worth keeping, is left exactly as it was.
func dropMetadataOnly(ctx context.Context, dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return err == nil
	}
	for _, e := range entries {
		if e.IsDir() || !externalMetadata[strings.ToLower(e.Name())] {
			return false
		}
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if err := os.Remove(path); err != nil {
			return false
		}
		slog.InfoContext(ctx, "removed orphaned media-server metadata",
			"file.path", path)
	}
	return true
}
