package bittorrent

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"go.etcd.io/bbolt"
)

var (
	_ storage.ClientImplCloser = (*drainingStorage)(nil)
	// Without WriteTo every piece hash falls back to anacrolix's generic copy
	// through its mmap reader, the page-cache path uncachedPiece exists to avoid.
	_ io.WriterTo = guardedPiece{}
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
	inner      storage.ClientImplCloser
	completion storage.PieceCompletion
	base       string

	mu     sync.RWMutex
	closed bool
}

func newDrainingStorage(
	inner storage.ClientImplCloser,
	completion storage.PieceCompletion,
	base string,
) *drainingStorage {
	return &drainingStorage{inner: inner, completion: completion, base: base}
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
	files := pieceFiles(d.base, info)
	if err := d.recheckFinishedFilePieces(infoHash, info, files); err != nil {
		return impl, errors.Join(err, impl.Close())
	}
	segs := info.FileSegmentsIndex()
	piece := impl.Piece
	impl.Piece = func(p metainfo.Piece) storage.PieceImpl {
		return guardedPiece{
			PieceImpl: piece(p),
			read: uncachedPiece{
				files: files, segs: segs, offset: p.Offset(), length: p.Length(),
			},
			store:    d,
			infoHash: infoHash,
			index:    p.Index(),
		}
	}
	return impl, nil
}

// recheckFinishedFilePieces sets every piece stored not-complete that touches a
// finished file back to unknown, so anacrolix hashes it instead of
// downloading it again.
//
// A download into such a piece destroys the finished file. anacrolix v1.62.0
// writes a piece to its files' ".part" names, creating them if absent, so the
// finished file gets a new, empty ".part"; when the piece completes,
// promotePartFile renames that sparse file over the finished one
// (exclRenameIfExists is a plain os.Rename), leaving it zeros but for the
// piece. setCompletionFromPartFiles does not prevent this for a piece the
// finished file shares with an unfinished one, and a killed process leaves
// exactly that piece stored not-complete: the completion store writes a
// piece's false at once but buffers its true until a checkpoint, and the
// piece that finishes a file is usually the one it shares with the next.
//
// Hashing it passes on the data the two files already hold. If it fails,
// MarkNotComplete renames the finished file back to ".part" before anything
// is written to it.
func (d *drainingStorage) recheckFinishedFilePieces(
	infoHash metainfo.Hash,
	info *metainfo.Info,
	files []string,
) error {
	for i, fi := range info.UpvertedFiles() {
		if fi.Length == 0 {
			continue
		}
		if _, err := os.Stat(files[i]); err != nil {
			continue
		}
		for p := fi.BeginPieceIndex(info.PieceLength); p < fi.EndPieceIndex(info.PieceLength); p++ {
			key := metainfo.PieceKey{InfoHash: infoHash, Index: p}
			c, err := d.completion.Get(key)
			if err != nil {
				return fmt.Errorf("piece %d completion: %w", p, err)
			}
			if c.Ok && !c.Complete {
				if err := d.completion.Set(key, g.None[bool]()); err != nil {
					return fmt.Errorf("piece %d completion: %w", p, err)
				}
			}
		}
	}
	return nil
}

func (d *drainingStorage) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return d.inner.Close()
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

// settledCompletion keeps a piece the store already holds complete from being
// written to it again.
//
// Opening a torrent marks every piece of its finished files complete
// (anacrolix's setCompletionFromPartFiles), and anacrolix v1.62.0's bolt store
// buffers each such mark as a change to persist on Close, held or not. With
// hundreds of seeding torrents that is every piece in the store, rewritten in
// one bolt transaction at shutdown: longer than the pod's termination grace,
// so the process was killed before the completions that did change were saved.
//
// It wraps the store NewBoltPieceCompletion returns, which is a
// PieceCompletionCheckpointer and PieceCompletionPersistenter; anacrolix
// type-asserts both, so they are passed through.
type settledCompletion struct{ storage.PieceCompletion }

func (c settledCompletion) Set(
	key metainfo.PieceKey,
	complete g.Option[bool],
) error {
	if complete.Ok && complete.Value {
		if held, err := c.Get(key); err == nil && held.Ok && held.Complete {
			return nil
		}
	}
	return c.PieceCompletion.Set(key, complete)
}

func (c settledCompletion) Checkpoint(keys []metainfo.PieceKey) error {
	return c.PieceCompletion.(storage.PieceCompletionCheckpointer).Checkpoint(keys)
}

func (c settledCompletion) Persistent() bool {
	return c.PieceCompletion.(storage.PieceCompletionPersistenter).Persistent()
}

// pruneCompletion deletes what the bolt completion store at path holds for
// torrents outside live, a set of hex info hashes. anacrolix never forgets a
// removed torrent's pieces, so the store only grows: homelab's held 917
// torrents for 492 sessions. It must run before NewBoltPieceCompletion opens
// the file, which bolt locks.
func pruneCompletion(path string, live map[string]bool) error {
	db, err := bbolt.Open(path, 0o660, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		return err
	}
	err = db.Update(func(tx *bbolt.Tx) error {
		// anacrolix's bucket (storage/bolt-piece-completion.go), one nested
		// bucket per info hash.
		completion := tx.Bucket([]byte("completion"))
		if completion == nil {
			return nil
		}
		var stale [][]byte
		err := completion.ForEach(func(k, v []byte) error {
			if v == nil && !live[hex.EncodeToString(k)] {
				stale = append(stale, k)
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, k := range stale {
			if err := completion.DeleteBucket(k); err != nil {
				return err
			}
		}
		return nil
	})
	return errors.Join(err, db.Close())
}

type guardedPiece struct {
	storage.PieceImpl
	read     uncachedPiece
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

func (p guardedPiece) ReadAt(b []byte, off int64) (int, error) {
	return p.read.ReadAt(b, off)
}

func (p guardedPiece) WriteTo(w io.Writer) (int64, error) {
	return p.read.WriteTo(w)
}
