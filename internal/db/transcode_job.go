package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/transcodejob"
)

var (
	ErrTranscodeJobNotRetryable = errors.New(
		"transcode job is not failed or rejected",
	)
	ErrTranscodeJobNotCancelable = errors.New("transcode job is not open")
)

// withTranscodeOwners eager-loads a MediaFile's movie/episode owner edges (and
// the episode's season → show chain) so a transcode job's row can render
// "Movie (2019)" / "Show S01E02" and link to it without a follow-up query.
func withTranscodeOwners(q *ent.MediaFileQuery) {
	q.WithMovie().
		WithEpisode(func(eq *ent.EpisodeQuery) {
			eq.WithSeason(func(sq *ent.SeasonQuery) {
				sq.WithTvShow()
			})
		})
}

// CreateTranscodeJob queues a transcode for mediaFileID, or returns the
// job already queued/running for that file rather than duplicating it — a
// file can only be mid-transcode once.
func (db *DB) CreateTranscodeJob(
	ctx context.Context,
	mediaFileID uint32,
) (*ent.TranscodeJob, error) {
	existing, err := db.client.TranscodeJob.Query().
		Where(
			transcodejob.HasMediaFileWith(mediafile.IDEQ(mediaFileID)),
			transcodejob.StatusIn(transcodejob.StatusQueued, transcodejob.StatusRunning),
		).
		Only(ctx)
	if err == nil {
		return existing, nil
	}
	if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("query existing transcode job: %w", err)
	}

	job, err := db.client.TranscodeJob.Create().
		SetMediaFileID(mediaFileID).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create transcode job: %w", err)
	}
	return job, nil
}

// ClaimNextTranscodeJob atomically moves the oldest queued job to running,
// bumping attempts and stamping started_at, and returns it with its media
// file's owner chain loaded. A row deferred past now is skipped until its
// deadline; the claim that finally takes it clears the stamp. (nil, nil)
// when nothing is claimable.
func (db *DB) ClaimNextTranscodeJob(ctx context.Context) (*ent.TranscodeJob, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	job, err := tx.TranscodeJob.Query().
		Where(
			transcodejob.StatusEQ(transcodejob.StatusQueued),
			transcodejob.Or(
				transcodejob.DeferredUntilIsNil(),
				transcodejob.DeferredUntilLTE(time.Now()),
			),
		).
		Order(ent.Asc(transcodejob.FieldCreateTime), ent.Asc(transcodejob.FieldID)).
		First(ctx)
	if err != nil {
		tx.Rollback()
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("find next queued transcode job: %w", err)
	}

	updated, err := tx.TranscodeJob.UpdateOneID(job.ID).
		SetStatus(transcodejob.StatusRunning).
		SetAttempts(job.Attempts + 1).
		SetStartedAt(time.Now()).
		ClearDeferredUntil().
		Save(ctx)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("claim transcode job %d: %w", job.ID, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim of transcode job %d: %w", job.ID, err)
	}

	claimed, err := db.client.TranscodeJob.Query().
		Where(transcodejob.IDEQ(updated.ID)).
		WithMediaFile(withTranscodeOwners).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"reload claimed transcode job %d: %w",
			updated.ID,
			err,
		)
	}
	return claimed, nil
}

// CompleteTranscodeJob marks id succeeded, records the before/after sizes,
// stamps finished_at, and clears any error left over from earlier attempts.
func (db *DB) CompleteTranscodeJob(
	ctx context.Context,
	id uint32,
	sizeBefore, sizeAfter int64,
) error {
	if err := db.client.TranscodeJob.UpdateOneID(id).
		SetStatus(transcodejob.StatusSucceeded).
		SetSizeBefore(sizeBefore).
		SetSizeAfter(sizeAfter).
		SetFinishedAt(time.Now()).
		ClearError().
		Exec(ctx); err != nil {
		return fmt.Errorf("complete transcode job %d: %w", id, err)
	}
	return nil
}

