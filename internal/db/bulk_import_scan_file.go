package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	"github.com/datahearth/streamline/ent/schema"
)

// ErrImportScanFileNotFound reports that a scan-scoped UPDATE matched no row:
// the file id does not exist, or it belongs to a different scan. An
// affected-rows check has no ent error to hand back — NotFoundError's label
// field is unexported, so one built here renders "ent:  not found" — so the
// miss travels as a sentinel, the way ConsumeInvite's ErrInviteUsed does.
var ErrImportScanFileNotFound = errors.New("import scan file not found")

type CreateImportScanFileParams struct {
	SourcePath         string
	Size               int64
	ParsedTitle        string
	ParsedYear         *uint16
	ParsedQuality      string
	ParsedReleaseGroup string
	Classification     entimportscanfile.Classification
	Candidates         []schema.ScannedCandidate
	TMDBID             uint32 // 0 = unset
	ExistingMovieID    uint32 // 0 = unset
}

type FilterImportScanFilesParams struct {
	ScanID         uint32
	Classification entimportscanfile.Classification // empty = all
	Query          string
	Offset         uint32
	Limit          uint32
}

type UpdateScanFileOutcomeOpts struct {
	Message        string
	CreatedMovieID uint32
}

// ListPendingImportScanFilePaths returns the source_path of every
// ImportScanFile attached to a scan whose status is still "awaiting_review".
// Used by orphan_scan to skip files already queued for human review.
func (db *DB) ListPendingImportScanFilePaths(ctx context.Context) ([]string, error) {
	rows, err := db.client.ImportScanFile.Query().
		Where(entimportscanfile.HasScanWith(
			entimportscan.StatusEQ(entimportscan.StatusAwaitingReview),
		)).
		Select(entimportscanfile.FieldSourcePath).
		Strings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending import_scan_file paths: %w", err)
	}
	return rows, nil
}

func (db *DB) BulkCreateImportScanFiles(
	ctx context.Context,
	scanID uint32,
	files []CreateImportScanFileParams,
) error {
	if len(files) == 0 {
		return nil
	}
	creates := make([]*ent.ImportScanFileCreate, 0, len(files))
	for _, p := range files {
		c := db.client.ImportScanFile.Create().
			SetScanID(scanID).
			SetSourcePath(p.SourcePath).
			SetSize(p.Size).
			SetClassification(p.Classification)
		applyImportScanFileFields(c, p)
		creates = append(creates, c)
	}
	if _, err := db.client.ImportScanFile.CreateBulk(creates...).
		Save(ctx); err != nil {
		return fmt.Errorf("bulk create import scan files: %w", err)
	}
	return nil
}

func applyImportScanFileFields(
	c *ent.ImportScanFileCreate,
	p CreateImportScanFileParams,
) {
	if p.ParsedTitle != "" {
		c.SetParsedTitle(p.ParsedTitle)
	}
	if p.ParsedYear != nil {
		c.SetParsedYear(*p.ParsedYear)
	}
	if p.ParsedQuality != "" {
		c.SetParsedQuality(p.ParsedQuality)
	}
	if p.ParsedReleaseGroup != "" {
		c.SetParsedReleaseGroup(p.ParsedReleaseGroup)
	}
	if len(p.Candidates) > 0 {
		c.SetCandidates(p.Candidates)
	}
	if p.TMDBID != 0 {
		c.SetTmdbID(p.TMDBID)
	}
	if p.ExistingMovieID != 0 {
		c.SetExistingMovieID(p.ExistingMovieID)
	}
}

func (db *DB) FilterImportScanFiles(
	ctx context.Context,
	p FilterImportScanFilesParams,
) ([]*ent.ImportScanFile, uint32, error) {
	q := db.client.ImportScanFile.Query().
		Where(entimportscanfile.HasScanWith(entimportscan.ID(p.ScanID)))
	if p.Classification != "" {
		q = q.Where(entimportscanfile.ClassificationEQ(p.Classification))
	}
	if p.Query != "" {
		q = q.Where(entimportscanfile.SourcePathContainsFold(p.Query))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count import scan files: %w", err)
	}
	limit := p.Limit
	if limit == 0 {
		limit = 50
	}
	rows, err := q.Order(ent.Asc(entimportscanfile.FieldSourcePath)).
		Offset(int(p.Offset)).Limit(int(limit)).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list import scan files: %w", err)
	}
	return rows, uint32(total), nil //nolint:gosec // count is non-negative
}

