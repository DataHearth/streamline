package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanalbum "github.com/datahearth/streamline/ent/importscanalbum"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

// ErrImportScanAlbumNotFound is returned when the scan-scoped UPDATE matched no
// row because the album id is unknown or belongs to a different scan.
var ErrImportScanAlbumNotFound = errors.New("import scan album not found")

type ListImportScanAlbumsParams struct {
	ScanID         uint32
	Classification entimportscanalbum.Classification // empty = all
	Query          string
	Offset, Limit  uint32
}

type CreateImportScanAlbumParams struct {
	FolderPath       string
	TaggedArtist     string
	TaggedAlbum      string
	Classification   entimportscanalbum.Classification
	ReleaseGroupMBID string
	ArtistMBID       string
	Candidates       []schema.ScannedAlbumCandidate
	ExistingAlbumID  *uint32
	FileCount        uint16
	TaggedYear       uint16
	Format           string
	Size             int64
}

type UpdateScanAlbumOutcomeOpts struct {
	Message        string
	CreatedAlbumID uint32
}

// AdoptAlbumFile is one on-disk audio file bound to the track it fills.
type AdoptAlbumFile struct {
	TrackID uint32
	Path    string
	Quality string
	Format  string
	Size    int64
}

func (db *DB) ListPendingImportScanAlbumFolders(
	ctx context.Context,
) ([]string, error) {
	rows, err := db.client.ImportScanAlbum.Query().
		Where(entimportscanalbum.HasScanWith(
			entimportscan.StatusEQ(entimportscan.StatusAwaitingReview),
		)).
		Select(entimportscanalbum.FieldFolderPath).
		Strings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending import_scan_album folders: %w", err)
	}
	return rows, nil
}

func (db *DB) BulkCreateImportScanAlbums(
	ctx context.Context, scanID uint32, albums []CreateImportScanAlbumParams,
) error {
	if len(albums) == 0 {
		return nil
	}
	creates := make([]*ent.ImportScanAlbumCreate, 0, len(albums))
	for _, p := range albums {
		c := db.client.ImportScanAlbum.Create().
			SetScanID(scanID).
			SetFolderPath(p.FolderPath).
			SetFileCount(p.FileCount).
			SetSize(p.Size).
			SetClassification(p.Classification)
		if p.TaggedYear != 0 {
			c.SetTaggedYear(p.TaggedYear)
		}
		if p.Format != "" {
			c.SetFormat(p.Format)
		}
		if p.TaggedArtist != "" {
			c.SetTaggedArtist(p.TaggedArtist)
		}
		if p.TaggedAlbum != "" {
			c.SetTaggedAlbum(p.TaggedAlbum)
		}
		if p.ReleaseGroupMBID != "" {
			c.SetReleaseGroupMbid(p.ReleaseGroupMBID)
		}
		if p.ArtistMBID != "" {
			c.SetArtistMbid(p.ArtistMBID)
		}
		if p.ExistingAlbumID != nil {
			c.SetExistingAlbumID(*p.ExistingAlbumID)
		}
		if len(p.Candidates) > 0 {
			c.SetCandidates(p.Candidates)
		}
		creates = append(creates, c)
	}
	if _, err := db.client.ImportScanAlbum.CreateBulk(creates...).
		Save(ctx); err != nil {
		return fmt.Errorf("bulk create import scan albums: %w", err)
	}
	return nil
}

// ListImportScanAlbumsForCommit mirrors ListImportScanShowsForCommit: not
// skipped, and either accepted by the reviewer or auto-matched.
func (db *DB) ListImportScanAlbumsForCommit(
	ctx context.Context, scanID uint32,
) ([]*ent.ImportScanAlbum, error) {
	return db.client.ImportScanAlbum.Query().
		Where(
			entimportscanalbum.HasScanWith(entimportscan.ID(scanID)),
			entimportscanalbum.DecisionNEQ(entimportscanalbum.DecisionSkip),
			entimportscanalbum.Or(
				entimportscanalbum.DecisionEQ(entimportscanalbum.DecisionAccept),
				entimportscanalbum.ClassificationIn(
					entimportscanalbum.ClassificationConfirmed,
					entimportscanalbum.ClassificationExisting,
				),
			),
		).
		All(ctx)
}

func (db *DB) UpdateImportScanAlbumOutcome(
	ctx context.Context, id uint32,
	outcome entimportscanalbum.Outcome, opts UpdateScanAlbumOutcomeOpts,
) error {
	u := db.client.ImportScanAlbum.UpdateOneID(id).SetOutcome(outcome)
	if opts.Message != "" {
		u = u.SetOutcomeMessage(opts.Message)
	}
	if opts.CreatedAlbumID != 0 {
		u = u.SetCreatedAlbumID(opts.CreatedAlbumID)
	}
	return u.Exec(ctx)
}

func (db *DB) BulkUpdateImportScanAlbumDecisions(
	ctx context.Context,
	scanID uint32,
	decision entimportscanalbum.Decision,
	classification entimportscanalbum.Classification,
	ids []uint32,
) (int, error) {
	u := db.client.ImportScanAlbum.Update().
		Where(entimportscanalbum.HasScanWith(entimportscan.ID(scanID))).
		SetDecision(decision)
	if classification != "" {
		u = u.Where(entimportscanalbum.ClassificationEQ(classification))
	}
	if len(ids) > 0 {
		u = u.Where(entimportscanalbum.IDIn(ids...))
	}
	n, err := u.Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("bulk update import scan album decisions: %w", err)
	}
	return n, nil
}

