package bittorrent

import (
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/anacrolix/torrent/segments"
)

// uncachedPiece reads one piece straight from its files and drops what it
// read from the page cache.
//
// anacrolix reads pieces through mmap by default and keeps every mapping
// until the storage closes, and the kernel reclaims mapped pages after
// unmapped ones and never drops them on request. Seeding a few hundred
// torrents therefore filled the pod's memory cgroup with torrent data that
// outlived the database's pages: with seeding paused on the homelab, the
// database volume's reads fell from 65-88 KB/s to near zero. The upload
// itself is read once per peer request, so caching it buys little.
//
// Its contract is upstream's (storage/file-torrent-io.go): a missing or short
// file reads as io.EOF, which is what marks the piece incomplete, and a file
// still downloading is read from its ".part" name first.
type uncachedPiece struct {
	files          []string
	segs           segments.Index
	offset, length int64
}

func (p uncachedPiece) ReadAt(b []byte, off int64) (int, error) {
	if off >= p.length {
		return 0, io.EOF
	}
	var end error
	if rem := p.length - off; int64(len(b)) > rem {
		b, end = b[:rem], io.EOF
	}
	n := 0
	for i, e := range p.segs.LocateIter(
		segments.Extent{Start: p.offset + off, Length: int64(len(b))},
	) {
		n1, err := readUncached(p.files[i], b[:e.Length], e.Start)
		n += n1
		b = b[n1:]
		if err != nil {
			return n, err
		}
	}
	return n, end
}

// WriteTo reads in 1 MiB steps: every read reopens its files with readahead
// off, so io.Copy's 32 KiB buffer would be 128 round trips to CephFS for a
// 4 MiB piece.
func (p uncachedPiece) WriteTo(w io.Writer) (int64, error) {
	n, err := io.CopyBuffer(
		w, io.NewSectionReader(p, 0, p.length), make([]byte, 1<<20),
	)
	if err == nil && n < p.length {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

// path is a pieceFiles entry: the layout the file storage already opened, its
// torrent-supplied parts held inside the download dir by contentPaths.
func readUncached(path string, b []byte, off int64) (int, error) {
	f, err := os.Open(path + ".part") //nolint:gosec // see the doc comment
	if errors.Is(err, fs.ErrNotExist) {
		f, err = os.Open(path) //nolint:gosec // see the doc comment
	}
	if errors.Is(err, fs.ErrNotExist) {
		return 0, io.EOF
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if err := noReadahead(f); err != nil {
		return 0, err
	}
	n, err := f.ReadAt(b, off)
	return n, errors.Join(err, dropCached(f, off, int64(n)))
}
