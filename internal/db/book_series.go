package db

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/bookcontribution"
	"github.com/datahearth/streamline/ent/bookseries"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/media/book/pick"
)

// CreateSeriesParams is a series row with its first volumes. Volumes whose
// RefreshedAt is nil are stubs.
type CreateSeriesParams struct {
	HardcoverID      uint32
	Title            string
	OriginalTitle    string
	Overview         string
	AuthorName       string
	Kind             string
	QualityProfile   string
	RatingTenths     *uint8
	Ongoing          bool
	Since            *uint16
	Monitor          string
	EditionLanguage  string
	EditionPublisher string
	RefreshedAt      time.Time
	Credits          []CreditSeed
	Volumes          []BookSeed
}

// SeriesMetadata is the provider-derived part of a series, written on a
// refresh. Monitor, profile and the chosen edition are never in it.
type SeriesMetadata struct {
	Title         string
	OriginalTitle string
	Overview      string
	AuthorName    string
	// Kind is written only when not empty, as for a book.
	Kind         string
	RatingTenths *uint8
	Ongoing      bool
	Since        *uint16
	Credits      []CreditSeed
	RefreshedAt  time.Time
}

const maxRatingTenths = 50

// VolumeCounts says what happened to the volumes offered to a series.
type VolumeCounts struct {
	Created int
	// Adopted counts volumes already in the library as standalone books.
	Adopted int
	// Skipped counts volumes that belong to another library series.
	Skipped int
}

func addVolumes(
	ctx context.Context,
	c *ent.Client,
	seriesID uint32,
	profile string,
	seeds []BookSeed,
) (VolumeCounts, error) {
	var counts VolumeCounts
	for _, s := range seeds {
		cur, err := c.Book.Query().
			Where(book.HardcoverIDEQ(s.HardcoverID)).
			WithSeries().
			Only(ctx)
		switch {
		case ent.IsNotFound(err):
			if _, err := createBookTx(ctx, c, s, seriesID); err != nil {
				return counts, err
			}
			counts.Created++
		case err != nil:
			return counts, err
		case cur.Edges.Series == nil:
			u := c.Book.UpdateOneID(cur.ID).
				SetSeriesID(seriesID).
				SetNillableSeriesPosition(s.SeriesPosition)
			if profile != "" {
				u = u.SetQualityProfile(profile)
			}
			if err := u.Exec(ctx); err != nil {
				return counts, err
			}
			counts.Adopted++
		case cur.Edges.Series.ID == seriesID:
			if err := c.Book.UpdateOneID(cur.ID).
				SetNillableSeriesPosition(s.SeriesPosition).
				Exec(ctx); err != nil {
				return counts, err
			}
		default:
			counts.Skipped++
		}
	}
	return counts, nil
}

// CreateSeries writes the series, its contributors and its first volumes in
// one transaction. A volume already in the library as a standalone book is
// adopted into the series with its data kept; one in another series is
// skipped.
func (db *DB) CreateSeries(
	ctx context.Context,
	p CreateSeriesParams,
) (*ent.BookSeries, VolumeCounts, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return nil, VolumeCounts{}, err
	}
	c := tx.Client()
	row, err := c.BookSeries.Create().
		SetHardcoverID(p.HardcoverID).
		SetTitle(p.Title).
		SetSortTitle(p.Title).
		SetOriginalTitle(p.OriginalTitle).
		SetOverview(p.Overview).
		SetAuthorName(p.AuthorName).
		SetKind(bookseries.Kind(p.Kind)).
		SetQualityProfile(p.QualityProfile).
		SetNillableRatingTenths(p.RatingTenths).
		SetOngoing(p.Ongoing).
		SetNillableSince(p.Since).
		SetMonitor(bookseries.Monitor(p.Monitor)).
		SetEditionLanguage(p.EditionLanguage).
		SetEditionPublisher(p.EditionPublisher).
		SetLastRefreshedAt(p.RefreshedAt).
		Save(ctx)
	if err != nil {
		tx.Rollback()
		return nil, VolumeCounts{}, fmt.Errorf("create series: %w", err)
	}
	if err := writeContributions(ctx, c, 0, row.ID, p.Credits); err != nil {
		tx.Rollback()
		return nil, VolumeCounts{}, err
	}
	counts, err := addVolumes(ctx, c, row.ID, p.QualityProfile, p.Volumes)
	if err != nil {
		tx.Rollback()
		return nil, VolumeCounts{}, err
	}
	if err := tx.Commit(); err != nil {
		return nil, VolumeCounts{}, err
	}
	out, err := db.FindSeriesByID(ctx, row.ID)
	return out, counts, err
}