func (db *DB) FindImportScanFile(
	ctx context.Context,
	scanID, fileID uint32,
) (*ent.ImportScanFile, error) {
	row, err := db.client.ImportScanFile.Query().
		Where(
			entimportscanfile.ID(fileID),
			entimportscanfile.HasScanWith(entimportscan.ID(scanID)),
		).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("find import scan file: %w", err)
	}
	return row, nil
}

// UpdateImportScanFileDecision records a reviewer decision on the file with
// fileID under scanID. The scan predicate lives in the UPDATE itself so a
// fileID belonging to another scan matches nothing instead of being mutated
// before a scoped read-back can reject the request.
func (db *DB) UpdateImportScanFileDecision(
	ctx context.Context,
	scanID, fileID uint32,
	decision entimportscanfile.Decision,
	tmdbID *uint32,
) error {
	u := db.client.ImportScanFile.Update().
		Where(
			entimportscanfile.ID(fileID),
			entimportscanfile.HasScanWith(entimportscan.ID(scanID)),
		).
		SetDecision(decision)
	if tmdbID != nil {
		u = u.SetDecisionTmdbID(*tmdbID)
	} else {
		u = u.ClearDecisionTmdbID()
	}
	n, err := u.Save(ctx)
	if err != nil {
		return fmt.Errorf("update import scan file decision: %w", err)
	}
	if n == 0 {
		return ErrImportScanFileNotFound
	}
	return nil
}

func (db *DB) UpdateImportScanFileOutcome(
	ctx context.Context,
	id uint32,
	outcome entimportscanfile.Outcome,
	opts UpdateScanFileOutcomeOpts,
) error {
	u := db.client.ImportScanFile.UpdateOneID(id).SetOutcome(outcome)
	if opts.Message != "" {
		u = u.SetOutcomeMessage(opts.Message)
	}
	if opts.CreatedMovieID != 0 {
		u = u.SetCreatedMovieID(opts.CreatedMovieID)
	}
	return u.Exec(ctx)
}

func (db *DB) ListImportScanFilesForCommit(
	ctx context.Context,
	scanID uint32,
) ([]*ent.ImportScanFile, error) {
	return db.client.ImportScanFile.Query().
		Where(
			entimportscanfile.HasScanWith(entimportscan.ID(scanID)),
			// Skip is an explicit exclusion. Everything else commits if it was
			// either accepted by the reviewer or auto-matched (confirmed /
			// existing) — mirrors the "Commit N files" count in the UI.
			entimportscanfile.DecisionNEQ(entimportscanfile.DecisionSkip),
			entimportscanfile.Or(
				entimportscanfile.DecisionEQ(entimportscanfile.DecisionAccept),
				entimportscanfile.ClassificationIn(
					entimportscanfile.ClassificationConfirmed,
					entimportscanfile.ClassificationExisting,
				),
			),
		).
		All(ctx)
}

// BulkUpdateImportScanFileDecisions sets one decision across a scan's files,
// narrowed by classification and/or an explicit id list, and returns how many
// rows changed. It never writes decision_tmdb_id: a bulk action applies to rows
// the reviewer has not individually picked a match for, so the scan-time match
// stands.
func (db *DB) BulkUpdateImportScanFileDecisions(
	ctx context.Context,
	scanID uint32,
	decision entimportscanfile.Decision,
	classification entimportscanfile.Classification,
	ids []uint32,
) (int, error) {
	u := db.client.ImportScanFile.Update().
		Where(entimportscanfile.HasScanWith(entimportscan.ID(scanID))).
		SetDecision(decision)
	if classification != "" {
		u = u.Where(entimportscanfile.ClassificationEQ(classification))
	}
	if len(ids) > 0 {
		u = u.Where(entimportscanfile.IDIn(ids...))
	}
	n, err := u.Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("bulk update import scan file decisions: %w", err)
	}
	return n, nil
}
