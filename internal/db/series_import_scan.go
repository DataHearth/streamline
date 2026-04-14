package db

import (
	"context"
	"fmt"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/schema"
)

type CreateImportScanShowParams struct {
	FolderPath       string
	ParsedTitle      string
	ParsedYear       *uint16
	Classification   entimportscanshow.Classification
	TVDBID           *uint32
	Candidates       []schema.ScannedShowCandidate
	ExistingTvshowID *uint32
	FileCount        uint16
}

type ListImportScanShowsParams struct {
	ScanID         uint32
	Classification entimportscanshow.Classification // empty = all
	Offset, Limit  uint32
}

type UpdateScanShowOutcomeOpts struct {
	Message         string
	CreatedTvshowID uint32
}

// ListPendingImportScanShowFolders returns folder_path of every show attached to
// a scan still awaiting_review. The series scanner uses it to skip folders
// already queued for review (the show-level analogue of pending file paths).
func (db *DB) ListPendingImportScanShowFolders(
	ctx context.Context,
) ([]string, error) {
	rows, err := db.client.ImportScanShow.Query().
		Where(entimportscanshow.HasScanWith(
			entimportscan.StatusEQ(entimportscan.StatusAwaitingReview),
		)).
		Select(entimportscanshow.FieldFolderPath).
		Strings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending import_scan_show folders: %w", err)
	}
	return rows, nil
}

func (db *DB) BulkCreateImportScanShows(
	ctx context.Context, scanID uint32, shows []CreateImportScanShowParams,
) error {
	if len(shows) == 0 {
		return nil
	}
	creates := make([]*ent.ImportScanShowCreate, 0, len(shows))
	for _, p := range shows {
		c := db.client.ImportScanShow.Create().
			SetScanID(scanID).
			SetFolderPath(p.FolderPath).
			SetFileCount(p.FileCount).
			SetClassification(p.Classification)
		if p.ParsedTitle != "" {
			c.SetParsedTitle(p.ParsedTitle)
		}
		if p.ParsedYear != nil {
			c.SetParsedYear(*p.ParsedYear)
		}
		if p.TVDBID != nil {
			c.SetTvdbID(*p.TVDBID)
		}
		if p.ExistingTvshowID != nil {
			c.SetExistingTvshowID(*p.ExistingTvshowID)
		}
		if len(p.Candidates) > 0 {
			c.SetCandidates(p.Candidates)
		}
		creates = append(creates, c)
	}
	if _, err := db.client.ImportScanShow.CreateBulk(creates...).
		Save(ctx); err != nil {
		return fmt.Errorf("bulk create import scan shows: %w", err)
	}
	return nil
}

func (db *DB) ListImportScanShows(
	ctx context.Context, p ListImportScanShowsParams,
) ([]*ent.ImportScanShow, uint32, error) {
	q := db.client.ImportScanShow.Query().
		Where(entimportscanshow.HasScanWith(entimportscan.ID(p.ScanID)))
	if p.Classification != "" {
		q = q.Where(entimportscanshow.ClassificationEQ(p.Classification))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count import scan shows: %w", err)
	}
	limit := p.Limit
	if limit == 0 {
		limit = 50
	}
	rows, err := q.Order(ent.Asc(entimportscanshow.FieldParsedTitle)).
		Offset(int(p.Offset)).Limit(int(limit)).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list import scan shows: %w", err)
	}
	return rows, uint32(total), nil //nolint:gosec // count is non-negative
}

func (db *DB) FindImportScanShow(
	ctx context.Context, scanID, showID uint32,
) (*ent.ImportScanShow, error) {
	row, err := db.client.ImportScanShow.Query().
		Where(
			entimportscanshow.ID(showID),
			entimportscanshow.HasScanWith(entimportscan.ID(scanID)),
		).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("find import scan show: %w", err)
	}
	return row, nil
}

func (db *DB) UpdateImportScanShowDecision(
	ctx context.Context, id uint32,
	decision entimportscanshow.Decision, tvdbID *uint32,
) error {
	u := db.client.ImportScanShow.UpdateOneID(id).SetDecision(decision)
	if tvdbID != nil {
		u = u.SetDecisionTvdbID(*tvdbID)
	} else {
		u = u.ClearDecisionTvdbID()
	}
	return u.Exec(ctx)
}

// ListImportScanShowsForCommit returns the shows to adopt when a series scan is
// committed: everything not explicitly skipped that was either accepted by the
// reviewer or auto-matched (confirmed / existing). Mirrors the movie
// ListImportScanFilesForCommit filter.
func (db *DB) ListImportScanShowsForCommit(
	ctx context.Context, scanID uint32,
) ([]*ent.ImportScanShow, error) {
	return db.client.ImportScanShow.Query().
		Where(
			entimportscanshow.HasScanWith(entimportscan.ID(scanID)),
			entimportscanshow.DecisionNEQ(entimportscanshow.DecisionSkip),
			entimportscanshow.Or(
				entimportscanshow.DecisionEQ(entimportscanshow.DecisionAccept),
				entimportscanshow.ClassificationIn(
					entimportscanshow.ClassificationConfirmed,
					entimportscanshow.ClassificationExisting,
				),
			),
		).
		All(ctx)
}

func (db *DB) UpdateImportScanShowOutcome(
	ctx context.Context, id uint32,
	outcome entimportscanshow.Outcome, opts UpdateScanShowOutcomeOpts,
) error {
	u := db.client.ImportScanShow.UpdateOneID(id).SetOutcome(outcome)
	if opts.Message != "" {
		u = u.SetOutcomeMessage(opts.Message)
	}
	if opts.CreatedTvshowID != 0 {
		u = u.SetCreatedTvshowID(opts.CreatedTvshowID)
	}
	return u.Exec(ctx)
}

// ListAllEpisodeMediaFilePaths returns every episode media_file path, for the
// series scanner's tracked-file gate (a show whose files are all tracked is
// skipped).
func (db *DB) ListAllEpisodeMediaFilePaths(ctx context.Context) ([]string, error) {
	rows, err := db.client.MediaFile.Query().
		Where(entmediafile.HasEpisode()).
		Select(entmediafile.FieldPath).
		Strings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list episode media_file paths: %w", err)
	}
	return rows, nil
}