// AddSeriesVolumes writes volumes a refresh found, with the same adoption and
// skip rules as CreateSeries.
func (db *DB) AddSeriesVolumes(
	ctx context.Context,
	seriesID uint32,
	profile string,
	seeds []BookSeed,
) (VolumeCounts, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return VolumeCounts{}, err
	}
	counts, err := addVolumes(ctx, tx.Client(), seriesID, profile, seeds)
	if err != nil {
		tx.Rollback()
		return VolumeCounts{}, err
	}
	return counts, tx.Commit()
}

func withSeriesDetail(q *ent.BookSeriesQuery) *ent.BookSeriesQuery {
	return q.
		WithContributions(func(cq *ent.BookContributionQuery) {
			cq.WithAuthor().Order(ent.Asc(bookcontribution.FieldOrder))
		}).
		WithVolumes(func(vq *ent.BookQuery) {
			vq.Order(ent.Asc(book.FieldSeriesPosition), ent.Asc(book.FieldID)).
				WithMediaFiles().
				WithEditions().
				WithEbookEdition().
				WithAudiobookEdition()
		})
}

// FindSeriesByID loads the series with its volumes (ascending position), their
// files and editions, and its contributors.
func (db *DB) FindSeriesByID(
	ctx context.Context,
	id uint32,
) (*ent.BookSeries, error) {
	return withSeriesDetail(
		db.client.BookSeries.Query().Where(bookseries.IDEQ(id)),
	).Only(ctx)
}

