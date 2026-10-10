package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/bookseries"
)

// The shelf is the books page: standalone books and series in one list. A
// volume is never an item of its own. ent cannot union, and merging two paged
// ent queries gives wrong deep pages, so the list is one UNION ALL read in raw
// SQL, with the filters applied inside both arms and the status (derived from
// the slots) filtered, ordered and paged outside.

const (
	shelfFacetStatus = "status"
	shelfFacetAuthor = "author"
	shelfFacetFormat = "format"
)

// ShelfParams are the shelf filters. An empty field filters nothing.
type ShelfParams struct {
	Status string // available | wanted | downloading
	Author string // a creator's name, folded
	Format string // ebook | audiobook
	Kinds  []string
	Query  string
	Sort   string // added | title | year
	Desc   bool
	Limit  uint32
	Offset uint32
}

// ShelfRow is one shelf item: a book or a series. A series carries empty slot
// states and the volume counts; a book carries zero counts.
type ShelfRow struct {
	Type           string    `sql:"type"`
	ID             uint32    `sql:"id"`
	CoverID        uint32    `sql:"cover_id"`
	Title          string    `sql:"title"`
	Author         string    `sql:"author"`
	Kind           string    `sql:"kind"`
	Year           uint16    `sql:"year"`
	Status         string    `sql:"status"`
	EbookState     string    `sql:"ebook_state"`
	AudiobookState string    `sql:"audiobook_state"`
	VolumesHave    uint32    `sql:"volumes_have"`
	VolumesOut     uint32    `sql:"volumes_out"`
	QualityProfile string    `sql:"quality_profile"`
	AddedAt        time.Time `sql:"-"`
}

// ShelfAuthorCount is one entry of the author facet.
type ShelfAuthorCount struct {
	Name  string `sql:"name"`
	Count uint32 `sql:"n"`
}

// ShelfCounts are the faceted counts of the shelf. Each facet is counted with
// the other facets' filters applied and its own left out.
type ShelfCounts struct {
	Total       uint32
	StatusTotal uint32
	Available   uint32
	Wanted      uint32
	Downloading uint32
	AuthorTotal uint32
	Authors     []ShelfAuthorCount
	FormatTotal uint32
	Ebook       uint32
	Audiobook   uint32
}

type sqlBuilder struct {
	b    strings.Builder
	args []any
}

func (q *sqlBuilder) w(s string, args ...any) {
	q.b.WriteString(s)
	q.args = append(q.args, args...)
}

// slotState renders a slot's FormatState for alias a: unmonitored iff the slot
// is not monitored and neither available nor downloading; otherwise available
// and downloading map across (paused is still in flight) and a monitored wanted
// or skipped slot is wanted.
func slotState(a, slot string) string {
	return fmt.Sprintf(`CASE
 WHEN %[1]s.%[2]s_status = 'downloading' OR (%[1]s.%[2]s_status = 'paused' AND %[1]s.%[2]s_monitored = 1) THEN 'downloading'
 WHEN %[1]s.%[2]s_monitored = 1 AND %[1]s.%[2]s_status IN ('wanted','skipped') THEN 'wanted'
 WHEN %[1]s.%[2]s_status = 'available' THEN 'available'
 ELSE 'unmonitored' END`, a, slot)
}

func volumeDownloading(a string) string {
	return fmt.Sprintf(
		"(%[1]s.ebook_status = 'downloading' OR (%[1]s.ebook_status = 'paused' AND %[1]s.ebook_monitored = 1))",
		a,
	)
}

func volumeWanted(a string) string {
	return fmt.Sprintf(
		"(%[1]s.ebook_monitored = 1 AND %[1]s.ebook_status IN ('wanted','skipped'))",
		a,
	)
}

const (
	releasedVolume = "(v.release_date IS NULL OR v.release_date <= ?)"
	creatorRoles   = "('author','writer','artist')"
)

func hasFile(a, kind string) string {
	return fmt.Sprintf(
		"EXISTS (SELECT 1 FROM media_files m WHERE m.book_media_files = %s.id AND m.book_kind = '%s')",
		a,
		kind,
	)
}

