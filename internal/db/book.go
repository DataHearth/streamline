package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/author"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/bookcontribution"
	"github.com/datahearth/streamline/ent/bookedition"
	"github.com/datahearth/streamline/ent/bookseries"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/internal/media/book/pick"
)

// EditionSeed is one stored edition of a book.
type EditionSeed struct {
	HardcoverID     uint32
	Language        string
	Title           string
	Publisher       string
	Year            uint16
	Format          string
	Original        bool
	Pages           uint16
	DurationSeconds uint32
	Narrator        string
	Translator      string
	ISBN13          string
	ASIN            string
	Popularity      uint32
}

// CreditSeed is one contribution of a person to a book or a series.
type CreditSeed struct {
	AuthorHardcoverID uint32
	Name              string
	ImageURL          string
	Role              string
	Language          string
	Order             uint8
}

// BookSeed is everything needed to create a book row. RefreshedAt nil makes
// the row a series stub: no editions, never searched, grabbed or imported
// until the hydration worker fills it in.
type BookSeed struct {
	HardcoverID       uint32
	Title             string
	OriginalTitle     string
	AuthorName        string
	Kind              string
	Genre             string
	Overview          string
	RatingTenths      *uint8
	ReleaseYear       *uint16
	ReleaseDate       *time.Time
	PreferredLanguage string
	QualityProfile    string
	SeriesPosition    *float64
	// EbookMonitored and AudiobookMonitored are the slots asked for. On a
	// hydrated book a slot with no edition of its format stays unmonitored.
	EbookMonitored     bool
	AudiobookMonitored bool
	Editions           []EditionSeed
	Credits            []CreditSeed
	RefreshedAt        *time.Time
}

// BookMetadata is the provider-derived part of a book, written on a refresh or
// a hydration. User state (monitoring, profile, preferred language, statuses)
// is never in it.
type BookMetadata struct {
	// Title is Hardcover's own, the fallback when no edition names one.
	Title      string
	AuthorName string
	// Kind is written only when not empty: a refresh leaves a corrected kind
	// alone, a hydration sets it.
	Kind         string
	Genre        string
	Overview     string
	RatingTenths *uint8
	ReleaseYear  *uint16
	ReleaseDate  *time.Time
	Editions     []EditionSeed
	Credits      []CreditSeed
	RefreshedAt  time.Time
}

func sortName(name string) string { return name }

func upsertAuthors(
	ctx context.Context,
	c *ent.Client,
	credits []CreditSeed,
) (map[uint32]*ent.Author, error) {
	out := make(map[uint32]*ent.Author, len(credits))
	for _, cr := range credits {
		if _, ok := out[cr.AuthorHardcoverID]; ok {
			continue
		}
		a, err := c.Author.Query().
			Where(author.HardcoverIDEQ(cr.AuthorHardcoverID)).
			Only(ctx)
		switch {
		case ent.IsNotFound(err):
			a, err = c.Author.Create().
				SetHardcoverID(cr.AuthorHardcoverID).
				SetName(cr.Name).
				SetSortName(sortName(cr.Name)).
				SetImageSource(cr.ImageURL).
				Save(ctx)
		case err == nil && cr.ImageURL != "" && a.ImageSource != cr.ImageURL:
			a, err = c.Author.UpdateOne(a).SetImageSource(cr.ImageURL).Save(ctx)
		}
		if err != nil {
			return nil, fmt.Errorf("upsert author %d: %w", cr.AuthorHardcoverID, err)
		}
		out[cr.AuthorHardcoverID] = a
	}
	return out, nil
}

func writeEdition(
	ctx context.Context,
	c *ent.Client,
	bookID uint32,
	e EditionSeed,
) (*ent.BookEdition, error) {
	cr := c.BookEdition.Create().
		SetBookID(bookID).
		SetHardcoverEditionID(e.HardcoverID).
		SetLanguage(e.Language).
		SetTitle(e.Title).
		SetPublisher(e.Publisher).
		SetYear(e.Year).
		SetFormat(bookedition.Format(e.Format)).
		SetOriginal(e.Original).
		SetNarrator(e.Narrator).
		SetTranslator(e.Translator).
		SetIsbn13(e.ISBN13).
		SetAsin(e.ASIN).
		SetPopularity(e.Popularity)
	if e.Pages > 0 {
		cr = cr.SetPages(e.Pages)
	}
	if e.DurationSeconds > 0 {
		cr = cr.SetDurationSeconds(e.DurationSeconds)
	}
	return cr.Save(ctx)
}

