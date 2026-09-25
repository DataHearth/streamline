package bittorrent

import (
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
// ponytail: only live torrents are known. A name matching data on disk that
// no live torrent owns (a finished torrent removed without its files) is not
// refused, since a re-add of that same torrent resuming its data looks alike.
type contentPaths struct {
	mu      sync.Mutex
	owners  map[string]metainfo.Hash
	refused map[*metainfo.Info]bool
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
	}
}

func (p *contentPaths) torrentDir(
	base string,
	info *metainfo.Info,
	ih metainfo.Hash,
) string {
	name := info.BestName()
	p.mu.Lock()
	defer p.mu.Unlock()
	if owner, taken := p.owners[name]; !safeContentName(name) ||
		(taken && owner != ih) {
		p.refused[info] = true
		return base
	}
	p.owners[name] = ih
	return base
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
}

func safeContentName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		name != sessionDirName && !strings.ContainsAny(name, `/\`)
}