// shelfFilters appends the arm's WHERE conditions, leaving out the facet named
// by omit. owner is the contribution column that points at the arm's row.
func shelfFilters(q *sqlBuilder, p ShelfParams, omit, arm string) {
	owner, alias := "book_contributions", "b"
	if arm == "series" {
		owner, alias = "book_series_contributions", "s"
	}
	if len(p.Kinds) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(p.Kinds)), ",")
		args := make([]any, len(p.Kinds))
		for i, k := range p.Kinds {
			args[i] = k
		}
		q.w(" AND "+alias+".kind IN ("+marks+")", args...)
	}
	if omit != shelfFacetFormat && p.Format != "" {
		switch {
		case arm == "series" && p.Format == "ebook":
		case arm == "series":
			q.w(" AND 0")
		case p.Format == "ebook":
			q.w(" AND " + ebookOK())
		default:
			q.w(" AND " + audioOK())
		}
	}
	if omit != shelfFacetAuthor && p.Author != "" {
		q.w(
			" AND EXISTS (SELECT 1 FROM book_contributions c JOIN authors a ON a.id = c.author_contributions"+
				" WHERE c."+owner+" = "+alias+".id AND c.role IN "+creatorRoles+" AND fold(a.name) = ?)",
			foldText(p.Author),
		)
	}
	if p.Query != "" {
		like := "%" + foldText(p.Query) + "%"
		q.w(
			" AND (fold("+alias+".title) LIKE ? OR fold("+alias+".original_title) LIKE ?",
			like,
			like,
		)
		if arm == "series" {
			q.w(
				" OR EXISTS (SELECT 1 FROM book_editions e JOIN books v ON e.book_editions = v.id" +
					" WHERE v.book_series_volumes = s.id AND fold(e.title) LIKE ?)",
			)
		} else {
			q.w(
				" OR EXISTS (SELECT 1 FROM book_editions e WHERE e.book_editions = b.id AND fold(e.title) LIKE ?)",
			)
		}
		q.args = append(q.args, like)
		q.w(
			" OR EXISTS (SELECT 1 FROM book_contributions c JOIN authors a ON a.id = c.author_contributions"+
				" WHERE c."+owner+" = "+alias+".id AND c.role IN "+creatorRoles+" AND fold(a.name) LIKE ?))",
			like,
		)
	}
}

func ebookOK() string { return "(b.ebook_monitored = 1 OR " + hasFile("b", "ebook") + ")" }

func audioOK() string { return "(b.audiobook_monitored = 1 OR " + hasFile("b", "audiobook") + ")" }

// shelfUnion is the item list: one row per standalone book and per series,
// with the derived status. Its columns are the ShelfRow ones plus added_key,
// the sort key for the arrival order, and the two format flags.
func shelfUnion(q *sqlBuilder, p ShelfParams, now time.Time, omit string) {
	es, as := slotState("b", "ebook"), slotState("b", "audiobook")
	q.w(`SELECT 'book' AS type, b.id AS id, b.id AS cover_id, b.title AS title,
 COALESCE(b.author_name, '') AS author, b.kind AS kind, b.create_time AS added_key,
 COALESCE(b.release_year, 0) AS year,
 CASE WHEN b.release_date IS NOT NULL AND b.release_date > ? THEN 'available'
  WHEN (`+es+`) = 'downloading' OR (`+as+`) = 'downloading' THEN 'downloading'
  WHEN (`+es+`) = 'wanted' OR (`+as+`) = 'wanted' THEN 'wanted'
  ELSE 'available' END AS status,
 (`+es+`) AS ebook_state, (`+as+`) AS audiobook_state,
 0 AS volumes_have, 0 AS volumes_out, COALESCE(b.quality_profile, '') AS quality_profile,
 CASE WHEN `+ebookOK()+` THEN 1 ELSE 0 END AS ebook_ok,
 CASE WHEN `+audioOK()+` THEN 1 ELSE 0 END AS audio_ok
 FROM books b WHERE b.book_series_volumes IS NULL`, now)
	shelfFilters(q, p, omit, "book")

	q.w(`
 UNION ALL
 SELECT 'series', s.id,
 COALESCE((SELECT v.id FROM books v WHERE v.book_series_volumes = s.id
  ORDER BY v.series_position, v.id LIMIT 1), 0),
 s.title, COALESCE(s.author_name, ''), s.kind, s.create_time, COALESCE(s.since, 0),
 CASE WHEN EXISTS (SELECT 1 FROM books v WHERE v.book_series_volumes = s.id
   AND v.last_refreshed_at IS NOT NULL AND `+releasedVolume+` AND `+volumeDownloading("v")+`) THEN 'downloading'
  WHEN s.monitor <> 'none' AND EXISTS (SELECT 1 FROM books v WHERE v.book_series_volumes = s.id
   AND v.last_refreshed_at IS NOT NULL AND `+releasedVolume+` AND `+volumeWanted("v")+`) THEN 'wanted'
  ELSE 'available' END,
 '', '',
 (SELECT COUNT(*) FROM books v WHERE v.book_series_volumes = s.id AND v.ebook_status = 'available'),
 (SELECT COUNT(*) FROM books v WHERE v.book_series_volumes = s.id AND `+releasedVolume+`),
 COALESCE(s.quality_profile, ''), 1, 0
 FROM book_series s WHERE 1 = 1`, now, now, now)
	shelfFilters(q, p, omit, "series")
}