// FindSeriesByHardcoverID returns nil, nil when no series has the id.
func (db *DB) FindSeriesByHardcoverID(
	ctx context.Context,
	hardcoverID uint32,
) (*ent.BookSeries, error) {
	row, err := db.client.BookSeries.Query().
		Where(bookseries.HardcoverIDEQ(hardcoverID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return row, err
}

// ListStaleSeries returns at most limit series never refreshed or last
// refreshed before cutoff, oldest first.
func (db *DB) ListStaleSeries(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) ([]*ent.BookSeries, error) {
	return db.client.BookSeries.Query().
		Where(bookseries.Or(
			bookseries.LastRefreshedAtIsNil(),
			bookseries.LastRefreshedAtLT(cutoff),
		)).
		Order(ent.Asc(bookseries.FieldLastRefreshedAt), ent.Asc(bookseries.FieldID)).
		Limit(limit).
		All(ctx)
}

// ApplySeriesMetadata writes provider data onto a series and replaces its
// contributions.
func (db *DB) ApplySeriesMetadata(
	ctx context.Context,
	id uint32,
	m SeriesMetadata,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	c := tx.Client()
	u := c.BookSeries.UpdateOneID(id).
		SetTitle(m.Title).
		SetSortTitle(m.Title).
		SetOriginalTitle(m.OriginalTitle).
		SetOverview(m.Overview).
		SetAuthorName(m.AuthorName).
		SetNillableRatingTenths(m.RatingTenths).
		SetOngoing(m.Ongoing).
		SetNillableSince(m.Since).
		SetLastRefreshedAt(m.RefreshedAt)
	if m.RatingTenths == nil {
		u = u.ClearRatingTenths()
	}
	if m.Since == nil {
		u = u.ClearSince()
	}
	if m.Kind != "" {
		u = u.SetKind(bookseries.Kind(m.Kind))
	}
	if err := u.Exec(ctx); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := c.BookContribution.Delete().
		Where(bookcontribution.HasSeriesWith(bookseries.IDEQ(id))).
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("clear series contributions: %w", err)
	}
	if err := writeContributions(ctx, c, 0, id, m.Credits); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := deleteOrphanAuthors(ctx, c); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// SetSeriesQualityProfile writes the profile on the series and on every one of
// its volumes in one transaction.
func (db *DB) SetSeriesQualityProfile(
	ctx context.Context,
	id uint32,
	profile string,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	c := tx.Client()
	if err := c.BookSeries.UpdateOneID(id).
		SetQualityProfile(profile).
		Exec(ctx); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := c.Book.Update().
		Where(book.HasSeriesWith(bookseries.IDEQ(id))).
		SetQualityProfile(profile).
		Save(ctx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func ebookFile(b *ent.Book) bool {
	for _, f := range b.Edges.MediaFiles {
		if f.BookKind == mediafile.BookKindEbook {
			return true
		}
	}
	return false
}

// setEbookMonitored applies a volume's ebook monitoring: on picks an edition
// when there is none and, on a hydrated volume with no ebook edition at all,
// does nothing, like a book. A stub has no editions yet and is just flagged.
func setEbookMonitored(
	ctx context.Context,
	c *ent.Client,
	v *ent.Book,
	on bool,
) error {
	if on && v.LastRefreshedAt != nil && v.Edges.EbookEdition == nil {
		ed, ok := pick.Slot(
			editionViews(v.Edges.Editions), "ebook", v.PreferredLanguage,
		)
		if !ok {
			return nil
		}
		if err := c.Book.UpdateOneID(v.ID).
			SetEbookEditionID(ed.ID).
			Exec(ctx); err != nil {
			return err
		}
	}
	if err := c.Book.UpdateOneID(v.ID).SetEbookMonitored(on).Exec(ctx); err != nil {
		return err
	}
	return moveSlot(ctx, c, v.ID, string(mediafile.BookKindEbook), on)
}

// SetSeriesMonitor stores the policy and applies it to each volume's ebook
// slot. all monitors every volume; future monitors those releasing after
// since and unmonitors the earlier ones that hold no file; none unmonitors
// every volume without a file. A volume with a file keeps its status.
func (db *DB) SetSeriesMonitor(
	ctx context.Context,
	id uint32,
	monitor string,
	since time.Time,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	c := tx.Client()
	if err := c.BookSeries.UpdateOneID(id).
		SetMonitor(bookseries.Monitor(monitor)).
		Exec(ctx); err != nil {
		tx.Rollback()
		return err
	}
	vols, err := c.Book.Query().
		Where(book.HasSeriesWith(bookseries.IDEQ(id))).
		WithMediaFiles().
		WithEditions().
		WithEbookEdition().
		All(ctx)
	if err != nil {
		tx.Rollback()
		return err
	}
	for _, v := range vols {
		want := pick.MonitorsVolume(monitor, v.ReleaseDate, since)
		if !want && ebookFile(v) {
			continue
		}
		if err := setEbookMonitored(ctx, c, v, want); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// SetSeriesEdition stores the chosen (language, publisher) and re-picks the
// ebook slot of every volume without an ebook file: the volume's stored edition
// of that language and publisher, else its pick for that language, else the
// pointer is left alone. Titles are recomputed in the series language, which
// becomes each such volume's preferred language. Volumes with a file keep their
// file and edition.
func (db *DB) SetSeriesEdition(
	ctx context.Context,
	id uint32,
	language, publisher string,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	c := tx.Client()
	if err := c.BookSeries.UpdateOneID(id).
		SetEditionLanguage(language).
		SetEditionPublisher(publisher).
		Exec(ctx); err != nil {
		tx.Rollback()
		return err
	}
	vols, err := c.Book.Query().
		Where(book.HasSeriesWith(bookseries.IDEQ(id))).
		WithMediaFiles().
		WithEditions().
		All(ctx)
	if err != nil {
		tx.Rollback()
		return err
	}
	for _, v := range vols {
		if ebookFile(v) {
			continue
		}
		u := c.Book.UpdateOneID(v.ID).SetPreferredLanguage(language)
		views := editionViews(v.Edges.Editions)
		if len(views) > 0 {
			ed, ok := pick.SlotIn(views, "ebook", language, publisher)
			if !ok {
				ed, ok = pick.Slot(views, "ebook", language)
			}
			if ok {
				u = u.SetEbookEditionID(ed.ID)
			}
			title, original := pick.Titles(views, language, v.Title)
			u = u.SetTitle(title).SetSortTitle(title).SetOriginalTitle(original)
		}
		if err := u.Exec(ctx); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// DeleteSeries removes the series; volumes, their files and records, and the
// series' contributions cascade in the schema. People left with no
// contribution go in the same transaction; their ids are returned for poster
// cleanup.
func (db *DB) DeleteSeries(ctx context.Context, id uint32) ([]uint32, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	c := tx.Client()
	if err := c.BookSeries.DeleteOneID(id).Exec(ctx); err != nil {
		tx.Rollback()
		return nil, err
	}
	orphans, err := deleteOrphanAuthors(ctx, c)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return orphans, tx.Commit()
}

// MarkSeriesRefreshed stamps the series as read now, for a refresh whose
// metadata did not change.
func (db *DB) MarkSeriesRefreshed(
	ctx context.Context,
	id uint32,
	at time.Time,
) error {
	return db.client.BookSeries.UpdateOneID(id).SetLastRefreshedAt(at).Exec(ctx)
}

// DeleteBooks removes the given books, for the compilations and duplicate
// positions a hydration finds among a series' volumes.
func (db *DB) DeleteBooks(ctx context.Context, ids []uint32) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := db.client.Book.Delete().Where(book.IDIn(ids...)).Exec(ctx)
	return err
}

// RefreshSeriesStats recomputes the stored since (earliest release year over
// the volumes) and rating (mean of the rated volumes, which Hardcover does not
// publish for a series) from the volumes as they now stand.
func (db *DB) RefreshSeriesStats(ctx context.Context, id uint32) error {
	vols, err := db.client.Book.Query().
		Where(book.HasSeriesWith(bookseries.IDEQ(id))).
		Select(book.FieldReleaseYear, book.FieldRatingTenths).
		All(ctx)
	if err != nil {
		return err
	}
	var (
		since        *uint16
		sum, counted int
	)
	for _, v := range vols {
		if v.ReleaseYear != nil && (since == nil || *v.ReleaseYear < *since) {
			y := *v.ReleaseYear
			since = &y
		}
		if v.RatingTenths != nil {
			sum += int(*v.RatingTenths)
			counted++
		}
	}
	u := db.client.BookSeries.UpdateOneID(id)
	if since != nil {
		u = u.SetSince(*since)
	} else {
		u = u.ClearSince()
	}
	if counted > 0 {
		mean := min((sum+counted/2)/counted, maxRatingTenths)
		u = u.SetRatingTenths(saturateU8(mean))
	} else {
		u = u.ClearRatingTenths()
	}
	return u.Exec(ctx)
}

// FirstVolumeID is the lowest-position volume of a series, whose poster is the
// series' cover; 0 for a series with no volume.
func (db *DB) FirstVolumeID(ctx context.Context, seriesID uint32) (uint32, error) {
	id, err := db.client.Book.Query().
		Where(book.HasSeriesWith(bookseries.IDEQ(seriesID))).
		Order(ent.Asc(book.FieldSeriesPosition), ent.Asc(book.FieldID)).
		FirstID(ctx)
	if ent.IsNotFound(err) {
		return 0, nil
	}
	return id, err
}

// saturateU8 narrows to uint8, clamping at both ends.
func saturateU8(v int) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= math.MaxUint8 {
		return math.MaxUint8
	}
	return uint8(v)
}
