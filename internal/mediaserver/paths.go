package mediaserver

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/datahearth/streamline/internal/config"
)

// scanFolder turns one path a caller changed into the folder a media server
// should rescan for it: a file's own folder, a folder itself, and for a path
// that is gone (a deleted file, a title deleted with its folder) the nearest
// folder above it that still exists — the servers find a removal by rescanning
// its parent, and a partial scan of a missing folder is ignored. It never
// climbs out of root: a path that reaches it, or lies outside it, is the whole
// library, reported as "".
func scanFolder(root, p string) string {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) || !localPathUnder(p, root) {
		return ""
	}
	for p != root {
		if fi, err := os.Stat(p); err == nil {
			if !fi.IsDir() {
				p = filepath.Dir(p)
			}
			if p == root {
				return ""
			}
			return p
		}
		p = filepath.Dir(p)
	}
	return ""
}

// scanFolders folds the paths a caller changed into the folders to rescan,
// sorted, with every folder that another one in the set already contains
// dropped. whole reports that one of them resolved to the library itself
// (or that none was given), in which case the folders are moot.
func scanFolders(root string, paths []string) (folders []string, whole bool) {
	if len(paths) == 0 {
		return nil, true
	}
	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		f := scanFolder(root, p)
		if f == "" {
			return nil, true
		}
		set[f] = struct{}{}
	}
	return pruneNested(set), false
}

// pruneNested returns the folders of set, sorted, without any that lies under
// another one of them: a rescan of the season folder already covers the
// episode's, and of the artist folder every album's.
func pruneNested(set map[string]struct{}) []string {
	all := make([]string, 0, len(set))
	for f := range set {
		all = append(all, f)
	}
	slices.Sort(all)
	out := all[:0]
	for _, f := range all {
		if len(out) > 0 && localPathUnder(f, out[len(out)-1]) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// mapServerPath rewrites a local path into the server's view through the
// entry's path_mappings: the first mapping whose from contains it wins, and
// an unmatched path is returned unchanged (both processes see the same paths
// when none is configured). A Windows to is joined with backslashes.
func mapServerPath(mappings []config.MediaServerPathMapping, p string) string {
	if p == "" {
		return p
	}
	for _, m := range mappings {
		if m.From == "" || m.To == "" || !localPathUnder(p, m.From) {
			continue
		}
		rel, err := filepath.Rel(filepath.Clean(m.From), filepath.Clean(p))
		if err != nil {
			continue
		}
		return joinServerPath(m.To, filepath.ToSlash(rel))
	}
	return p
}

// joinServerPath appends a slash-separated relative path to a server-side
// prefix, in the prefix's own separator.
func joinServerPath(prefix, rel string) string {
	if rel == "." || rel == "" {
		return prefix
	}
	if isWindowsPath(prefix) {
		return strings.TrimRight(prefix, `\/`) + `\` +
			strings.ReplaceAll(rel, "/", `\`)
	}
	return path.Join(prefix, rel)
}

// isWindowsPath reports a drive-rooted or UNC path.
func isWindowsPath(p string) bool {
	if strings.HasPrefix(p, `\\`) {
		return true
	}
	return len(p) >= 2 && p[1] == ':' &&
		(p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z')
}

// serverPathUnder reports whether p lies at or under dir, both as a media
// server reports them. Windows paths compare case-insensitively with either
// separator, since that is how the server's own filesystem compares them.
func serverPathUnder(p, dir string) bool {
	if p == "" || dir == "" {
		return false
	}
	if isWindowsPath(dir) || isWindowsPath(p) {
		p = strings.ToLower(strings.ReplaceAll(p, `\`, "/"))
		dir = strings.ToLower(strings.ReplaceAll(dir, `\`, "/"))
	}
	p = strings.TrimRight(p, "/")
	dir = strings.TrimRight(dir, "/")
	return p == dir || strings.HasPrefix(p, dir+"/")
}

// longestContaining returns the index of the location in locs that contains p
// most narrowly, or -1 when none does.
func longestContaining(p string, locs []string) int {
	best, bestLen := -1, -1
	for i, l := range locs {
		if serverPathUnder(p, l) && len(l) > bestLen {
			best, bestLen = i, len(l)
		}
	}
	return best
}

// localPathUnder reports whether p lies at or under dir, both clean absolute
// paths on this side.
func localPathUnder(p, dir string) bool {
	p, dir = filepath.Clean(p), filepath.Clean(dir)
	if p == dir {
		return true
	}
	if !strings.HasSuffix(dir, string(filepath.Separator)) {
		dir += string(filepath.Separator)
	}
	return strings.HasPrefix(p, dir)
}