func writeContributions(
	ctx context.Context,
	c *ent.Client,
	bookID, seriesID uint32,
	credits []CreditSeed,
) error {
	authors, err := upsertAuthors(ctx, c, credits)
	if err != nil {
		return err
	}
	for _, cr := range credits {
		cc := c.BookContribution.Create().
			SetAuthorID(authors[cr.AuthorHardcoverID].ID).
			SetRole(bookcontribution.Role(cr.Role)).
			SetLanguage(cr.Language).
			SetOrder(cr.Order)
		switch {
		case bookID != 0:
			cc = cc.SetBookID(bookID)
		case seriesID != 0:
			cc = cc.SetSeriesID(seriesID)
		default:
			return errors.New("contribution without an owner")
		}
		if err := cc.Exec(ctx); err != nil {
			return fmt.Errorf("create contribution: %w", err)
		}
	}
	return nil
}

func editionViews(rows []*ent.BookEdition) []pick.Edition {
	out := make([]pick.Edition, 0, len(rows))
	for _, e := range rows {
		out = append(out, pick.Edition{
			ID:         e.ID,
			Language:   e.Language,
			Title:      e.Title,
			Publisher:  e.Publisher,
			Format:     string(e.Format),
			Original:   e.Original,
			Popularity: e.Popularity,
		})
	}
	return out
}

func createBookTx(
	ctx context.Context,
	c *ent.Client,
	b BookSeed,
	seriesID uint32,
) (*ent.Book, error) {
	ebookStatus, audiobookStatus := book.EbookStatusSkipped, book.AudiobookStatusSkipped
	if b.EbookMonitored {
		ebookStatus = book.EbookStatusWanted
	}
	if b.AudiobookMonitored {
		audiobookStatus = book.AudiobookStatusWanted
	}
	cr := c.Book.Create().
		SetHardcoverID(b.HardcoverID).
		SetTitle(b.Title).
		SetSortTitle(b.Title).
		SetOriginalTitle(b.OriginalTitle).
		SetAuthorName(b.AuthorName).
		SetKind(book.Kind(b.Kind)).
		SetGenre(b.Genre).
		SetOverview(b.Overview).
		SetPreferredLanguage(b.PreferredLanguage).
		SetQualityProfile(b.QualityProfile).
		SetNillableRatingTenths(b.RatingTenths).
		SetNillableReleaseYear(b.ReleaseYear).
		SetNillableReleaseDate(b.ReleaseDate).
		SetNillableSeriesPosition(b.SeriesPosition).
		SetNillableLastRefreshedAt(b.RefreshedAt).
		SetEbookMonitored(b.EbookMonitored).
		SetEbookStatus(ebookStatus).
		SetAudiobookMonitored(b.AudiobookMonitored).
		SetAudiobookStatus(audiobookStatus)
	if seriesID != 0 {
		cr = cr.SetSeriesID(seriesID)
	}
	row, err := cr.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create book %d: %w", b.HardcoverID, err)
	}
	editions := make([]*ent.BookEdition, 0, len(b.Editions))
	for _, e := range b.Editions {
		ed, err := writeEdition(ctx, c, row.ID, e)
		if err != nil {
			return nil, fmt.Errorf("create edition %d: %w", e.HardcoverID, err)
		}
		editions = append(editions, ed)
	}
	if b.RefreshedAt != nil {
		if err := settleSlots(ctx, c, row, editions, b.Title, true); err != nil {
			return nil, err
		}
	}
	if err := writeContributions(ctx, c, row.ID, 0, b.Credits); err != nil {
		return nil, err
	}
	return row, nil
}

