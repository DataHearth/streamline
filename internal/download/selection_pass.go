package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RunSelectionPass resolves every download_record still parked at
// selection_state=pending (spec §4.5) — a magnet grab whose keep-set
// couldn't be computed at grab time because the file list wasn't known yet.
// One bad record must not starve the rest, so per-record failures are
// logged and the loop continues; only the initial list query fails the
// whole pass.
func (d *download) RunSelectionPass(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "download.run_selection_pass")
	defer span.End()

	records, err := d.db.ListPendingSelectionRecords(ctx)
	if err != nil {
		return otelx.RecordSpanError(
			span, fmt.Errorf("list pending selection records: %w", err),
		)
	}
	span.SetAttributes(attribute.Int("records.count", len(records)))

	for _, rec := range records {
		if err := d.resolvePendingSelection(ctx, rec); err != nil {
			slog.WarnContext(ctx, "file selection: resolve record failed",
				"record.id", rec.ID, "hash", rec.TorrentHash, "error", err)
		}
	}
	return nil
}

// resolvePendingSelection resolves one pending record: metadata not yet
// known is a no-op (unless the grace window expired), a zero-match keep-set
// drops the release (spec §6 rule 1), and a partial or full match applies or
// confirms the selection exactly like Flow A's post-add confirmation.
func (d *download) resolvePendingSelection(
	ctx context.Context, rec *ent.DownloadRecord,
) error {
	ctx, span := tracer.Start(ctx, "download.resolve_selection",
		trace.WithAttributes(
			attribute.Int64("download_record.id", int64(rec.ID)),
		),
	)
	defer span.End()

	dc, ok := config.FindDownloadClient(rec.DownloadClientName)
	if !ok {
		return otelx.RecordSpanError(span, fmt.Errorf(
			"download client %q not found", rec.DownloadClientName,
		))
	}
	client, err := d.buildClient(dc)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}

	clientFiles, err := client.ListFiles(ctx, rec.TorrentHash)
	switch {
	case errors.Is(err, ErrNotSupported):
		// This client can never report a per-file list at all — terminal,
		// same as an unsupported SetWantedFiles confirmation below.
		return d.finalizePending(
			ctx, span, client, rec, downloadrecord.SelectionStateUnsupported,
		)
	case err != nil:
		if time.Since(rec.CreateTime) <=
			config.Get().Download.SelectionGraceDuration() {
			// Could be transient (a client briefly unreachable) or permanent
			// (a removed hash, a dead host) — indistinguishable this tick, so
			// retry until the grace window says otherwise.
			return otelx.RecordSpanError(span, fmt.Errorf("list files: %w", err))
		}
		// Grace expired with ListFiles still erroring: give up exactly like
		// an empty listing past the deadline, rather than stranding the
		// record at pending forever on a hash the client will never resolve.
		return d.finalizePending(
			ctx, span, client, rec, downloadrecord.SelectionStateSkipped,
		)
	}

	if len(clientFiles) == 0 {
		if time.Since(rec.CreateTime) <=
			config.Get().Download.SelectionGraceDuration() {
			// Metadata not yet resolved (e.g. a magnet still fetching peers)
			// — try again next tick.
			return nil
		}
		// Give up rather than hang forever on a magnet whose metadata never
		// arrives: download the whole torrent instead.
		return d.finalizePending(
			ctx, span, client, rec, downloadrecord.SelectionStateSkipped,
		)
	}

	show, err := d.showForRecord(ctx, rec)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("resolve show: %w", err))
	}

	files := make([]metaFile, len(clientFiles))
	for i, f := range clientFiles {
		files[i] = metaFile{Index: f.Index, Path: f.Path, Size: f.Size}
	}
	keep, keptBytes, matched := computeKeepSet(
		files, show.Edges.Seasons, show.Type == tvshow.TypeAnime,
		rec.WantedEpisodes,
	)

	if matched == 0 {
		// Nothing in this torrent serves any wanted episode, and nothing in
		// it ever will — the importer runs the same matcher over the same
		// basenames, so downloading the rest buys nothing (spec §6 rule 1).
		//
		// The DB writes land first, RemoveTorrent last: they're the cheap,
		// reversible half. A RemoveTorrent that runs first and then fails to
		// write would strand the record at pending with no torrent left
		// behind it — ListPendingSelectionRecords filters on selection_state
		// alone, so a pending record with a dead hash would re-enter this
		// pass every tick until the 14-day failed-record retention, erroring
		// against a hash that no longer exists. Flipping selection_state to
		// skipped here (alongside the fail) is what takes it out of that
		// query for good, whether or not RemoveTorrent below still succeeds.
		if err := d.db.SetDownloadRecordSelection(
			ctx, rec.ID, downloadrecord.SelectionStateSkipped, nil, 0,
		); err != nil {
			return otelx.RecordSpanError(
				span, fmt.Errorf("mark selection skipped: %w", err),
			)
		}
		reason := fmt.Sprintf(
			"no files in this release matched wanted episodes %s",
			strings.Join(episodeLabels(show, rec.WantedEpisodes), ", "),
		)
		if err := d.db.FailDownloadRecord(ctx, rec.ID, reason); err != nil {
			return otelx.RecordSpanError(
				span, fmt.Errorf("fail download record: %w", err),
			)
		}
		if anchor := rec.Edges.Episode; anchor != nil {
			if ierr := d.db.IncrementEpisodeGrabFailures(
				ctx, anchor.ID,
			); ierr != nil {
				slog.WarnContext(ctx,
					"file selection: bump episode grab_failures failed",
					"episode.id", anchor.ID, "error", ierr)
			}
		}
		if err := client.RemoveTorrent(ctx, rec.TorrentHash, true); err != nil {
			return otelx.RecordSpanError(
				span, fmt.Errorf("remove torrent: %w", err),
			)
		}
		slog.WarnContext(ctx, "file selection: dropped zero-match magnet",
			"record.id", rec.ID, "hash", rec.TorrentHash,
			"wanted_episodes", rec.WantedEpisodes, "files", clientFiles)
		return nil
	}

	videoTotal := countVideoCandidates(files)
	if len(keep) == len(files) || matched == videoTotal {
		// Every candidate serves a wanted episode: not selective, take it
		// whole.
		return d.finalizePending(
			ctx, span, client, rec, downloadrecord.SelectionStateSkipped,
		)
	}

	switch serr := client.SetWantedFiles(ctx, rec.TorrentHash, keep); {
	case errors.Is(serr, ErrNotSupported):
		if err := d.db.SetDownloadRecordSelection(
			ctx, rec.ID, downloadrecord.SelectionStateUnsupported, nil, 0,
		); err != nil {
			return otelx.RecordSpanError(
				span, fmt.Errorf("mark selection unsupported: %w", err),
			)
		}
	case serr != nil:
		return otelx.RecordSpanError(
			span, fmt.Errorf("set wanted files: %w", serr),
		)
	default:
		if err := d.db.SetDownloadRecordSelection(
			ctx, rec.ID, downloadrecord.SelectionStateApplied, keep, keptBytes,
		); err != nil {
			return otelx.RecordSpanError(
				span, fmt.Errorf("mark selection applied: %w", err),
			)
		}
	}
	// Starts a qB torrent stopped at metadata; no-op for an already-running
	// builtin/Transmission torrent.
	if err := client.ResumeTorrent(ctx, rec.TorrentHash); err != nil {
		slog.WarnContext(ctx, "file selection: resume torrent failed",
			"record.id", rec.ID, "hash", rec.TorrentHash, "error", err)
	}
	return nil
}

