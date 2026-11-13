package extension

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/datahearth/streamline/internal/config"
	"github.com/tetratelabs/wazero"
	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
	"github.com/tetratelabs/wazero/experimental/sysfs"
	"github.com/tetratelabs/wazero/sys"
)

const guestFoldersRoot = "/folders/"

func (m *Manager) cacheDir(name string) string {
	return filepath.Join(m.dataDir, "extensions", name, "cache")
}

// folders resolves the granted folders for one request, so a changed
// library.music_path reaches the next request without a restart. A folder
// whose directory is not there is left out: the extension still answers.
func (m *Manager) folders(ctx context.Context, run *Running) []Folder {
	var out []Folder
	for _, name := range knownFolders {
		if !slices.Contains(run.grants.Folders, name) {
			continue
		}
		host := m.folderHost(run.name, name)
		if name == folderCache {
			//nolint:gosec // host is the extension's own directory under data_dir
			if err := os.MkdirAll(host, 0o750); err != nil {
				slog.WarnContext(ctx, "Extension folder is unavailable",
					"extension", run.name, "folder", name, "error", err)
				continue
			}
		} else if st, err := os.Stat(host); err != nil || !st.IsDir() { //nolint:gosec // host comes from the operator's config or data_dir
			slog.WarnContext(ctx, "Extension folder is unavailable",
				"extension", run.name, "folder", name, "path", host)
			continue
		}
		out = append(out, Folder{
			Name:      name,
			HostPath:  host,
			GuestPath: guestFoldersRoot + name,
		})
	}
	return out
}

func (m *Manager) folderHost(extension, folder string) string {
	switch folder {
	case folderArtwork:
		return filepath.Join(m.dataDir, "posters")
	case folderCache:
		return m.cacheDir(extension)
	}
	cfg := config.Get()
	if cfg == nil {
		return ""
	}
	switch folder {
	case folderMusic:
		return cfg.Library.MusicPath
	case folderMovies:
		return cfg.Library.MoviePath
	default:
		return cfg.Library.SeriesPath
	}
}

// mountConfig mounts each folder into a guest. Read-only folders go through
// os.Root, which refuses a path that resolves outside the directory, links
// included; the cache is writable and goes through guardFS.
func mountConfig(folders []Folder) (wazero.FSConfig, func(), error) {
	cfg := wazero.NewFSConfig()
	var roots []*os.Root
	closeAll := func() {
		for _, r := range roots {
			if err := r.Close(); err != nil {
				slog.DebugContext(
					context.Background(),
					"Extension folder root not closed",
					"error", err,
				)
			}
		}
	}
	for _, f := range folders {
		root, err := os.OpenRoot(f.HostPath)
		if err != nil {
			closeAll()
			return nil, nil, fmt.Errorf("open folder %s: %w", f.Name, err)
		}
		roots = append(roots, root)
		if f.Name == folderCache {
			cfg = cfg.(sysfs.FSConfig).WithSysFSMount(
				guardFS{FS: sysfs.DirFS(f.HostPath), root: root},
				f.GuestPath,
			)
			continue
		}
		cfg = cfg.WithFSMount(root.FS(), f.GuestPath)
	}
	return cfg, closeAll, nil
}

// guardFS is the writable cache mount. A plain directory mount follows a
// link the host or a guest left there out of the directory; guardFS refuses
// any path whose resolution leaves it, and refuses to create links.
type guardFS struct {
	experimentalsys.FS
	root *os.Root
}

func (g guardFS) inside(p string) bool {
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" {
		return true
	}
	_, err := g.root.Stat(p)
	if err == nil {
		return true
	}
	if !os.IsNotExist(err) {
		return false
	}
	// Not there yet: the name itself must not be a dangling link and the
	// parent must resolve inside.
	if _, err := g.root.Lstat(p); err == nil {
		return false
	}
	return g.inside(path.Dir(p))
}

func (g guardFS) OpenFile(
	p string,
	flag experimentalsys.Oflag,
	perm fs.FileMode,
) (experimentalsys.File, experimentalsys.Errno) {
	if !g.inside(p) {
		return nil, experimentalsys.EACCES
	}
	return g.FS.OpenFile(p, flag, perm)
}

func (g guardFS) Stat(p string) (sys.Stat_t, experimentalsys.Errno) {
	if !g.inside(p) {
		return sys.Stat_t{}, experimentalsys.EACCES
	}
	return g.FS.Stat(p)
}

func (g guardFS) Lstat(p string) (sys.Stat_t, experimentalsys.Errno) {
	if !g.inside(p) {
		return sys.Stat_t{}, experimentalsys.EACCES
	}
	return g.FS.Lstat(p)
}

func (g guardFS) Mkdir(p string, perm fs.FileMode) experimentalsys.Errno {
	if !g.inside(p) {
		return experimentalsys.EACCES
	}
	return g.FS.Mkdir(p, perm)
}

func (g guardFS) Chmod(p string, perm fs.FileMode) experimentalsys.Errno {
	if !g.inside(p) {
		return experimentalsys.EACCES
	}
	return g.FS.Chmod(p, perm)
}

func (g guardFS) Rename(from, to string) experimentalsys.Errno {
	if !g.inside(from) || !g.inside(to) {
		return experimentalsys.EACCES
	}
	return g.FS.Rename(from, to)
}

func (g guardFS) Rmdir(p string) experimentalsys.Errno {
	if !g.inside(p) {
		return experimentalsys.EACCES
	}
	return g.FS.Rmdir(p)
}

func (g guardFS) Unlink(p string) experimentalsys.Errno {
	if !g.inside(p) {
		return experimentalsys.EACCES
	}
	return g.FS.Unlink(p)
}

func (g guardFS) Readlink(p string) (string, experimentalsys.Errno) {
	if !g.inside(p) {
		return "", experimentalsys.EACCES
	}
	return g.FS.Readlink(p)
}

func (g guardFS) Utimens(p string, atim, mtim int64) experimentalsys.Errno {
	if !g.inside(p) {
		return experimentalsys.EACCES
	}
	return g.FS.Utimens(p, atim, mtim)
}

func (guardFS) Symlink(string, string) experimentalsys.Errno {
	return experimentalsys.EPERM
}

func (guardFS) Link(string, string) experimentalsys.Errno {
	return experimentalsys.EPERM
}

// resolveInFolder finds the granted folder holding hostPath and opens it as a
// root, with the path inside it. The caller closes the root. A path outside
// every folder (a dot segment resolved first) is not ok.
func resolveInFolder(
	folders []Folder,
	hostPath string,
) (*os.Root, string, bool) {
	if !filepath.IsAbs(hostPath) {
		return nil, "", false
	}
	hostPath = filepath.Clean(hostPath)
	for _, f := range folders {
		inside, err := filepath.Rel(filepath.Clean(f.HostPath), hostPath)
		if err != nil || inside == "." || !filepath.IsLocal(inside) {
			continue
		}
		r, err := os.OpenRoot(f.HostPath)
		if err != nil {
			return nil, "", false
		}
		return r, inside, true
	}
	return nil, "", false
}
