package download

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
)

// rotationVictim picks, among the torrents importedAt lists, the one imported
// on the earliest UTC day, and among those of one day the one with the highest
// ratio: it has seeded longest, and of its day has given back the most.
func rotationVictim(
	torrents []Torrent,
	importedAt map[string]time.Time,
) (Torrent, bool) {
	day := func(t Torrent) time.Time {
		return importedAt[t.Hash].UTC().Truncate(24 * time.Hour)
	}
	candidates := slices.DeleteFunc(slices.Clone(torrents), func(t Torrent) bool {
		_, ok := importedAt[t.Hash]
		return !ok
	})
	if len(candidates) == 0 {
		return Torrent{}, false
	}
	return slices.MinFunc(candidates, func(a, b Torrent) int {
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
	}), true
}

// addRotating adds src, and when the builtin engine refuses it for being full
// removes one imported torrent with its files and tries once more. Nothing
// evictable, or an eviction that fails, leaves the refusal as it was.
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
	hashes := make([]string, len(torrents))
	for i, t := range torrents {
		hashes[i] = t.Hash
	}
	importedAt, err := d.db.ImportedTorrentTimes(ctx, hashes)
	if err != nil {
		return fail("looking up the full engine's imported torrents failed", err)
	}
	victim, ok := rotationVictim(torrents, importedAt)
	if !ok {
		span.SetAttributes(attribute.Bool("rotation.victim_found", false))
		return false
	}
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