func (db *DB) scanRaw(
	ctx context.Context,
	q *sqlBuilder,
	columns []string,
	dest any,
) error {
	return db.client.Book.Query().
		Modify(func(s *entsql.Selector) {
			s.Select(columns...).
				FromExpr(entsql.Expr("("+q.b.String()+") AS raw_rows", q.args...))
		}).
		Scan(ctx, dest)
}

var shelfColumns = []string{
	"type",
	"id",
	"cover_id",
	"title",
	"author",
	"kind",
	"year",
	"status",
	"ebook_state",
	"audiobook_state",
	"volumes_have",
	"volumes_out",
	"quality_profile",
}

func shelfOrder(p ShelfParams) string {
	dir := "ASC"
	if p.Desc {
		dir = "DESC"
	}
	switch p.Sort {
	case "title":
		return "fold(title) " + dir + ", type, id"
	case "year":
		return "year " + dir + ", fold(title), type, id"
	}
	return "added_key " + dir + ", type, id"
}

// ListShelf returns one page of the shelf and the number of items matching the
// filters.
func (db *DB) ListShelf(
	ctx context.Context,
	p ShelfParams,
) ([]ShelfRow, uint32, error) {
	now := time.Now()

	count := &sqlBuilder{}
	count.w("SELECT COUNT(*) AS n FROM (")
	shelfUnion(count, p, now, "")
	count.w(") AS shelf")
	if p.Status != "" {
		count.w(" WHERE status = ?", p.Status)
	}
	var totals []struct {
		N uint32 `sql:"n"`
	}
	if err := db.scanRaw(ctx, count, []string{"n"}, &totals); err != nil {
		return nil, 0, fmt.Errorf("count shelf: %w", err)
	}
	var total uint32
	if len(totals) > 0 {
		total = totals[0].N
	}
	if total == 0 {
		return []ShelfRow{}, 0, nil
	}

	list := &sqlBuilder{}
	list.w("SELECT * FROM (")
	shelfUnion(list, p, now, "")
	list.w(") AS shelf")
	if p.Status != "" {
		list.w(" WHERE status = ?", p.Status)
	}
	list.w(
		" ORDER BY "+shelfOrder(p)+" LIMIT ? OFFSET ?",
		int(p.Limit),
		int(p.Offset),
	)
	var rows []ShelfRow
	if err := db.scanRaw(ctx, list, shelfColumns, &rows); err != nil {
		return nil, 0, fmt.Errorf("list shelf: %w", err)
	}
	if err := db.shelfAddedAt(ctx, rows); err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// shelfAddedAt fills the arrival time of a page from the rows themselves: an
// expression column carries no declared type, so the driver would hand the
// union's text back where a time is expected.
func (db *DB) shelfAddedAt(ctx context.Context, rows []ShelfRow) error {
	var bookIDs, seriesIDs []uint32
	for _, r := range rows {
		if r.Type == "series" {
			seriesIDs = append(seriesIDs, r.ID)
		} else {
			bookIDs = append(bookIDs, r.ID)
		}
	}
	books := map[uint32]time.Time{}
	if len(bookIDs) > 0 {
		var got []struct {
			ID uint32    `sql:"id"`
			At time.Time `sql:"create_time"`
		}
		if err := db.client.Book.Query().
			Where(book.IDIn(bookIDs...)).
			Select(book.FieldID, book.FieldCreateTime).
			Scan(ctx, &got); err != nil {
			return fmt.Errorf("shelf arrival times: %w", err)
		}
		for _, g := range got {
			books[g.ID] = g.At
		}
	}
	series := map[uint32]time.Time{}
	if len(seriesIDs) > 0 {
		var got []struct {
			ID uint32    `sql:"id"`
			At time.Time `sql:"create_time"`
		}
		if err := db.client.BookSeries.Query().
			Where(bookseries.IDIn(seriesIDs...)).
			Select(bookseries.FieldID, bookseries.FieldCreateTime).
			Scan(ctx, &got); err != nil {
			return fmt.Errorf("shelf arrival times: %w", err)
		}
		for _, g := range got {
			series[g.ID] = g.At
		}
	}
	for i := range rows {
		if rows[i].Type == "series" {
			rows[i].AddedAt = series[rows[i].ID]
		} else {
			rows[i].AddedAt = books[rows[i].ID]
		}
	}
	return nil
}

func (db *DB) scanCount(ctx context.Context, q *sqlBuilder) (uint32, error) {
	var out []struct {
		N uint32 `sql:"n"`
	}
	if err := db.scanRaw(ctx, q, []string{"n"}, &out); err != nil {
		return 0, err
	}
	if len(out) == 0 {
		return 0, nil
	}
	return out[0].N, nil
}

// ShelfCountsFor counts the shelf by facet. Total ignores every filter.
func (db *DB) ShelfCountsFor(
	ctx context.Context,
	p ShelfParams,
) (ShelfCounts, error) {
	now := time.Now()
	var c ShelfCounts

	whole := &sqlBuilder{}
	whole.w(`SELECT (SELECT COUNT(*) FROM books WHERE book_series_volumes IS NULL) +
 (SELECT COUNT(*) FROM book_series) AS n`)
	total, err := db.scanCount(ctx, whole)
	if err != nil {
		return c, fmt.Errorf("count shelf total: %w", err)
	}
	c.Total = total

	statuses := &sqlBuilder{}
	statuses.w("SELECT status, COUNT(*) AS n FROM (")
	shelfUnion(statuses, p, now, shelfFacetStatus)
	statuses.w(") AS shelf GROUP BY status")
	var byStatus []struct {
		Status string `sql:"status"`
		N      uint32 `sql:"n"`
	}
	if err := db.scanRaw(
		ctx,
		statuses,
		[]string{"status", "n"},
		&byStatus,
	); err != nil {
		return c, fmt.Errorf("count shelf by status: %w", err)
	}
	for _, r := range byStatus {
		c.StatusTotal += r.N
		switch r.Status {
		case "available":
			c.Available = r.N
		case "wanted":
			c.Wanted = r.N
		case "downloading":
			c.Downloading = r.N
		}
	}

	status := func(q *sqlBuilder) {
		if p.Status != "" {
			q.w(" WHERE status = ?", p.Status)
		}
	}

	formats := &sqlBuilder{}
	formats.w(
		"SELECT COUNT(*) AS n, COALESCE(SUM(ebook_ok), 0) AS ebook, COALESCE(SUM(audio_ok), 0) AS audiobook FROM (",
	)
	shelfUnion(formats, p, now, shelfFacetFormat)
	formats.w(") AS shelf")
	status(formats)
	var fr []struct {
		N         uint32 `sql:"n"`
		Ebook     uint32 `sql:"ebook"`
		Audiobook uint32 `sql:"audiobook"`
	}
	if err := db.scanRaw(
		ctx,
		formats,
		[]string{"n", "ebook", "audiobook"},
		&fr,
	); err != nil {
		return c, fmt.Errorf("count shelf by format: %w", err)
	}
	if len(fr) > 0 {
		c.FormatTotal, c.Ebook, c.Audiobook = fr[0].N, fr[0].Ebook, fr[0].Audiobook
	}

	authorTotal := &sqlBuilder{}
	authorTotal.w("SELECT COUNT(*) AS n FROM (")
	shelfUnion(authorTotal, p, now, shelfFacetAuthor)
	authorTotal.w(") AS shelf")
	status(authorTotal)
	if c.AuthorTotal, err = db.scanCount(ctx, authorTotal); err != nil {
		return c, fmt.Errorf("count shelf by author: %w", err)
	}

	authors := &sqlBuilder{}
	authors.w(
		`SELECT MIN(a.name) AS name, COUNT(DISTINCT shelf.type || ':' || shelf.id) AS n FROM (`,
	)
	shelfUnion(authors, p, now, shelfFacetAuthor)
	authors.w(`) AS shelf
 JOIN book_contributions c ON ((shelf.type = 'book' AND c.book_contributions = shelf.id)
  OR (shelf.type = 'series' AND c.book_series_contributions = shelf.id))
 JOIN authors a ON a.id = c.author_contributions
 WHERE c.role IN ` + creatorRoles)
	if p.Status != "" {
		authors.w(" AND shelf.status = ?", p.Status)
	}
	authors.w(" GROUP BY fold(a.name) ORDER BY fold(a.name)")
	if err := db.scanRaw(
		ctx,
		authors,
		[]string{"name", "n"},
		&c.Authors,
	); err != nil {
		return c, fmt.Errorf("list shelf authors: %w", err)
	}
	return c, nil
}
