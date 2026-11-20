package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
)

// rotationVictims picks at most n torrents to remove, among those importedAt
// lists: the ones imported on the earliest UTC day first, and among those of
// one day the highest ratio first, since they have seeded longest and of
// their day have given back the most.
func rotationVictims(
	torrents []Torrent,
	importedAt map[string]time.Time,
	n int,
) []Torrent {
	day := func(t Torrent) time.Time {
		return importedAt[t.Hash].UTC().Truncate(24 * time.Hour)
	}
	candidates := slices.DeleteFunc(slices.Clone(torrents), func(t Torrent) bool {
		_, ok := importedAt[t.Hash]
		return !ok
	})
	slices.SortStableFunc(candidates, func(a, b Torrent) int {
		if c := day(a).Compare(day(b)); c != 0 {
			return c
		}
		switch {
		case a.Ratio > b.Ratio:
			return -1
		case a.Ratio < b.Ratio:
			return 1
		}
		return 0
	})
	return candidates[:max(0, min(n, len(candidates)))]
}

// overLimit is how many torrents the client holds past its own limit, 0 for a
// client at or under it and for one that has none.
func overLimit(client Client, held int) int {
	limiter, ok := client.(TorrentLimiter)
	if !ok {
		return 0
	}
	return max(0, held-limiter.TorrentLimit())
}

// addRotating adds src, and when the builtin engine refuses it for being full
// removes one imported torrent with its files and tries once more. Nothing
// evictable, an eviction that fails, or an engine holding more than its limit
// leaves the refusal as it was: one removal frees no slot there, and bringing
// the engine back under a lowered max_torrents is TrimOverLimitTorrents' pass.
//
// The eviction and the second add share rotateMu: two grabs at the cap would
// otherwise pick the same victim, and only one of them would get its slot.
func (d *download) addRotating(
	ctx context.Context,
	dc config.DownloadClientEntry,
	client Client,
	src TorrentSource,
) (string, error) {
	hash, err := client.AddTorrent(ctx, src)
	if !errors.Is(err, ErrClientFull) || dc.ClientType != "builtin" {
		return hash, err
	}
	d.rotateMu.Lock()
	defer d.rotateMu.Unlock()
	if !d.rotateOut(ctx, dc.Name, client) {
		return "", err
	}
	return client.AddTorrent(ctx, src)
}

// rotateOut removes the builtin engine's longest-seeded imported torrent and
// reports whether a slot was freed.
func (d *download) rotateOut(
	ctx context.Context,
	clientName string,
	client Client,
) bool {
	ctx, span := tracer.Start(ctx, "download.rotate_out")
	defer span.End()

	fail := func(msg string, err error, attrs ...any) bool {
		slog.WarnContext(ctx, msg,
			append(attrs, "error", otelx.RecordSpanError(span, err))...)
		return false
	}

	torrents, err := client.ListTorrents(ctx)
	if err != nil {
		return fail("listing the full engine's torrents failed", err)
	}
	if over := overLimit(client, len(torrents)); over > 0 {
		span.SetAttributes(attribute.Int("rotation.over_limit", over))
		return false
	}
	hashes := make([]string, len(torrents))
	for i, t := range torrents {
		hashes[i] = t.Hash
	}
	importedAt, err := d.db.ImportedTorrentTimes(ctx, hashes)
	if err != nil {
		return fail("looking up the full engine's imported torrents failed", err)
	}
	victims := rotationVictims(torrents, importedAt, 1)
	if len(victims) == 0 {
		span.SetAttributes(attribute.Bool("rotation.victim_found", false))
		return false
	}
	victim := victims[0]
	if err := client.RemoveTorrent(ctx, victim.Hash, true); err != nil {
		return fail("removing a torrent to make room in the engine failed",
			err, "hash", victim.Hash)
	}
	slog.InfoContext(ctx,
		"engine full, removed the longest-seeded torrent to make room",
		"hash", victim.Hash, "torrent", victim.Name, "ratio", victim.Ratio,
		"imported_at", importedAt[victim.Hash], "client", clientName)
	rotationCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("download_client.name", clientName),
	))
	return true
}

// TrimOverLimitTorrents brings the builtin engine back under its max_torrents
// after the limit was lowered below what it holds: it removes, with their
// files, as many imported torrents as the engine holds past the limit, in the
// order rotation evicts them. Only a hash with a completed record and no
// unfinished one goes (db.ImportedTorrentTimes), so a download in flight, a
// held one and a torrent with no record stay, and an engine with too few
// imported torrents stays over its limit, refusing grabs, until they finish.
//
// The limit is the one the engine runs with (TorrentLimiter), which a config
// edit reaches only on restart. A per-torrent failure is logged and comes back
// on the next tick; only failing to reach the engine or the database is
// returned.
func (d *download) TrimOverLimitTorrents(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "download.trim_over_limit_torrents")
	defer span.End()

	dc, ok := config.BuiltinDownloadClient()
	if !ok {
		return nil
	}
	client, err := d.buildClient(dc)
	if err != nil {
		return otelx.RecordSpanError(
			span, fmt.Errorf("build builtin client: %w", err),
		)
	}
	if _, ok := client.(TorrentLimiter); !ok {
		return nil
	}

	// Shared with rotation, which reads the same listing to pick its victim.
	d.rotateMu.Lock()
	defer d.rotateMu.Unlock()

	torrents, err := client.ListTorrents(ctx)
	if err != nil {
		return otelx.RecordSpanError(
			span, fmt.Errorf("list builtin torrents: %w", err),
		)
	}
	over := overLimit(client, len(torrents))
	span.SetAttributes(attribute.Int("torrents.over_limit", over))
	if over == 0 {
		return nil
	}
	hashes := make([]string, len(torrents))
	for i, t := range torrents {
		hashes[i] = t.Hash
	}
	importedAt, err := d.db.ImportedTorrentTimes(ctx, hashes)
	if err != nil {
		return otelx.RecordSpanError(
			span, fmt.Errorf("look up imported torrents: %w", err),
		)
	}

	removed := 0
	for _, victim := range rotationVictims(torrents, importedAt, over) {
		if err := client.RemoveTorrent(ctx, victim.Hash, true); err != nil {
			slog.WarnContext(ctx,
				"removing a torrent held past the engine's limit failed",
				"hash", victim.Hash, "error", err)
			continue
		}
		removed++
		slog.InfoContext(ctx,
			"engine over its torrent limit, removed an imported torrent",
			"hash", victim.Hash, "torrent", victim.Name, "ratio", victim.Ratio,
			"imported_at", importedAt[victim.Hash], "client", dc.Name)
	}

	span.SetAttributes(attribute.Int("torrents.removed", removed))
	if removed > 0 {
		trimCounter.Add(ctx, int64(removed), metric.WithAttributes(
			attribute.String("download_client.name", dc.Name),
		))
	}
	if left := over - removed; left > 0 {
		slog.DebugContext(ctx,
			"engine still over its torrent limit, nothing else is imported",
			"over", left, "held", len(torrents)-removed, "client", dc.Name)
	}
	return nil
}
