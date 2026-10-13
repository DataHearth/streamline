package download

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/library"
)

// SeedingSet is every torrent the enabled download clients still list: what
// the music tag writer must not write through. A client stopping its seeding
// is visible only for the builtin engine (Torrent.SeedingStopped), so the
// signal every client gives is the listing itself: a torrent still listed may
// still be served, paused or not, since resuming one serves whatever bytes
// are then on disk.
type SeedingSet struct {
	// Complete is false when a client could not be listed: what it holds is
	// unknown, and a caller must assume the worst of anything a torrent could
	// own.
	Complete bool
	roots    []string
	hashes   map[string]struct{}
}

// NewSeedingSet builds a set from content roots and torrent hashes, for the
// callers' tests.
func NewSeedingSet(complete bool, roots, hashes []string) SeedingSet {
	s := SeedingSet{Complete: complete, roots: roots, hashes: map[string]struct{}{}}
	for _, h := range hashes {
		s.hashes[strings.ToLower(h)] = struct{}{}
	}
	return s
}

// Owns reports a path inside the content of a listed torrent: the torrent's
// own file, which a write would change under it.
func (s SeedingSet) Owns(path string) bool {
	for _, r := range s.roots {
		if library.PathUnderRoot(path, r) {
			return true
		}
	}
	return false
}

// Live reports a torrent hash some client still lists.
func (s SeedingSet) Live(hash string) bool {
	_, ok := s.hashes[strings.ToLower(hash)]
	return ok
}

// SeedingTorrents lists every enabled client once. A torrent's content root
// is where the client says it saved it, mapped through download.path_mappings,
// and the convention path <library.download_path>/<name> beside it, since a
// client's own save path is often a view streamline cannot open.
func (d *download) SeedingTorrents(ctx context.Context) SeedingSet {
	ctx, span := tracer.Start(ctx, "download.seeding_torrents")
	defer span.End()

	set := SeedingSet{Complete: true, hashes: map[string]struct{}{}}
	downloadRoot := config.Get().Library.DownloadPath
	for _, dc := range config.EnabledDownloadClients() {
		client, err := d.buildClient(dc)
		if err != nil {
			set.Complete = false
			slog.WarnContext(ctx, "seeding listing: build client failed",
				"client", dc.Name, "error", err)
			continue
		}
		torrents, err := client.ListTorrents(ctx)
		if err != nil {
			set.Complete = false
			slog.WarnContext(ctx, "seeding listing: list torrents failed",
				"client", dc.Name, "error", err)
			continue
		}
		for _, t := range torrents {
			set.hashes[strings.ToLower(t.Hash)] = struct{}{}
			if t.Name == "" || strings.ContainsAny(t.Name, `/\`) {
				continue
			}
			if t.SavePath != "" {
				set.roots = append(set.roots,
					MapClientPath(filepath.Join(t.SavePath, t.Name)))
			}
			if downloadRoot != "" {
				set.roots = append(set.roots, filepath.Join(downloadRoot, t.Name))
			}
		}
	}
	span.SetAttributes(
		attribute.Int("torrents.count", len(set.hashes)),
		attribute.Bool("listing.complete", set.Complete),
	)
	return set
}