// settleSlots derives a hydrated book's titles from its editions and points
// each slot at its edition. A refresh fills an empty slot and moves a slot
// holding no file to the picked edition when that one is in another language,
// so a preferred-language edition that appears later is taken; a choice of
// publisher within the language, and any slot holding a file, are left alone. A requested slot whose format has no edition is unmonitored, the
// patch of a book with nothing to download in that format.
func settleSlots(
	ctx context.Context,
	c *ent.Client,
	row *ent.Book,
	editions []*ent.BookEdition,
	fallbackTitle string,
	requested bool,
) error {
	views := editionViews(editions)
	title, original := pick.Titles(views, row.PreferredLanguage, fallbackTitle)
	u := c.Book.UpdateOneID(row.ID).
		SetTitle(title).
		SetSortTitle(title).
		SetOriginalTitle(original)

	pointed, err := c.Book.Query().
		Where(book.IDEQ(row.ID)).
		WithEbookEdition().
		WithAudiobookEdition().
		WithSeries().
		Only(ctx)
	if err != nil {
		return err
	}
	moving := !requested && pointed.Edges.Series == nil
	if ed, ok := pick.Slot(views, "ebook", row.PreferredLanguage); ok {
		cur := pointed.Edges.EbookEdition
		move, err := repoint(
			ctx,
			c,
			row.ID,
			mediafile.BookKindEbook,
			cur,
			ed,
			moving,
		)
		if err != nil {
			return err
		}
		if move {
			u = u.SetEbookEditionID(ed.ID)
		}
	} else if requested && row.EbookMonitored {
		u = u.SetEbookMonitored(false).SetEbookStatus(book.EbookStatusSkipped)
	}
	if ed, ok := pick.Slot(views, "audiobook", row.PreferredLanguage); ok {
		cur := pointed.Edges.AudiobookEdition
		move, err := repoint(
			ctx,
			c,
			row.ID,
			mediafile.BookKindAudiobook,
			cur,
			ed,
			moving,
		)
		if err != nil {
			return err
		}
		if move {
			u = u.SetAudiobookEditionID(ed.ID)
		}
	} else if requested && row.AudiobookMonitored {
		u = u.SetAudiobookMonitored(false).
			SetAudiobookStatus(book.AudiobookStatusSkipped)
	}
	return u.Exec(ctx)
}

// repoint reports whether a slot should move to the picked edition: it is
// empty, or moving is set (a refresh of a standalone book) and the slot sits on
// another language's edition while holding no file. A series volume follows
// its series' edition choice instead.
func repoint(
	ctx context.Context,
	c *ent.Client,
	bookID uint32,
	kind mediafile.BookKind,
	cur *ent.BookEdition,
	picked pick.Edition,
	moving bool,
) (bool, error) {
	if cur == nil {
		return true, nil
	}
	if !moving || cur.Language == picked.Language || cur.ID == picked.ID {
		return false, nil
	}
	held, err := c.MediaFile.Query().
		Where(
			mediafile.HasBookWith(book.IDEQ(bookID)),
			mediafile.BookKindEQ(kind),
		).
		Exist(ctx)
	return !held, err
}

// CreateBook writes a standalone book with its editions, contributions and
// people in one transaction and returns it loaded.
func (db *DB) CreateBook(ctx context.Context, b BookSeed) (*ent.Book, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	row, err := createBookTx(ctx, tx.Client(), b, 0)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.FindBookByID(ctx, row.ID)
}

func withBookDetail(q *ent.BookQuery) *ent.BookQuery {
	return q.
		WithMediaFiles().
		WithEditions().
		WithEbookEdition().
		WithAudiobookEdition().
		WithSeries().
		WithContributions(func(cq *ent.BookContributionQuery) {
			cq.WithAuthor().Order(ent.Asc(bookcontribution.FieldOrder))
		})
}

// FindBookByID loads the book with its files, editions, slot editions, series
// and contributors.
func (db *DB) FindBookByID(ctx context.Context, id uint32) (*ent.Book, error) {
	return withBookDetail(db.client.Book.Query().Where(book.IDEQ(id))).Only(ctx)
}

// FindBookByHardcoverID returns nil, nil when no book has the id.
func (db *DB) FindBookByHardcoverID(
	ctx context.Context,
	hardcoverID uint32,
) (*ent.Book, error) {
	row, err := db.client.Book.Query().
		Where(book.HardcoverIDEQ(hardcoverID)).
		WithSeries().
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return row, err
}

