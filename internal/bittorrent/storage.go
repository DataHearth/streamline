package bittorrent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

var (
	_ storage.ClientImplCloser = (*drainingStorage)(nil)
	// A refactor that breaks the WriterTo promotion would silently demote
	// every piece hash to the generic copy fallback (see guard).
	_ io.WriterTo = writerToPiece{}
)

var errStorageClosed = errors.New("piece storage is closed")

// drainingStorage holds piece completion marks and storage shutdown apart.
//
// anacrolix v1.61.0 marks a hashed piece complete/not-complete with the client
// lock released (torrent.go:2645-2651 and :2706-2712) and the per-torrent
// storage lock already given up — finishHash drops it at torrent.go:2862,
// before calling pieceHashed at :2886 — from piece-hasher goroutines
// (torrent.go:2789) that Client.Close never awaits: its closeGroup covers only
// the per-torrent storage close (torrent.go:1113-1126). Closing the bolt
// completion store as soon as Client.Close returns therefore pulls the database
// out from under a mark that is still running, bolt fails it with "database not
// open", and the piece loses its persisted completion — so the next boot
// re-hashes and re-downloads it, contradicting this package's contract.
//
// Engine.Close is what actually fences the hasher marks, by waiting out
// PieceState.Marking (see waitPieceMarks). This type covers what that flag
// cannot see: storage.Piece.ReadAt marks a piece not-complete on a short read
// (storage/wrappers.go:119-127) from goroutines with no marking flag raised. It
// also keeps the two shutdown steps from overlapping at all, since holding the
// read lock for a whole mark makes Close's write lock a drain.
//
// One mutation bypasses both layers: filePieceImpl.markIncompletePieces
// (storage/file-piece.go:156-167) writes pieceCompletion().Set directly —
// never through MarkNotComplete — when Completion's file-size check finds a
// constituent file missing or short (file-piece.go:81-90, :107-149). That only
// fires when data has genuinely vanished from disk, where re-hashing is the
// right outcome anyway, so it is left unguarded.
//
// The lock has to span the whole mark rather than each store access: one
// MarkComplete is a Set followed by a full completion scan of the file's pieces
// (storage/file-piece.go:169-196), and a guard on the individual Get/Set would
// still let the store close in the gap between them.
type drainingStorage struct {
	inner storage.ClientImplCloser

	mu     sync.RWMutex
	closed bool
}

func newDrainingStorage(inner storage.ClientImplCloser) *drainingStorage {
	return &drainingStorage{inner: inner}
}

func (d *drainingStorage) OpenTorrent(
	ctx context.Context,
	info *metainfo.Info,
	infoHash metainfo.Hash,
) (storage.TorrentImpl, error) {
	impl, err := d.inner.OpenTorrent(ctx, info, infoHash)
	if err != nil {
		return impl, err
	}
	// File storage leaves PieceWithHash nil (storage/file-client.go:121-124)
	// and anacrolix then routes every piece through Piece
	// (storage/wrappers.go:43-48), so guarding Piece covers all of them.
	piece := impl.Piece
	impl.Piece = func(p metainfo.Piece) storage.PieceImpl {
		return d.guard(piece(p), infoHash, p.Index())
	}
	return impl, nil
}

func (d *drainingStorage) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return d.inner.Close()
}

func (d *drainingStorage) guard(
	inner storage.PieceImpl,
	infoHash metainfo.Hash,
	index int,
) storage.PieceImpl {
	p := guardedPiece{PieceImpl: inner, store: d, infoHash: infoHash, index: index}
	// Hashing a piece goes through the io.WriterTo fast path when the piece
	// offers one (torrent.go:1358 into storage/wrappers.go:60-77), and file
	// storage does (storage/file-piece.go:28-32). A wrapper that dropped it
	// would push every hash onto the generic copy fallback.
	if wt, ok := inner.(io.WriterTo); ok {
		return writerToPiece{guardedPiece: p, WriterTo: wt}
	}
	return p
}

// mark runs one completion mark under the drain lock, refusing it once the
// storage is closed.
func (d *drainingStorage) mark(
	infoHash metainfo.Hash, index int, complete bool, f func() error,
) error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.closed {
		// A refused mark is lost completion state — the piece gets re-hashed or
		// re-downloaded on the next boot — and this line is the only evidence an
		// operator ever gets, so it must not be swallowed.
		slog.WarnContext(context.Background(),
			"piece completion mark arrived after the storage closed",
			"info_hash", infoHash.HexString(), "piece", index,
			"complete", complete)
		return errStorageClosed
	}
	return f()
}

type guardedPiece struct {
	storage.PieceImpl
	store    *drainingStorage
	infoHash metainfo.Hash
	index    int
}

func (p guardedPiece) MarkComplete() error {
	return p.store.mark(p.infoHash, p.index, true, p.PieceImpl.MarkComplete)
}

func (p guardedPiece) MarkNotComplete() error {
	return p.store.mark(p.infoHash, p.index, false, p.PieceImpl.MarkNotComplete)
}

type writerToPiece struct {
	guardedPiece
	io.WriterTo
}