// FailTranscodeJob records a failed attempt. A non-terminal failure returns
// the job to queued (with the error recorded) so the worker retries it;
// terminal marks it failed with finished_at stamped.
func (db *DB) FailTranscodeJob(
	ctx context.Context,
	id uint32,
	reason string,
	terminal bool,
) error {
	q := db.client.TranscodeJob.UpdateOneID(id).SetError(reason)
	if terminal {
		q = q.SetStatus(transcodejob.StatusFailed).SetFinishedAt(time.Now())
	} else {
		q = q.SetStatus(transcodejob.StatusQueued)
	}
	if err := q.Exec(ctx); err != nil {
		return fmt.Errorf("fail transcode job %d: %w", id, err)
	}
	return nil
}

// DeferTranscodeJob returns id to the queue with a deadline before which no
// claim may take it. attempts is walked back by one and started_at cleared:
// the claim that produced this job bumped both, and a deferral is not an
// attempt — a file held for a long seed would otherwise exhaust max_failures
// without ffmpeg ever running.
func (db *DB) DeferTranscodeJob(
	ctx context.Context,
	id uint32,
	until time.Time,
) error {
	job, err := db.client.TranscodeJob.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("load transcode job %d: %w", id, err)
	}
	q := db.client.TranscodeJob.UpdateOneID(id).
		SetStatus(transcodejob.StatusQueued).
		SetDeferredUntil(until).
		ClearStartedAt()
	if job.Attempts > 0 {
		q = q.SetAttempts(job.Attempts - 1)
	}
	if err := q.Exec(ctx); err != nil {
		return fmt.Errorf("defer transcode job %d: %w", id, err)
	}
	return nil
}

// RejectTranscodeJob parks id as rejected: the encode verified as worse than
// or broken relative to its source. Terminal like a failed job, but it also
// records both sizes, since the numbers are usually the reason. Conditional
// on the row still being running — an operator's cancel arriving during
// verification must win, not be overwritten by a verdict about bytes the
// job no longer owns.
func (db *DB) RejectTranscodeJob(
	ctx context.Context,
	id uint32,
	reason string,
	sizeBefore, sizeAfter int64,
) error {
	n, err := db.client.TranscodeJob.Update().
		Where(
			transcodejob.IDEQ(id),
			transcodejob.StatusEQ(transcodejob.StatusRunning),
		).
		SetStatus(transcodejob.StatusRejected).
		SetError(reason).
		SetSizeBefore(sizeBefore).
		SetSizeAfter(sizeAfter).
		SetFinishedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("reject transcode job %d: %w", id, err)
	}
	if n == 0 {
		slog.DebugContext(ctx, "transcode job was no longer running when rejected",
			"transcode.job_id", id)
	}
	return nil
}

// MarkTranscodeJobCanceled cancels id from queued or running, stamping
// finished_at. Any other status is ErrTranscodeJobNotCancelable.
func (db *DB) MarkTranscodeJobCanceled(ctx context.Context, id uint32) error {
	n, err := db.client.TranscodeJob.Update().
		Where(
			transcodejob.IDEQ(id),
			transcodejob.StatusIn(transcodejob.StatusQueued, transcodejob.StatusRunning),
		).
		SetStatus(transcodejob.StatusCanceled).
		SetFinishedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("cancel transcode job %d: %w", id, err)
	}
	if n == 0 {
		return ErrTranscodeJobNotCancelable
	}
	return nil
}

// RetryTranscodeJob resets a failed or rejected job back to queued with a
// clean slate — attempts, error and finished_at all cleared. Any other
// status is ErrTranscodeJobNotRetryable.
func (db *DB) RetryTranscodeJob(ctx context.Context, id uint32) error {
	n, err := db.client.TranscodeJob.Update().
		Where(
			transcodejob.IDEQ(id),
			transcodejob.StatusIn(transcodejob.StatusFailed, transcodejob.StatusRejected),
		).
		SetStatus(transcodejob.StatusQueued).
		SetAttempts(0).
		SetError("").
		ClearFinishedAt().
		Save(ctx)
	if err != nil {
		return fmt.Errorf("retry transcode job %d: %w", id, err)
	}
	if n == 0 {
		return ErrTranscodeJobNotRetryable
	}
	return nil
}