// FindBooksByHardcoverIDs maps Hardcover ids to the books already in the
// library, with their series edge loaded.
func (db *DB) FindBooksByHardcoverIDs(
	ctx context.Context,
	ids []uint32,
) (map[uint32]*ent.Book, error) {
	rows, err := db.client.Book.Query().
		Where(book.HardcoverIDIn(ids...)).
		WithSeries().
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[uint32]*ent.Book, len(rows))
	for _, r := range rows {
		out[r.HardcoverID] = r
	}
	return out, nil
}

// ApplyBookMetadata writes provider data onto a book: its fields, its editions
// (upserted by Hardcover id, rows no longer offered pruned unless a slot
// points at them), its contributions, then its titles and any slot still
// without an edition. User state is never touched.
func (db *DB) ApplyBookMetadata(
	ctx context.Context,
	id uint32,
	m BookMetadata,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	c := tx.Client()
	cur, err := c.Book.Query().
		Where(book.IDEQ(id)).
		WithEbookEdition().
		WithAudiobookEdition().
		Only(ctx)
	if err != nil {
		tx.Rollback()
		return err
	}

	u := c.Book.UpdateOneID(id).
		SetAuthorName(m.AuthorName).
		SetGenre(m.Genre).
		SetOverview(m.Overview).
		SetNillableRatingTenths(m.RatingTenths).
		SetNillableReleaseYear(m.ReleaseYear).
		SetNillableReleaseDate(m.ReleaseDate).
		SetLastRefreshedAt(m.RefreshedAt)
	if m.RatingTenths == nil {
		u = u.ClearRatingTenths()
	}
	if m.ReleaseYear == nil {
		u = u.ClearReleaseYear()
	}
	if m.ReleaseDate == nil {
		u = u.ClearReleaseDate()
	}
	if m.Kind != "" {
		u = u.SetKind(book.Kind(m.Kind))
	}
	if err := u.Exec(ctx); err != nil {
		tx.Rollback()
		return err
	}

	stored, err := c.BookEdition.Query().
		Where(bookedition.HasBookWith(book.IDEQ(id))).
		All(ctx)
	if err != nil {
		tx.Rollback()
		return err
	}
	byHC := make(map[uint32]*ent.BookEdition, len(stored))
	for _, e := range stored {
		byHC[e.HardcoverEditionID] = e
	}
	offered := make(map[uint32]bool, len(m.Editions))
	for _, e := range m.Editions {
		offered[e.HardcoverID] = true
		if row, ok := byHC[e.HardcoverID]; ok {
			if err := updateEdition(ctx, c, row.ID, e); err != nil {
				tx.Rollback()
				return err
			}
			continue
		}
		if _, err := writeEdition(ctx, c, id, e); err != nil {
			tx.Rollback()
			return fmt.Errorf("create edition %d: %w", e.HardcoverID, err)
		}
	}
	pinned := map[uint32]bool{}
	if cur.Edges.EbookEdition != nil {
		pinned[cur.Edges.EbookEdition.ID] = true
	}
	if cur.Edges.AudiobookEdition != nil {
		pinned[cur.Edges.AudiobookEdition.ID] = true
	}
	var prune []uint32
	for _, e := range stored {
		if !offered[e.HardcoverEditionID] && !pinned[e.ID] {
			prune = append(prune, e.ID)
		}
	}
	if len(prune) > 0 {
		if _, err := c.BookEdition.Delete().
			Where(bookedition.IDIn(prune...)).
			Exec(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("prune editions: %w", err)
		}
	}

	if _, err := c.BookContribution.Delete().
		Where(bookcontribution.HasBookWith(book.IDEQ(id))).
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("clear contributions: %w", err)
	}
	if err := writeContributions(ctx, c, id, 0, m.Credits); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := deleteOrphanAuthors(ctx, c); err != nil {
		tx.Rollback()
		return err
	}

	rows, err := c.BookEdition.Query().
		Where(bookedition.HasBookWith(book.IDEQ(id))).
		All(ctx)
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := settleSlots(ctx, c, cur, rows, m.Title, false); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func updateEdition(
	ctx context.Context,
	c *ent.Client,
	id uint32,
	e EditionSeed,
) error {
	u := c.BookEdition.UpdateOneID(id).
		SetLanguage(e.Language).
		SetTitle(e.Title).
		SetPublisher(e.Publisher).
		SetYear(e.Year).
		SetFormat(bookedition.Format(e.Format)).
		SetOriginal(e.Original).
		SetNarrator(e.Narrator).
		SetTranslator(e.Translator).
		SetIsbn13(e.ISBN13).
		SetAsin(e.ASIN).
		SetPopularity(e.Popularity)
	if e.Pages > 0 {
		u = u.SetPages(e.Pages)
	} else {
		u = u.ClearPages()
	}
	if e.DurationSeconds > 0 {
		u = u.SetDurationSeconds(e.DurationSeconds)
	} else {
		u = u.ClearDurationSeconds()
	}
	return u.Exec(ctx)
}

