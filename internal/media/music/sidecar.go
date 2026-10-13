package music

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/audiotags"
)

// artistCoverNames are the artist-folder image names players read, in the
// order the writer looks for one it did not write.
var artistCoverNames = []string{"folder", "artist"}

// sidecar is one image file the tag writer keeps beside the library's files:
// the album's cover.jpg, the artist's folder.jpg.
type sidecar struct {
	dir string
	// names are the base names (any of folderCoverExts) a player reads
	// there; base is the one the writer creates.
	names []string
	base  string
	// ours is the hash of the file the writer left last, empty when none.
	ours string
	// data is what should be there, nil when nothing should (the setting
	// is off, or there is no picture).
	data []byte
}

// place makes the sidecar hold data and reports the hash now recorded and
// whether anything on disk changed. A file under one of the names whose
// content is not the one the writer left is someone else's: it is never
// replaced or removed, and nothing is written beside it.
func (sc sidecar) place(seeding download.SeedingSet) (string, bool, error) {
	existing, err := findImage(sc.dir, sc.names)
	if err != nil {
		return sc.ours, false, err
	}
	if existing != "" {
		//nolint:gosec // a cover file in the library's own album folder
		cur, err := os.ReadFile(existing)
		if err != nil {
			return sc.ours, false, err
		}
		if sc.ours == "" || audiotags.ArtHash(cur) != sc.ours {
			return sc.ours, false, nil
		}
	}

	ext := imageExt(sc.data)
	if sc.data == nil || ext == "" {
		if existing == "" {
			return "", false, nil
		}
		if err := os.Remove(
			existing,
		); err != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			return sc.ours, false, err
		}
		return "", true, nil
	}
	target := filepath.Join(sc.dir, sc.base+ext)
	want := audiotags.ArtHash(sc.data)
	if existing == target && want == sc.ours {
		return sc.ours, false, nil
	}
	if seeding.Owns(target) || (!seeding.Complete &&
		library.PathUnderRoot(target, config.Get().Library.DownloadPath)) {
		return sc.ours, false, nil
	}
	if err := writeFileAtomic(target, sc.data); err != nil {
		return sc.ours, false, err
	}
	if existing != "" && existing != target {
		_ = os.Remove(existing)
	}
	return want, true, nil
}

// sidecars keeps the album's cover file and, once per pass, its artist's
// folder image. It reports whether anything on disk changed.
func (a *albumTagging) sidecars(
	ctx context.Context,
	artistsDone map[uint32]bool,
) bool {
	root := config.Get().Library.MusicPath
	dir := albumDir(a.alb)
	if dir == "" || !strictlyUnder(dir, root) {
		return false
	}
	art := a.artwork(ctx)
	var data []byte
	if a.cfg.SidecarArt {
		data = art.cover
	}
	changed := false
	hash, did, err := sidecar{
		dir: dir, names: folderCoverNames, base: "cover",
		ours: a.alb.ArtSidecarHash, data: data,
	}.place(a.seeding)
	if err != nil {
		slog.WarnContext(ctx, "album cover file not written",
			"album.id", a.alb.ID, "dir", dir, "error", err)
	}
	if hash != a.alb.ArtSidecarHash {
		if err := a.svc.db.SetAlbumArtSidecar(ctx, a.alb.ID, hash); err != nil {
			slog.WarnContext(ctx, "recording the album cover file failed",
				"album.id", a.alb.ID, "error", err)
		}
		a.alb.ArtSidecarHash = hash
	}
	changed = changed || did

	ar := a.alb.Edges.Artist
	if ar == nil || artistsDone[ar.ID] {
		return changed
	}
	artistsDone[ar.ID] = true
	parent := filepath.Dir(dir)
	// Only a folder named after the artist, as {Artist}/ renders it, is
	// the artist's: a template writing "{Artist} - {Album}" puts albums
	// straight in the root, and a "Compilations" folder belongs to no one.
	if !strictlyUnder(parent, root) ||
		!strings.EqualFold(filepath.Base(parent), library.SanitizePath(ar.Name)) {
		return changed
	}
	var photo []byte
	if a.cfg.SidecarArt && a.svc.posters != nil {
		photo, _ = os.ReadFile(a.svc.posters.Path(artistKind, ar.ID))
	}
	hash, did, err = sidecar{
		dir: parent, names: artistCoverNames, base: "folder",
		ours: ar.ArtSidecarHash, data: nonEmpty(photo),
	}.place(a.seeding)
	if err != nil {
		slog.WarnContext(ctx, "artist folder image not written",
			"artist.id", ar.ID, "dir", parent, "error", err)
	}
	if hash != ar.ArtSidecarHash {
		if err := a.svc.db.SetArtistArtSidecar(ctx, ar.ID, hash); err != nil {
			slog.WarnContext(ctx, "recording the artist folder image failed",
				"artist.id", ar.ID, "error", err)
		}
		ar.ArtSidecarHash = hash
	}
	return changed || did
}

// albumDir is the folder holding the album's files: their common directory,
// so an album split into disc folders gets one cover above them.
func albumDir(alb *ent.Album) string {
	dir := ""
	for _, t := range alb.Edges.Tracks {
		for _, mf := range t.Edges.MediaFiles {
			d := filepath.Dir(mf.Path)
			if dir == "" {
				dir = d
				continue
			}
			for !library.PathUnderRoot(d, dir) {
				next := filepath.Dir(dir)
				if next == dir {
					return ""
				}
				dir = next
			}
		}
	}
	return dir
}

// strictlyUnder reports path inside root and not root itself: nothing is
// written in the music root.
func strictlyUnder(path, root string) bool {
	return root != "" && library.PathUnderRoot(path, root) &&
		filepath.Clean(path) != filepath.Clean(root)
}

// findImage finds name.jpg|jpeg|png in dir, any case, in the order of names.
func findImage(dir string, names []string) (string, error) {
	all, err := findImages(dir, names)
	if err != nil || len(all) == 0 {
		return "", err
	}
	return all[0], nil
}

// findImages is every name.jpg|jpeg|png in dir, any case, in the order of
// names then extensions.
func findImages(dir string, names []string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, name := range names {
		for _, ext := range folderCoverExts {
			for _, e := range entries {
				if !e.IsDir() && strings.EqualFold(e.Name(), name+ext) {
					found = append(found, filepath.Join(dir, e.Name()))
				}
			}
		}
	}
	return found, nil
}

// imageExt is the extension a sidecar of data is named with, empty for
// anything but a JPEG or PNG.
func imageExt(data []byte) string {
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	}
	return ""
}

func nonEmpty(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return b
}

// writeFileAtomic writes data to path through a hidden file beside it, so a
// player never reads half an image.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".streamline-art-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	err = errors.Join(err, tmp.Chmod(0o644), tmp.Close())
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}
