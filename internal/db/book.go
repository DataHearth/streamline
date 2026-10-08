package db

import (
	"context"
	"fmt"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/author"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/mediafile"
)

type BookSeed struct {
	HardcoverID        uint32
	Title              string
	SortTitle          string
	ReleaseDate        *time.Time
	SeriesName         string
	SeriesPosition     string
	EbookMonitored     bool
	AudiobookMonitored bool
}

type CreateAuthorParams struct {
	HardcoverID             uint32
	Name                    string
	SortName                string
	Overview                string
	Monitored               bool
	Folder                  string
	MonitorPolicy           string
	WantKinds               string
	EbookQualityProfile     string
	AudiobookQualityProfile string
	Books                   []BookSeed
}

// RefreshAuthorParams carries provider-sourced fields. Books already stored
// keep their slot flags and statuses; only books new to the author are
// created, with the slot flags their seed carries.
type RefreshAuthorParams struct {
	Name        string
	SortName    string
	Overview    string
	Books       []BookSeed
	RefreshedAt time.Time
}

// UpdateAuthorParams applies only the non-nil fields.
type UpdateAuthorParams struct {
	Monitored               *bool
	MonitorPolicy           *string
	WantKinds               *string
	EbookQualityProfile     *string
	AudiobookQualityProfile *string
}

func createBook(
	ctx context.Context,
	c *ent.Client,
	authorID uint32,
	b BookSeed,
) error {
	ebook, audiobook := book.EbookStatusSkipped, book.AudiobookStatusSkipped
	if b.EbookMonitored {
		ebook = book.EbookStatusWanted
	}
	if b.AudiobookMonitored {
		audiobook = book.AudiobookStatusWanted
	}
	return c.Book.Create().
		SetHardcoverID(b.HardcoverID).
		SetTitle(b.Title).
		SetSortTitle(b.SortTitle).
		SetNillableReleaseDate(b.ReleaseDate).
		SetSeriesName(b.SeriesName).
		SetSeriesPosition(b.SeriesPosition).
		SetEbookMonitored(b.EbookMonitored).
		SetEbookStatus(ebook).
		SetAudiobookMonitored(b.AudiobookMonitored).
		SetAudiobookStatus(audiobook).
		SetAuthorID(authorID).
		Exec(ctx)
}

