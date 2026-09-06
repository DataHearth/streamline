package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/transcodejob"
)

var (
	ErrTranscodeJobNotRetryable  = errors.New("transcode job is not failed")
	ErrTranscodeJobNotCancelable = errors.New("transcode job is not open")
)

// withTranscodeOwners eager-loads a MediaFile's movie/episode owner edges (and
// the episode's season → show chain) so a transcode job's row can render
// "Movie (2019)" / "Show S01E02" and link to it without a follow-up query.
func withTranscodeOwners(q *ent.MediaFileQuery) {
	q.WithMovie(withLeanMovie).
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
// file's owner chain loaded. (nil, nil) when nothing is queued.
func (db *DB) ClaimNextTranscodeJob(ctx context.Context) (*ent.TranscodeJob, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	job, err := tx.TranscodeJob.Query().
		Where(transcodejob.StatusEQ(transcodejob.StatusQueued)).
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

// RetryTranscodeJob resets a failed job back to queued with a clean slate —
// attempts, error and finished_at all cleared. Any other status is
// ErrTranscodeJobNotRetryable.
func (db *DB) RetryTranscodeJob(ctx context.Context, id uint32) error {
	n, err := db.client.TranscodeJob.Update().
		Where(
			transcodejob.IDEQ(id),
			transcodejob.StatusEQ(transcodejob.StatusFailed),
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
// ListImportingDownloadRecords' role for the import path.
func (db *DB) ResetRunningTranscodeJobs(ctx context.Context) (int, error) {
	n, err := db.client.TranscodeJob.Update().
		Where(transcodejob.StatusEQ(transcodejob.StatusRunning)).
		SetStatus(transcodejob.StatusQueued).
		Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("reset running transcode jobs: %w", err)
	}
	return n, nil
}

// ListTranscodeJobs returns up to limit jobs newest-first (create_time, then
// id, both descending) with each job's media file and owner chain loaded —
// what the REST layer renders as "Movie (2019)" / "Show S01E02".
func (db *DB) ListTranscodeJobs(
	ctx context.Context,
	limit int,
) ([]*ent.TranscodeJob, error) {
	rows, err := db.client.TranscodeJob.Query().
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
	return rows, nil
}

// ListMediaFilesWithTranscodeOwners returns every MediaFile with its owner
// chain loaded — the transcode scan's candidate set.
func (db *DB) ListMediaFilesWithTranscodeOwners(
	ctx context.Context,
) ([]*ent.MediaFile, error) {
	q := db.client.MediaFile.Query()
	withTranscodeOwners(q)
	rows, err := q.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list media_files with transcode owners: %w", err)
	}
	return rows, nil
}