// deleteOrphanAuthors removes people left with no contribution so the facet and
// OPDS do not list ghosts, returning their ids for poster cleanup.
func deleteOrphanAuthors(ctx context.Context, c *ent.Client) ([]uint32, error) {
	orphans, err := c.Author.Query().
		Where(author.Not(author.HasContributions())).
		IDs(ctx)
	if err != nil || len(orphans) == 0 {
		return nil, err
	}
	if _, err := c.Author.Delete().
		Where(author.IDIn(orphans...)).
		Exec(ctx); err != nil {
		return nil, err
	}
	return orphans, nil
}

// SetBookSlotEdition points a slot at an edition of the book; edition 0 clears
// it. The edition must belong to the book and fill that format.
func (db *DB) SetBookSlotEdition(
	ctx context.Context,
	id uint32,
	format string,
	edition uint32,
) error {
	u := db.client.Book.UpdateOneID(id)
	if edition != 0 {
		ok, err := db.client.BookEdition.Query().
			Where(
				bookedition.IDEQ(edition),
				bookedition.FormatEQ(bookedition.Format(format)),
				bookedition.HasBookWith(book.IDEQ(id)),
			).
			Exist(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf(
				"edition %d is not an %s edition of book %d",
				edition,
				format,
				id,
			)
		}
	}
	switch format {
	case string(bookedition.FormatEbook):
		if edition == 0 {
			u = u.ClearEbookEdition()
		} else {
			u = u.SetEbookEditionID(edition)
		}
	case string(bookedition.FormatAudiobook):
		if edition == 0 {
			u = u.ClearAudiobookEdition()
		} else {
			u = u.SetAudiobookEditionID(edition)
		}
	default:
		return fmt.Errorf("unknown book slot kind %q", format)
	}
	return u.Exec(ctx)
}

// SetBookTitles writes the display title and the original title.
func (db *DB) SetBookTitles(
	ctx context.Context,
	id uint32,
	title, original string,
) error {
	return db.client.Book.UpdateOneID(id).
		SetTitle(title).
		SetSortTitle(title).
		SetOriginalTitle(original).
		Exec(ctx)
}

func (db *DB) SetBookPreferredLanguage(
	ctx context.Context,
	id uint32,
	language string,
) error {
	return db.client.Book.UpdateOneID(id).SetPreferredLanguage(language).Exec(ctx)
}

func (db *DB) SetBookQualityProfile(
	ctx context.Context,
	id uint32,
	profile string,
) error {
	return db.client.Book.UpdateOneID(id).SetQualityProfile(profile).Exec(ctx)
}

func (db *DB) SetBookKind(ctx context.Context, id uint32, kind string) error {
	return db.client.Book.UpdateOneID(id).SetKind(book.Kind(kind)).Exec(ctx)
}

// SetBookReplacing records the language of the edition a slot's file is being
// replaced from; an empty language clears it.
func (db *DB) SetBookReplacing(
	ctx context.Context,
	id uint32,
	kind, language string,
) error {
	u := db.client.Book.UpdateOneID(id)
	switch kind {
	case string(mediafile.BookKindEbook):
		u = u.SetEbookReplacingLanguage(language)
	case string(mediafile.BookKindAudiobook):
		u = u.SetAudiobookReplacingLanguage(language)
	default:
		return fmt.Errorf("unknown book slot kind %q", kind)
	}
	return u.Exec(ctx)
}

// SetBookSlotStatus moves one slot's status from `from` to `to` and does
// nothing when the slot is in another status.
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

