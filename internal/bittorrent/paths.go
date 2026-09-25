package bittorrent

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// contentPaths decides where anacrolix's file storage puts each torrent. The
// layout is anacrolix's default — <download_dir>/<info name>[/<file path>],
// the location contract adoption and the importer derive a record's save path
// from — but the info name and file paths come from whoever authored the
// torrent, and the default trusts them. An empty name places a single-file
// torrent at the download dir itself, ".streamline-session" lands on the
// engine's own state, and another live torrent's name lands on its data; in
// each case anacrolix renames what is there out of the way on open, and
// remove-with-files later deletes it.
//
// Storage offers no hook that can refuse a torrent, so a refusal is made the
// way anacrolix already refuses a file escaping its torrent's directory: the
// file path returned points outside it, and OpenTorrent fails with "not sub
// path". torrentDir decides (it alone sees the infohash); filePath carries
// the verdict out, keyed on the *metainfo.Info pointer OpenTorrent hands both.
//
// Data on disk that no live torrent owns — one removed without its files — is
// told apart from a torrent resuming its own by size: a torrent the engine
// does not already hold may only open where each of its files is absent or
// already the length it declares, with no ".part" leftover beside it. The
// torrents restore re-adds are trusted outright, since the partial data there
// is theirs. A different release of identical sizes still passes; anacrolix
// then re-verifies it piece by piece.
type contentPaths struct {
	mu      sync.Mutex
	owners  map[string]metainfo.Hash
	refused map[*metainfo.Info]bool
	trusted map[metainfo.Hash]bool
}

// newContentStorage is the engine's file storage, placed by a fresh
// contentPaths.
func newContentStorage(
	dir string,
	pc storage.PieceCompletion,
) (*drainingStorage, *contentPaths) {
	paths := newContentPaths()
	return newDrainingStorage(storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   dir,
		PieceCompletion: pc,
		TorrentDirMaker: paths.torrentDir,
		FilePathMaker:   paths.filePath,
	})), paths
}

func newContentPaths() *contentPaths {
	return &contentPaths{
		owners:  map[string]metainfo.Hash{},
		refused: map[*metainfo.Info]bool{},
		trusted: map[metainfo.Hash]bool{},
	}
}

// trust marks ih as the owner of whatever its name holds on disk.
func (p *contentPaths) trust(ih metainfo.Hash) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.trusted[ih] = true
}

// admits reports whether a torrent may be placed, without claiming its name.
func (p *contentPaths) admits(
	base string,
	info *metainfo.Info,
	ih metainfo.Hash,
) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.admitsLocked(base, info, ih)
}

func (p *contentPaths) admitsLocked(
	base string,
	info *metainfo.Info,
	ih metainfo.Hash,
) bool {
	name := info.BestName()
	if !safeContentName(name) {
		return false
	}
	if owner, taken := p.owners[name]; taken {
		return owner == ih
	}
	return p.trusted[ih] || fitsOnDisk(base, info)
}

func (p *contentPaths) torrentDir(
	base string,
	info *metainfo.Info,
	ih metainfo.Hash,
) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.admitsLocked(base, info, ih) {
		p.refused[info] = true
		return base
	}
	p.owners[info.BestName()] = ih
	return base
}

// fitsOnDisk reports whether nothing on disk under the torrent's name would
// be overwritten or renamed away by it.
func fitsOnDisk(base string, info *metainfo.Info) bool {
	for _, fi := range info.UpvertedFiles() {
		path := filepath.Join(append(
			[]string{base, info.BestName()}, fi.BestPath()...,
		)...)
		if st, err := os.Stat(path); err == nil && st.Size() != fi.Length {
			return false
		}
		if _, err := os.Stat(path + ".part"); err == nil {
			return false
		}
	}
	return true
}

func (p *contentPaths) filePath(opts storage.FilePathMakerOpts) string {
	name := opts.Info.BestName()
	rel := filepath.Join(append([]string{name}, opts.File.BestPath()...)...)
	p.mu.Lock()
	refused := p.refused[opts.Info]
	delete(p.refused, opts.Info)
	p.mu.Unlock()
	// Join has already resolved any ".." in the file path, so one that walked
	// out of the torrent's own directory no longer starts with its name.
	if refused ||
		(rel != name && !strings.HasPrefix(rel, name+string(filepath.Separator))) {
		return ".."
	}
	return rel
}

// release frees every name ih holds, once the torrent is gone.
func (p *contentPaths) release(ih metainfo.Hash) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, owner := range p.owners {
		if owner == ih {
			delete(p.owners, name)
		}
	}
	delete(p.trusted, ih)
}

func safeContentName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		name != sessionDirName && !strings.ContainsAny(name, `/\`)
}
