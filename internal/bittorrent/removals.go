package bittorrent

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/datahearth/streamline/internal/db"
)

// PendingRemoval is a torrent the engine dropped whose files are still on
// disk: the last attempt to delete them failed, and the next one is due on the
// enforcer's tick.
type PendingRemoval struct {
	Hash  string
	Name  string
	Path  string
	Error string
	Since time.Time
}

type owedRemoval struct {
	name    string
	dir     string
	info    *metainfo.Info
	since   time.Time
	lastErr string
}

// dataRemovals deletes the files of torrents removed with their data, and
// keeps trying when a delete fails. Each one is a row before its torrent is
// dropped, so neither a failing volume nor a restart leaves files behind that
// nothing remembers: once the torrent is gone from the engine, no listing
// names them any more.
type dataRemovals struct {
	store db.Store
	paths *contentPaths

	// attempts serialises deletes: the removal's own first try and the tick's
	// retry must not walk one folder at once.
	attempts sync.Mutex

	mu   sync.Mutex
	owed map[string]*owedRemoval
}

func newDataRemovals(store db.Store, paths *contentPaths) *dataRemovals {
	return &dataRemovals{
		store: store,
		paths: paths,
		owed:  map[string]*owedRemoval{},
	}
}

// load picks up what an earlier process still owed. A row whose info no
// longer parses names no files and is dropped.
func (r *dataRemovals) load(ctx context.Context) error {
	rows, err := r.store.ListTorrentDataRemovals(ctx)
	if err != nil {
		return fmt.Errorf("list torrent data removals: %w", err)
	}
	for _, row := range rows {
		var info metainfo.Info
		if err := bencode.Unmarshal(row.Info, &info); err != nil {
			slog.WarnContext(ctx,
				"dropping a pending torrent data removal that no longer parses",
				"info_hash", row.InfoHash, "torrent", row.Name, "error", err)
			if err := r.store.DeleteTorrentDataRemoval(
				ctx, row.InfoHash,
			); err != nil {
				slog.WarnContext(ctx,
					"could not drop the unreadable torrent data removal",
					"info_hash", row.InfoHash, "error", err)
			}
			continue
		}
		r.mu.Lock()
		r.owed[row.InfoHash] = &owedRemoval{
			name: row.Name, dir: row.Dir, info: &info, since: row.CreateTime,
		}
		r.mu.Unlock()
	}
	return nil
}

// owe records that hash's files under dir are to be deleted.
func (r *dataRemovals) owe(
	ctx context.Context,
	hash, dir string,
	info *metainfo.Info,
) error {
	raw, err := bencode.Marshal(info)
	if err != nil {
		return fmt.Errorf("encode torrent info: %w", err)
	}
	name := info.BestName()
	if err := r.store.KeepTorrentDataRemoval(ctx, db.TorrentDataRemovalParams{
		InfoHash: hash, Name: name, Dir: dir, Info: raw,
	}); err != nil {
		return err
	}
	r.mu.Lock()
	r.owed[hash] = &owedRemoval{
		name: name, dir: dir, info: info, since: time.Now(),
	}
	r.mu.Unlock()
	return nil
}

// attempt deletes hash's owed files. A failure is kept with its reason for the
// next pass; a name a torrent has taken since means the files are that
// torrent's now, and nothing is owed.
func (r *dataRemovals) attempt(ctx context.Context, hash string) {
	r.attempts.Lock()
	defer r.attempts.Unlock()

	r.mu.Lock()
	o, ok := r.owed[hash]
	r.mu.Unlock()
	if !ok {
		return
	}
	if r.paths.hold(o.name) {
		err := removeTorrentData(o.dir, o.info)
		r.paths.unhold(o.name)
		if err != nil {
			r.mu.Lock()
			o.lastErr = err.Error()
			r.mu.Unlock()
			slog.WarnContext(ctx,
				"a removed torrent's files could not be deleted, will retry",
				"info_hash", hash, "torrent", o.name,
				"path", filepath.Join(o.dir, o.name), "error", err)
			return
		}
	}
	if err := r.store.DeleteTorrentDataRemoval(ctx, hash); err != nil {
		slog.WarnContext(ctx,
			"could not clear a finished torrent data removal",
			"info_hash", hash, "error", err)
		return
	}
	r.mu.Lock()
	delete(r.owed, hash)
	r.mu.Unlock()
}

func (r *dataRemovals) retryAll(ctx context.Context) {
	r.mu.Lock()
	hashes := make([]string, 0, len(r.owed))
	for hash := range r.owed {
		hashes = append(hashes, hash)
	}
	r.mu.Unlock()
	for _, hash := range hashes {
		r.attempt(ctx, hash)
	}
}

func (r *dataRemovals) views() []PendingRemoval {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]PendingRemoval, 0, len(r.owed))
	for hash, o := range r.owed {
		out = append(out, PendingRemoval{
			Hash:  hash,
			Name:  o.name,
			Path:  filepath.Join(o.dir, o.name),
			Error: o.lastErr,
			Since: o.since,
		})
	}
	slices.SortFunc(out, func(a, b PendingRemoval) int {
		return cmp.Or(a.Since.Compare(b.Since), cmp.Compare(a.Hash, b.Hash))
	})
	return out
}