// SetBookSlot monitors or unmonitors one slot. Monitoring moves skipped to
// wanted; unmonitoring moves wanted and paused to skipped. A slot that is
// downloading or available keeps its status, so unmonitoring never cancels a
// grab in flight.
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
	c := tx.Client()
	switch kind {
	case string(mediafile.BookKindEbook):
		err = c.Book.UpdateOneID(id).SetEbookMonitored(monitored).Exec(ctx)
		if err == nil {
			err = moveSlot(ctx, c, id, kind, monitored)
		}
	case string(mediafile.BookKindAudiobook):
		err = c.Book.UpdateOneID(id).SetAudiobookMonitored(monitored).Exec(ctx)
		if err == nil {
			err = moveSlot(ctx, c, id, kind, monitored)
		}
	default:
		err = fmt.Errorf("unknown book slot kind %q", kind)
	}
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func moveSlot(
	ctx context.Context,
	c *ent.Client,
	id uint32,
	kind string,
	monitored bool,
) error {
	u := c.Book.Update().Where(book.IDEQ(id))
	if kind == string(mediafile.BookKindEbook) {
		if monitored {
			return u.Where(book.EbookStatusEQ(book.EbookStatusSkipped)).
				SetEbookStatus(book.EbookStatusWanted).Exec(ctx)
		}
		return u.Where(book.EbookStatusIn(
			book.EbookStatusWanted, book.EbookStatusPaused,
		)).SetEbookStatus(book.EbookStatusSkipped).Exec(ctx)
	}
	if monitored {
		return u.Where(book.AudiobookStatusEQ(book.AudiobookStatusSkipped)).
			SetAudiobookStatus(book.AudiobookStatusWanted).Exec(ctx)
	}
	return u.Where(book.AudiobookStatusIn(
		book.AudiobookStatusWanted, book.AudiobookStatusPaused,
	)).SetAudiobookStatus(book.AudiobookStatusSkipped).Exec(ctx)
}

