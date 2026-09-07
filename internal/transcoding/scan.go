package transcoding

import (
	"context"
	"log/slog"
)

// Scan asynchronously probes every media file whose profile has a transcode
// policy and queues jobs for non-compliant ones. Returns immediately;
// ErrScanRunning while one is in flight.
func (w *Worker) Scan(ctx context.Context) error {
	if !w.scanning.CompareAndSwap(false, true) {
		return ErrScanRunning
	}

	// The scan outlives the request that triggered it, so it runs detached
	// from the caller's cancellation while keeping the caller's trace.
	sctx, span := tracer.Start(context.WithoutCancel(ctx), "transcoding.scan")
	go func() {
		defer w.scanning.Store(false)
		defer span.End()
		w.runScan(sctx)
	}()
	return nil
}

func (w *Worker) runScan(ctx context.Context) {
	files, err := w.db.ListMediaFilesWithTranscodeOwners(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "could not list media files for a transcode scan",
			"error", err)
		return
	}

	var queued int
	for _, mf := range files {
		pol := policyFor(mf)
		if pol == nil {
			continue
		}
		info, err := w.prober.Probe(ctx, mf.Path)
		if err != nil {
			slog.WarnContext(
				ctx,
				"could not probe a media file during a transcode scan",
				"media_file.id",
				mf.ID,
				"file.path",
				mf.Path,
				"error",
				err,
			)
			continue
		}
		if action, _ := Evaluate(mf.Path, info, *pol); action == ActionNone {
			continue
		}
		if _, err := w.db.CreateTranscodeJob(ctx, mf.ID); err != nil {
			slog.WarnContext(
				ctx,
				"could not queue a transcode job during a transcode scan",
				"media_file.id",
				mf.ID,
				"error",
				err,
			)
			continue
		}
		queued++
	}

	slog.InfoContext(ctx, "transcode scan finished",
		"transcode.queued", queued, "media_file.count", len(files))
}
