package db

import (
	"context"
	"fmt"
	"time"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
)

type CreateImportScanParams struct {
	SourcePath string
	Kind       entimportscan.Kind // empty = movie (schema default)
	Mode       entimportscan.Mode
	ImportMode entimportscan.ImportMode // empty = fall back to library.import_mode at commit time
}

type UpdateScanStatusOpts struct {
	TotalCount         *int
	FailureReason      *string
	ScannedAt          *time.Time
	CommittedAt        *time.Time
	CommitSuccessCount *uint32
	CommitFailedCount  *uint32
}

func (db *DB) CreateImportScan(
	ctx context.Context,
	p CreateImportScanParams,
) (*ent.ImportScan, error) {
	c := db.client.ImportScan.Create().
		SetSourcePath(p.SourcePath).
		SetMode(p.Mode)
	if p.Kind != "" {
		c = c.SetKind(p.Kind)
	}
	if p.ImportMode != "" {
		c = c.SetImportMode(p.ImportMode)
	}
	return c.Save(ctx)
}

func (db *DB) FindImportScan(
	ctx context.Context,
	id uint32,
) (*ent.ImportScan, error) {
	return db.client.ImportScan.Get(ctx, id)
}

// FindOpenImportScanForSource returns the oldest awaiting_review scan for
// sourcePath, or ent NotFound. The orphan scanner uses it to fold new orphans
// into the one open review queue for a directory instead of minting a fresh
// scan on every run.
func (db *DB) FindOpenImportScanForSource(
	ctx context.Context,
	sourcePath string,
) (*ent.ImportScan, error) {
	return db.client.ImportScan.Query().
		Where(
			entimportscan.SourcePathEQ(sourcePath),
			entimportscan.StatusEQ(entimportscan.StatusAwaitingReview),
		).
		Order(ent.Asc(entimportscan.FieldID)).
		First(ctx)
}

func (db *DB) ListImportScans(
	ctx context.Context,
	offset, limit uint32,
) ([]*ent.ImportScan, int, error) {
	q := db.client.ImportScan.Query().Order(ent.Desc(entimportscan.FieldCreateTime))
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count import scans: %w", err)
	}
	rows, err := q.Offset(int(offset)).Limit(int(limit)).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list import scans: %w", err)
	}
	return rows, total, nil
}

func (db *DB) UpdateImportScanStatus(
	ctx context.Context,
	id uint32,
	status entimportscan.Status,
	opts UpdateScanStatusOpts,
) error {
	u := db.client.ImportScan.UpdateOneID(id).SetStatus(status)
	if opts.TotalCount != nil {
		//nolint:gosec // scan file counts sit far below uint32 range
		u = u.SetTotalCount(uint32(*opts.TotalCount))
	}
	if opts.FailureReason != nil {
		u = u.SetFailureReason(*opts.FailureReason)
	}
	if opts.ScannedAt != nil {
		u = u.SetScannedAt(*opts.ScannedAt)
	}
	if opts.CommittedAt != nil {
		u = u.SetCommittedAt(*opts.CommittedAt)
	}
	if opts.CommitSuccessCount != nil {
		u = u.SetCommitSuccessCount(*opts.CommitSuccessCount)
	}
	if opts.CommitFailedCount != nil {
		u = u.SetCommitFailedCount(*opts.CommitFailedCount)
	}
	return u.Exec(ctx)
}

func (db *DB) IncrementImportScanProgress(
	ctx context.Context,
	id uint32,
	processedDelta int,
) error {
	//nolint:gosec // delta is a flush-batch length, far below int32 range
	return db.client.ImportScan.UpdateOneID(id).
		AddProcessedCount(int32(processedDelta)).
		Exec(ctx)
}

func (db *DB) CountActiveImportScans(ctx context.Context) (int, error) {
	n, err := db.client.ImportScan.Query().
		Where(entimportscan.StatusIn(entimportscan.StatusRunning, entimportscan.StatusCommitting)).
		Count(ctx)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (db *DB) DeleteImportScan(ctx context.Context, id uint32) error {
	return db.client.ImportScan.DeleteOneID(id).Exec(ctx)
}

func (db *DB) AbortInflightImportScans(
	ctx context.Context,
	reason string,
) (int, error) {
	n, err := db.client.ImportScan.Update().
		Where(entimportscan.StatusIn(entimportscan.StatusRunning, entimportscan.StatusCommitting)).
		SetStatus(entimportscan.StatusFailed).
		SetFailureReason(reason).
		Save(ctx)
	if err != nil {
		return 0, err
	}
	return n, nil
}