// ResetRunningTranscodeJobs bulk-reverts every running job back to queued —
// restart-safety for a worker that died mid-transcode, mirroring
// ListImportingDownloadRecords' role for the import path. Refunds the
// attempt ClaimNextTranscodeJob spent and clears started_at, same as
// DeferTranscodeJob: a restart mid-encode is not a failed attempt, and
// without the refund a deploy during a long encode silently spent one of
// the job's max_failures tries.
func (db *DB) ResetRunningTranscodeJobs(ctx context.Context) (int, error) {
	refunded, err := db.client.TranscodeJob.Update().
		Where(
			transcodejob.StatusEQ(transcodejob.StatusRunning),
			transcodejob.AttemptsGT(0),
		).
		SetStatus(transcodejob.StatusQueued).
		AddAttempts(-1).
		ClearStartedAt().
		Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("reset running transcode jobs: %w", err)
	}
	// A running row at attempts 0 is unreachable through a claim, but a bulk
	// decrement would wrap the counter, so it is requeued without the refund
	// rather than left running for nothing to ever drain.
	rest, err := db.client.TranscodeJob.Update().
		Where(transcodejob.StatusEQ(transcodejob.StatusRunning)).
		SetStatus(transcodejob.StatusQueued).
		ClearStartedAt().
		Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("reset running transcode jobs: %w", err)
	}
	return refunded + rest, nil
}

// ListTranscodeJobs returns the newest limit jobs of every status other than
// rejected, plus every rejected row regardless of age, merged and sorted
// newest-first (create_time, then id, both descending), each with its media
// file and owner chain loaded — what the REST layer renders as "Movie
// (2019)" / "Show S01E02". A rejected row is the only handle on a file the
// scan now skips (ListMediaFilesWithTranscodeOwners excludes it) and Retry
// is its only exit, so it must stay reachable no matter how old — the same
// reason held download records always appear in the live queue.
func (db *DB) ListTranscodeJobs(
	ctx context.Context,
	limit int,
) ([]*ent.TranscodeJob, error) {
	rows, err := db.client.TranscodeJob.Query().
		Where(transcodejob.StatusNEQ(transcodejob.StatusRejected)).
		Order(
			ent.Desc(transcodejob.FieldCreateTime),
			ent.Desc(transcodejob.FieldID),
		).
		Limit(limit).
		WithMediaFile(withTranscodeOwners).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list transcode jobs: %w", err)
	}

	rejected, err := db.client.TranscodeJob.Query().
		Where(transcodejob.StatusEQ(transcodejob.StatusRejected)).
		WithMediaFile(withTranscodeOwners).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list rejected transcode jobs: %w", err)
	}

	rows = append(rows, rejected...)
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].CreateTime.Equal(rows[j].CreateTime) {
			return rows[i].CreateTime.After(rows[j].CreateTime)
		}
		return rows[i].ID > rows[j].ID
	})
	return rows, nil
}

// ListMediaFilesWithTranscodeOwners returns every MediaFile with its owner
// chain loaded — the transcode scan's candidate set. A file holding a
// rejected job is left out: the encode was judged worse than the source, and
// a retry from the queue is the only way to ask again (it flips that same row
// back to queued, so "has a rejected job" is exactly "rejected, not retried").
func (db *DB) ListMediaFilesWithTranscodeOwners(
	ctx context.Context,
) ([]*ent.MediaFile, error) {
	q := db.client.MediaFile.Query().
		Where(mediafile.Not(mediafile.HasTranscodeJobsWith(
			transcodejob.StatusEQ(transcodejob.StatusRejected),
		)))
	withTranscodeOwners(q)
	rows, err := q.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list media_files with transcode owners: %w", err)
	}
	return rows, nil
}
