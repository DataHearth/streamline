package server

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/datahearth/streamline/internal/bittorrent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/observability"
)

// prewarmStreams is how many ranges of a file are read at once: storage
// reached over a WAN serves a single stream at a fraction of its throughput.
const prewarmStreams = 8

const prewarmChunk = 1 << 20

// prewarmLimit bounds the read well inside the chart's default startup
// allowance (5 min). Whatever was read by then stays cached.
const prewarmLimit = 2 * time.Minute

// bootFiles lists the files boot reads at random: the database and its WAL,
// and the builtin engine's piece completion store.
func bootFiles(dbPath string) []string {
	if p, err := filepath.EvalSymlinks(dbPath); err == nil {
		dbPath = p // SQLite keeps the WAL next to the link's target
	}
	files := []string{dbPath + "-wal", dbPath}
	if entry, ok := config.BuiltinDownloadClient(); ok {
		files = append(files, bittorrent.PieceCompletionPath(entry.DownloadDir))
	}
	return files
}

// prewarm reads files into the page cache and returns how many bytes it
// read. Boot reads them a page at a time, each a storage round trip on a
// cold cache, while large sequential reads pay one per readahead window.
// Read errors are left to the reads that follow.
//
// It must return before the files are opened: closing a descriptor on a
// file drops every POSIX lock the process holds on it, SQLite's included.
func prewarm(ctx context.Context, files []string) int64 {
	sizes := map[string]int64{}
	var size int64
	for _, p := range files {
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			continue
		}
		sizes[p] = info.Size()
		size += info.Size()
	}
	if size == 0 {
		return 0
	}

	slog.InfoContext(ctx, "reading boot files into the page cache",
		"bytes", size, "files", slices.Sorted(maps.Keys(sizes)))
	start := time.Now()
	readCtx, cancel := context.WithTimeout(ctx, prewarmLimit)
	defer cancel()
	// The files can sit on different storage (the database on a block
	// volume, the piece completion with the downloads), so they are read at
	// once.
	var read atomic.Int64
	var wg sync.WaitGroup
	for p, n := range sizes {
		wg.Go(func() {
			defer observability.RecoverPanic(ctx, "boot file prewarm", nil)
			read.Add(readIntoCache(readCtx, p, n))
		})
	}
	wg.Wait()
	slog.InfoContext(ctx, "boot files read into the page cache",
		"bytes", read.Load(),
		"of_bytes", size,
		"duration_ms", time.Since(start).Milliseconds(),
		"timed_out", errors.Is(readCtx.Err(), context.DeadlineExceeded))
	return read.Load()
}

func readIntoCache(ctx context.Context, path string, size int64) int64 {
	span := (size + prewarmStreams - 1) / prewarmStreams
	var read atomic.Int64
	var wg sync.WaitGroup
	for off := int64(0); off < size; off += span {
		wg.Go(func() {
			defer observability.RecoverPanic(ctx, "boot file prewarm", nil)
			read.Add(readRange(ctx, path, off, min(span, size-off)))
		})
	}
	wg.Wait()
	return read.Load()
}

// readRange reads n bytes of path from off through its own descriptor, so
// each range keeps its own readahead window.
func readRange(ctx context.Context, path string, off, n int64) int64 {
	if ctx.Err() != nil {
		return 0
	}
	f, err := os.Open(path) //nolint:gosec // a boot file under the configured dirs
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, min(prewarmChunk, n))
	var done int64
	for done < n && ctx.Err() == nil {
		m, err := f.ReadAt(buf[:min(int64(len(buf)), n-done)], off+done)
		done += int64(m)
		if err != nil {
			break
		}
	}
	return done
}