// finalizePending marks rec's selection at a terminal (non-pending) state and
// resumes the torrent — the shared shape of every "give up on this record"
// arm: grace expired on an empty or a persistently-erroring file listing, and
// a client whose ListFiles reports it can never support selection at all.
// Landing the DB write before the client call is what keeps the record out
// of the next ListPendingSelectionRecords pass even when ResumeTorrent fails.
func (d *download) finalizePending(
	ctx context.Context,
	span trace.Span,
	client Client,
	rec *ent.DownloadRecord,
	state downloadrecord.SelectionState,
) error {
	if err := d.db.SetDownloadRecordSelection(
		ctx, rec.ID, state, nil, 0,
	); err != nil {
		return otelx.RecordSpanError(
			span, fmt.Errorf("mark selection %s: %w", state, err),
		)
	}
	if err := client.ResumeTorrent(ctx, rec.TorrentHash); err != nil {
		slog.WarnContext(ctx, "file selection: resume torrent failed",
			"record.id", rec.ID, "hash", rec.TorrentHash, "error", err)
	}
	return nil
}

// episodeLabels renders ids as SxxExx against show's already-fetched season
// tree, in ids order. failure_reason is read by a human in the queue, and a
// list of episode row ids names nothing they can act on. An id the tree
// doesn't hold (a provider reconcile dropped it between grab and this pass)
// falls back to its number so the reason is never silently short.
func episodeLabels(show *ent.TVShow, ids []uint32) []string {
	labels := make(map[uint32]string)
	for _, se := range show.Edges.Seasons {
		for _, ep := range se.Edges.Episodes {
			labels[ep.ID] = fmt.Sprintf("S%02dE%02d", se.Number, ep.Number)
		}
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		if l, ok := labels[id]; ok {
			out[i] = l
			continue
		}
		out[i] = strconv.FormatUint(uint64(id), 10)
	}
	return out
}

// showForRecord resolves the TV show tree for rec's anchor episode, preferring
// the eager-loaded edge ListPendingSelectionRecords already carries so the
// common case needs no extra query.
func (d *download) showForRecord(
	ctx context.Context, rec *ent.DownloadRecord,
) (*ent.TVShow, error) {
	if ep := rec.Edges.Episode; ep != nil &&
		ep.Edges.Season != nil && ep.Edges.Season.Edges.TvShow != nil {
		return ep.Edges.Season.Edges.TvShow, nil
	}
	if rec.Edges.Episode == nil {
		return nil, fmt.Errorf("download record %d has no episode", rec.ID)
	}
	return d.db.TVShowForEpisode(ctx, rec.Edges.Episode.ID)
}