func (db *DB) CreateAuthor(
	ctx context.Context,
	p CreateAuthorParams,
) (*ent.Author, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	row, err := tx.Author.Create().
		SetHardcoverID(p.HardcoverID).
		SetName(p.Name).
		SetSortName(p.SortName).
		SetOverview(p.Overview).
		SetMonitored(p.Monitored).
		SetFolder(p.Folder).
		SetMonitorPolicy(author.MonitorPolicy(p.MonitorPolicy)).
		SetWantKinds(author.WantKinds(p.WantKinds)).
		SetEbookQualityProfile(p.EbookQualityProfile).
		SetAudiobookQualityProfile(p.AudiobookQualityProfile).
		Save(ctx)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	for _, b := range p.Books {
		if err := createBook(ctx, tx.Client(), row.ID, b); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.FindAuthorByID(ctx, row.ID)
}

func (db *DB) FindAuthorByID(ctx context.Context, id uint32) (*ent.Author, error) {
	return db.client.Author.Query().
		Where(author.IDEQ(id)).
		WithBooks(func(q *ent.BookQuery) {
			q.Order(ent.Asc(book.FieldReleaseDate), ent.Asc(book.FieldTitle)).
				WithMediaFiles()
		}).
		Only(ctx)
}

func (db *DB) FindBookByID(ctx context.Context, id uint32) (*ent.Book, error) {
	return db.client.Book.Query().
		Where(book.IDEQ(id)).
		WithAuthor().
		WithMediaFiles().
		Only(ctx)
}

func (db *DB) FindAuthorByHardcoverID(
	ctx context.Context,
	hardcoverID uint32,
) (*ent.Author, error) {
	row, err := db.client.Author.Query().
		Where(author.HardcoverIDEQ(hardcoverID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return row, err
}

func (db *DB) CountAuthors(ctx context.Context) (int, error) {
	return db.client.Author.Query().Count(ctx)
}

func (db *DB) ListAuthors(
	ctx context.Context,
	offset, limit uint32,
) ([]*ent.Author, error) {
	return db.client.Author.Query().
		Order(ent.Asc(author.FieldSortName), ent.Asc(author.FieldName)).
		Offset(int(offset)).Limit(int(limit)).
		WithBooks().
		All(ctx)
}

func (db *DB) RefreshAuthor(
	ctx context.Context,
	id uint32,
	p RefreshAuthorParams,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	c := tx.Client()
	if _, err := c.Author.Query().Where(author.IDEQ(id)).Only(ctx); err != nil {
		tx.Rollback()
		return err
	}
	if err := c.Author.UpdateOneID(id).
		SetName(p.Name).
		SetSortName(p.SortName).
		SetOverview(p.Overview).
		SetLastRefreshedAt(p.RefreshedAt).
		Exec(ctx); err != nil {
		tx.Rollback()
		return err
	}
	existing, err := c.Book.Query().
		Where(book.HasAuthorWith(author.IDEQ(id))).
		All(ctx)
	if err != nil {
		tx.Rollback()
		return err
	}
	byHC := make(map[uint32]*ent.Book, len(existing))
	for _, b := range existing {
		byHC[b.HardcoverID] = b
	}
	for _, b := range p.Books {
		if cur := byHC[b.HardcoverID]; cur != nil {
			if err := c.Book.UpdateOne(cur).
				SetTitle(b.Title).
				SetSortTitle(b.SortTitle).
				SetNillableReleaseDate(b.ReleaseDate).
				SetSeriesName(b.SeriesName).
				SetSeriesPosition(b.SeriesPosition).
				Exec(ctx); err != nil {
				tx.Rollback()
				return err
			}
			continue
		}
		if err := createBook(ctx, c, id, b); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// SetAuthorMonitored does not cascade: a slot is searched only while the
// author flag, the slot flag and the absence of a file all hold.
func (db *DB) SetAuthorMonitored(
	ctx context.Context,
	id uint32,
	monitored bool,
) error {
	return db.client.Author.UpdateOneID(id).SetMonitored(monitored).Exec(ctx)
}

func (db *DB) UpdateAuthor(
	ctx context.Context,
	id uint32,
	p UpdateAuthorParams,
) error {
	u := db.client.Author.UpdateOneID(id)
	if p.Monitored != nil {
		u = u.SetMonitored(*p.Monitored)
	}
	if p.MonitorPolicy != nil {
		u = u.SetMonitorPolicy(author.MonitorPolicy(*p.MonitorPolicy))
	}
	if p.WantKinds != nil {
		u = u.SetWantKinds(author.WantKinds(*p.WantKinds))
	}
	if p.EbookQualityProfile != nil {
		u = u.SetEbookQualityProfile(*p.EbookQualityProfile)
	}
	if p.AudiobookQualityProfile != nil {
		u = u.SetAudiobookQualityProfile(*p.AudiobookQualityProfile)
	}
	return u.Exec(ctx)
}

func (db *DB) SetBookSlotStatus(
	ctx context.Context,
	id uint32,
	kind string,
	from, to string,
) error {
	u := db.client.Book.Update().Where(book.IDEQ(id))
	switch kind {
	case string(mediafile.BookKindEbook):
		u = u.Where(book.EbookStatusEQ(book.EbookStatus(from))).
			SetEbookStatus(book.EbookStatus(to))
	case string(mediafile.BookKindAudiobook):
		u = u.Where(book.AudiobookStatusEQ(book.AudiobookStatus(from))).
			SetAudiobookStatus(book.AudiobookStatus(to))
	default:
		return fmt.Errorf("unknown book slot kind %q", kind)
	}
	return u.Exec(ctx)
}

func (db *DB) SetBookSlot(
	ctx context.Context,
	id uint32,
	kind string,
	monitored bool,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	row, err := tx.Book.Query().
		Where(book.IDEQ(id)).
		WithMediaFiles().
		Only(ctx)
	if err != nil {
		tx.Rollback()
		return err
	}
	hasFile := false
	for _, f := range row.Edges.MediaFiles {
		if string(f.BookKind) == kind {
			hasFile = true
			break
		}
	}
	u := tx.Book.UpdateOne(row)
	switch kind {
	case string(mediafile.BookKindEbook):
		u = u.SetEbookMonitored(monitored)
		switch {
		case hasFile:
		case monitored && row.EbookStatus == book.EbookStatusSkipped:
			u = u.SetEbookStatus(book.EbookStatusWanted)
		case !monitored && row.EbookStatus == book.EbookStatusWanted:
			u = u.SetEbookStatus(book.EbookStatusSkipped)
		}
	case string(mediafile.BookKindAudiobook):
		u = u.SetAudiobookMonitored(monitored)
		switch {
		case hasFile:
		case monitored && row.AudiobookStatus == book.AudiobookStatusSkipped:
			u = u.SetAudiobookStatus(book.AudiobookStatusWanted)
		case !monitored && row.AudiobookStatus == book.AudiobookStatusWanted:
			u = u.SetAudiobookStatus(book.AudiobookStatusSkipped)
		}
	}
	if err := u.Exec(ctx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (db *DB) DeleteAuthor(ctx context.Context, id uint32) error {
	return db.client.Author.DeleteOneID(id).Exec(ctx)
}

func (db *DB) ListUpcomingBooks(
	ctx context.Context,
	from, to time.Time,
) ([]*ent.Book, error) {
	return db.client.Book.Query().
		Where(
			book.Or(
				book.EbookMonitored(true),
				book.AudiobookMonitored(true),
			),
			book.ReleaseDateGTE(from),
			book.ReleaseDateLT(to),
		).
		WithAuthor().
		Order(ent.Asc(book.FieldReleaseDate)).
		All(ctx)
}