// AlbumMBIDIndex maps release-group mbid to album id, so the scanner can flag
// a folder as an album the library already holds.
func (db *DB) AlbumMBIDIndex(ctx context.Context) (map[string]uint32, error) {
	rows, err := db.client.Album.Query().
		Select(album.FieldID, album.FieldMbid).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("album mbid index: %w", err)
	}
	idx := make(map[string]uint32, len(rows))
	for _, a := range rows {
		idx[a.Mbid] = a.ID
	}
	return idx, nil
}

func (db *DB) IsAlbumMonitoredByMBID(
	ctx context.Context, mbid string,
) (bool, error) {
	return db.client.Album.Query().
		Where(album.MbidEQ(mbid), album.Monitored(true)).
		Exist(ctx)
}

// FindAlbumByMBID returns nil, nil on a miss.
func (db *DB) FindAlbumByMBID(
	ctx context.Context, mbid string,
) (*ent.Album, error) {
	row, err := db.client.Album.Query().
		Where(album.MbidEQ(mbid)).
		WithTracks(func(tq *ent.TrackQuery) {
			tq.Order(ent.Asc(track.FieldDisc), ent.Asc(track.FieldPosition)).
				WithMediaFiles()
		}).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return row, err
}

// AdoptAlbumFiles records pre-existing audio files against an album's tracks in
// one transaction: a failure leaves nothing behind. The album turns available
// only when every one of its tracks holds a file afterwards; a partial adoption
// leaves the status alone so the missing tracks stay wanted.
func (db *DB) AdoptAlbumFiles(
	ctx context.Context, albumID uint32, files []AdoptAlbumFile,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin adopt album files: %w", err)
	}
	if err := adoptAlbumFiles(ctx, tx.Client(), albumID, files); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit adopt album files: %w", err)
	}
	return nil
}

func adoptAlbumFiles(
	ctx context.Context, c *ent.Client, albumID uint32, files []AdoptAlbumFile,
) error {
	if err := c.Album.UpdateOneID(albumID).SetMonitored(true).Exec(ctx); err != nil {
		return fmt.Errorf("monitor album %d: %w", albumID, err)
	}
	for _, f := range files {
		if err := c.MediaFile.Create().
			SetPath(f.Path).
			SetSize(f.Size).
			SetQuality(f.Quality).
			SetFormat(f.Format).
			SetSource(entmediafile.SourceOrphan).
			SetTrackID(f.TrackID).
			Exec(ctx); err != nil {
			return fmt.Errorf("create media file %s: %w", f.Path, err)
		}
	}
	bare, err := c.Track.Query().
		Where(
			track.HasAlbumWith(album.ID(albumID)),
			track.Not(track.HasMediaFiles()),
		).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check album %d coverage: %w", albumID, err)
	}
	if bare {
		return nil
	}
	if _, err := c.Album.Update().
		Where(
			album.ID(albumID),
			album.StatusNEQ(album.StatusDownloading),
		).
		SetStatus(album.StatusAvailable).Save(ctx); err != nil {
		return fmt.Errorf("mark album %d available: %w", albumID, err)
	}
	return nil
}

func (db *DB) ListImportScanAlbums(
	ctx context.Context, p ListImportScanAlbumsParams,
) ([]*ent.ImportScanAlbum, uint32, error) {
	q := db.client.ImportScanAlbum.Query().
		Where(entimportscanalbum.HasScanWith(entimportscan.ID(p.ScanID)))
	if p.Classification != "" {
		q = q.Where(entimportscanalbum.ClassificationEQ(p.Classification))
	}
	if p.Query != "" {
		q = q.Where(entimportscanalbum.Or(
			entimportscanalbum.FolderPathContainsFold(p.Query),
			entimportscanalbum.TaggedAlbumContainsFold(p.Query),
			entimportscanalbum.TaggedArtistContainsFold(p.Query),
		))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count import scan albums: %w", err)
	}
	limit := p.Limit
	if limit == 0 {
		limit = 50
	}
	rows, err := q.Order(
		ent.Asc(entimportscanalbum.FieldTaggedAlbum),
		ent.Asc(entimportscanalbum.FieldID),
	).Offset(int(p.Offset)).Limit(int(limit)).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list import scan albums: %w", err)
	}
	return rows, numeric.SaturateU32(total), nil
}

func (db *DB) FindImportScanAlbum(
	ctx context.Context, scanID, albumID uint32,
) (*ent.ImportScanAlbum, error) {
	row, err := db.client.ImportScanAlbum.Query().
		Where(
			entimportscanalbum.ID(albumID),
			entimportscanalbum.HasScanWith(entimportscan.ID(scanID)),
		).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("find import scan album: %w", err)
	}
	return row, nil
}

// UpdateImportScanAlbumDecision scopes by scan in the UPDATE predicate so an
// album id from another scan matches nothing instead of being mutated.
func (db *DB) UpdateImportScanAlbumDecision(
	ctx context.Context, scanID, albumID uint32,
	decision entimportscanalbum.Decision, releaseGroupMBID *string,
) error {
	u := db.client.ImportScanAlbum.Update().
		Where(
			entimportscanalbum.ID(albumID),
			entimportscanalbum.HasScanWith(entimportscan.ID(scanID)),
		).
		SetDecision(decision)
	if releaseGroupMBID != nil {
		u = u.SetDecisionReleaseGroupMbid(*releaseGroupMBID)
	} else {
		u = u.ClearDecisionReleaseGroupMbid()
	}
	n, err := u.Save(ctx)
	if err != nil {
		return fmt.Errorf("update import scan album decision: %w", err)
	}
	if n == 0 {
		return ErrImportScanAlbumNotFound
	}
	return nil
}