// DeleteBook removes the book; editions, contributions, files and records
// cascade in the schema. People left with no contribution go in the same
// transaction; their ids are returned for poster cleanup.
func (db *DB) DeleteBook(ctx context.Context, id uint32) ([]uint32, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	c := tx.Client()
	if err := c.Book.DeleteOneID(id).Exec(ctx); err != nil {
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

// ListUpcomingBooks returns the monitored books releasing in [from, to).
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
		WithSeries().
		Order(ent.Asc(book.FieldReleaseDate)).
		All(ctx)
}

// searchable keeps a stub and an unreleased book out of every search: a stub
// has no data to search with, an unreleased book has nothing to find yet.
func searchable(now time.Time) predicate.Book {
	return book.And(
		book.LastRefreshedAtNotNil(),
		book.Or(book.ReleaseDateIsNil(), book.ReleaseDateLTE(now)),
	)
}

func slotInFlight(kind string) predicate.Book {
	return book.Not(book.HasDownloadRecordsWith(
		downloadrecord.BookKindEQ(downloadrecord.BookKind(kind)),
		downloadrecord.StatusIn(
			downloadrecord.StatusDownloading,
			downloadrecord.StatusImporting,
		),
	))
}

func eligibleSlot(
	kind string,
	maxGrabFailures uint8,
	notSearchedSince time.Time,
) (predicate.Book, error) {
	switch kind {
	case string(mediafile.BookKindEbook):
		return book.And(
			book.EbookStatusEQ(book.EbookStatusWanted),
			book.EbookMonitoredEQ(true),
			book.EbookGrabFailuresLT(maxGrabFailures),
			book.Or(
				book.EbookLastSearchAtIsNil(),
				book.EbookLastSearchAtLT(notSearchedSince),
			),
			slotInFlight(kind),
		), nil
	case string(mediafile.BookKindAudiobook):
		return book.And(
			book.AudiobookStatusEQ(book.AudiobookStatusWanted),
			book.AudiobookMonitoredEQ(true),
			book.AudiobookGrabFailuresLT(maxGrabFailures),
			book.Or(
				book.AudiobookLastSearchAtIsNil(),
				book.AudiobookLastSearchAtLT(notSearchedSince),
			),
			slotInFlight(kind),
		), nil
	}
	return nil, fmt.Errorf("unknown book slot kind %q", kind)
}

// ListEligibleBookSlotsForSync returns the standalone books whose kind slot is
// wanted and monitored, under that slot's failure cap and past its cooldown,
// least recently searched first (never-searched rows lead, SQLite sorts NULL
// first). A series volume's ebook slot is searched by its series, not here; its
// audiobook slot is an ordinary slot. The in-flight exclusion is per kind: an
// audiobook download does not hide the ebook slot.
func (db *DB) ListEligibleBookSlotsForSync(
	ctx context.Context,
	kind string,
	maxGrabFailures uint8,
	notSearchedSince time.Time,
) ([]*ent.Book, error) {
	slot, err := eligibleSlot(kind, maxGrabFailures, notSearchedSince)
	if err != nil {
		return nil, err
	}
	q := db.client.Book.Query().
		Where(slot, searchable(time.Now())).
		WithSeries().
		WithEbookEdition().
		WithAudiobookEdition().
		WithContributions(func(cq *ent.BookContributionQuery) { cq.WithAuthor() })
	if kind == string(mediafile.BookKindEbook) {
		q = q.Where(book.Not(book.HasSeries())).
			Order(ent.Asc(book.FieldEbookLastSearchAt), ent.Asc(book.FieldID))
	} else {
		q = q.Order(ent.Asc(book.FieldAudiobookLastSearchAt), ent.Asc(book.FieldID))
	}
	return q.All(ctx)
}

// ListEligibleSeriesVolumes returns the series volumes whose ebook slot is
// eligible, series loaded.
func (db *DB) ListEligibleSeriesVolumes(
	ctx context.Context,
	maxGrabFailures uint8,
	notSearchedSince time.Time,
) ([]*ent.Book, error) {
	slot, err := eligibleSlot(
		string(mediafile.BookKindEbook), maxGrabFailures, notSearchedSince,
	)
	if err != nil {
		return nil, err
	}
	return db.client.Book.Query().
		Where(slot, searchable(time.Now()), book.HasSeries()).
		WithSeries().
		WithEbookEdition().
		Order(ent.Asc(book.FieldID)).
		All(ctx)
}

func (db *DB) SetBookSlotLastSearchAt(
	ctx context.Context,
	id uint32,
	kind string,
	when time.Time,
) error {
	u := db.client.Book.UpdateOneID(id)
	switch kind {
	case string(mediafile.BookKindEbook):
		u = u.SetEbookLastSearchAt(when)
	case string(mediafile.BookKindAudiobook):
		u = u.SetAudiobookLastSearchAt(when)
	default:
		return fmt.Errorf("unknown book slot kind %q", kind)
	}
	return u.Exec(ctx)
}

func (db *DB) IncrementBookSlotGrabFailures(
	ctx context.Context,
	id uint32,
	kind string,
) error {
	u := db.client.Book.UpdateOneID(id)
	switch kind {
	case string(mediafile.BookKindEbook):
		u = u.AddEbookGrabFailures(1)
	case string(mediafile.BookKindAudiobook):
		u = u.AddAudiobookGrabFailures(1)
	default:
		return fmt.Errorf("unknown book slot kind %q", kind)
	}
	return u.Exec(ctx)
}

func (db *DB) ResetBookSlotGrabFailures(
	ctx context.Context,
	id uint32,
	kind string,
) error {
	u := db.client.Book.UpdateOneID(id)
	switch kind {
	case string(mediafile.BookKindEbook):
		u = u.SetEbookGrabFailures(0)
	case string(mediafile.BookKindAudiobook):
		u = u.SetAudiobookGrabFailures(0)
	default:
		return fmt.Errorf("unknown book slot kind %q", kind)
	}
	return u.Exec(ctx)
}

// ListHydrationStubs returns up to limit books never hydrated, oldest first:
// series stubs, and rows a pre-redesign database left without metadata.
func (db *DB) ListHydrationStubs(
	ctx context.Context,
	limit int,
) ([]*ent.Book, error) {
	return db.client.Book.Query().
		Where(book.LastRefreshedAtIsNil()).
		WithSeries().
		Order(ent.Asc(book.FieldID)).
		Limit(limit).
		All(ctx)
}

// ListPositionPeers returns the other books of each given book's series that
// sit at the same position, hydrated or not, with their series loaded. Books
// without a series or a position have none.
func (db *DB) ListPositionPeers(
	ctx context.Context,
	books []*ent.Book,
) ([]*ent.Book, error) {
	held := make([]uint32, 0, len(books))
	positions := map[uint32][]float64{}
	for _, b := range books {
		held = append(held, b.ID)
		if b.Edges.Series != nil && b.SeriesPosition != nil {
			id := b.Edges.Series.ID
			positions[id] = append(positions[id], *b.SeriesPosition)
		}
	}
	var out []*ent.Book
	for seriesID, at := range positions {
		rows, err := db.client.Book.Query().
			Where(
				book.HasSeriesWith(bookseries.ID(seriesID)),
				book.SeriesPositionIn(at...),
				book.IDNotIn(held...),
			).
			WithSeries().
			All(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

// ListStaleStandaloneBooks returns at most limit hydrated standalone books last
// refreshed before cutoff, oldest first. Keyed on last_refreshed_at, not
// update_time, which moves on every write.
func (db *DB) ListStaleStandaloneBooks(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) ([]*ent.Book, error) {
	return db.client.Book.Query().
		Where(
			book.Not(book.HasSeries()),
			book.LastRefreshedAtLT(cutoff),
		).
		Order(ent.Asc(book.FieldLastRefreshedAt), ent.Asc(book.FieldID)).
		Limit(limit).
		All(ctx)
}

// ListWantedBooks returns the searchable books with at least one slot that is
// monitored, wanted, under the grab-failure cap and not covered by a live
// download record, with their series, slot editions and makers loaded. The
// query only says some slot is eligible; the caller re-checks each slot. The
// cooldown is not applied: the feed scanner already holds the release.
func (db *DB) ListWantedBooks(
	ctx context.Context,
	maxGrabFailures uint8,
) ([]*ent.Book, error) {
	return db.client.Book.Query().
		Where(
			searchable(time.Now()),
			book.Or(
				book.And(
					book.EbookMonitored(true),
					book.EbookStatusEQ(book.EbookStatusWanted),
					book.EbookGrabFailuresLT(maxGrabFailures),
					slotInFlight(string(downloadrecord.BookKindEbook)),
				),
				book.And(
					book.AudiobookMonitored(true),
					book.AudiobookStatusEQ(book.AudiobookStatusWanted),
					book.AudiobookGrabFailuresLT(maxGrabFailures),
					slotInFlight(string(downloadrecord.BookKindAudiobook)),
				),
			),
		).
		WithSeries().
		WithEbookEdition().
		WithAudiobookEdition().
		WithEditions().
		WithContributions(func(cq *ent.BookContributionQuery) { cq.WithAuthor() }).
		All(ctx)
}

// MarkBookRefreshed stamps a book as read at `at` without changing its data,
// for a row the provider no longer knows: without the stamp it would be picked
// as a stub on every pass.
func (db *DB) MarkBookRefreshed(ctx context.Context, id uint32, at time.Time) error {
	return db.client.Book.UpdateOneID(id).SetLastRefreshedAt(at).Exec(ctx)
}

// BookRecordRef ties a live download record to the book slot it fills.
type BookRecordRef struct {
	RecordID uint32
	BookID   uint32
	Kind     string
}

// ListActiveBookRecords returns the in-flight download records of the given
// books, one per slot being fetched.
func (db *DB) ListActiveBookRecords(
	ctx context.Context,
	bookIDs []uint32,
) ([]BookRecordRef, error) {
	rows, err := db.client.DownloadRecord.Query().
		Where(
			downloadrecord.HasBookWith(book.IDIn(bookIDs...)),
			downloadrecord.StatusIn(
				downloadrecord.StatusDownloading,
				downloadrecord.StatusImporting,
			),
		).
		WithBook(func(q *ent.BookQuery) { q.Select(book.FieldID) }).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]BookRecordRef, 0, len(rows))
	for _, r := range rows {
		if r.Edges.Book == nil {
			continue
		}
		out = append(out, BookRecordRef{
			RecordID: r.ID,
			BookID:   r.Edges.Book.ID,
			Kind:     string(r.BookKind),
		})
	}
	return out, nil
}
