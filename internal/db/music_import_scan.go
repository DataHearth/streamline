package db

import (
	"context"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanalbum "github.com/datahearth/streamline/ent/importscanalbum"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/ent/track"
)

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
			SetClassification(p.Classification)
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
