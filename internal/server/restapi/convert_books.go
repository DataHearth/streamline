package restapi

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/bookcontribution"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

const (
	tenth     = 10
	slotEbook = "ebook"
	slotAudio = "audiobook"
)

func ratingFromTenths(t *uint8) *float32 {
	if t == nil || *t == 0 {
		return nil
	}
	v := float32(*t) / tenth
	return &v
}

// makerRoles are the roles a book or series lists as contributors; translators
// and narrators belong to the editions.
func isMakerRole(r bookcontribution.Role) bool {
	switch r {
	case bookcontribution.RoleAuthor,
		bookcontribution.RoleWriter,
		bookcontribution.RoleArtist,
		bookcontribution.RoleColorist,
		bookcontribution.RoleCover:
		return true
	}
	return false
}

// toBookCredits lists contributions as credits. translators keeps the
// translator rows a series carries; a book never lists them.
func toBookCredits(rows []*ent.BookContribution, translators bool) []BookCredit {
	out := make([]BookCredit, 0, len(rows))
	for _, c := range rows {
		if c.Edges.Author == nil {
			continue
		}
		if !isMakerRole(c.Role) &&
			(!translators || c.Role != bookcontribution.RoleTranslator) {
			continue
		}
		credit := BookCredit{
			AuthorId: c.Edges.Author.ID,
			Name:     c.Edges.Author.Name,
			Role:     BookRole(c.Role),
			Language: optString(c.Language),
		}
		out = append(out, credit)
	}
	return out
}

func toBookEditions(b *ent.Book) []BookEdition {
	eds := slices.Clone(b.Edges.Editions)
	slices.SortStableFunc(eds, func(x, y *ent.BookEdition) int {
		xp, yp := x.Language == b.PreferredLanguage, y.Language == b.PreferredLanguage
		switch {
		case xp && !yp:
			return -1
		case yp && !xp:
			return 1
		}
		return cmp.Or(
			cmp.Compare(x.Language, y.Language),
			cmp.Compare(string(x.Format), string(y.Format)),
			cmp.Compare(y.Popularity, x.Popularity),
			cmp.Compare(x.ID, y.ID),
		)
	})
	out := make([]BookEdition, 0, len(eds))
	for _, e := range eds {
		item := BookEdition{
			Id:         e.ID,
			Language:   e.Language,
			Title:      e.Title,
			Publisher:  e.Publisher,
			Year:       e.Year,
			Format:     BookFormat(e.Format),
			Pages:      e.Pages,
			Duration:   e.DurationSeconds,
			Narrator:   optString(e.Narrator),
			Translator: optString(e.Translator),
		}
		if e.Original {
			t := true
			item.Original = &t
		}
		out = append(out, item)
	}
	return out
}

// slotFile summarises a slot's files: the container of the first and the total
// size, with the file count for a folder.
func slotFile(b *ent.Book, kind string) *BookSlotFile {
	var files []*ent.MediaFile
	for _, f := range b.Edges.MediaFiles {
		if string(f.BookKind) == kind {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return nil
	}
	slices.SortFunc(files, func(a, b *ent.MediaFile) int {
		return strings.Compare(a.Path, b.Path)
	})
	var size int64
	for _, f := range files {
		size += f.Size
	}
	out := &BookSlotFile{
		Container: strings.ToUpper(
			strings.TrimPrefix(filepath.Ext(files[0].Path), "."),
		),
		Size: size,
	}
	if len(files) > 1 {
		n := numeric.SaturateU32(len(files))
		out.FileCount = &n
	}
	return out
}

func toFormatSlot(
	b *ent.Book,
	kind string,
	progress map[string]float64,
) BookFormatSlot {
	slot := BookFormatSlot{
		Format: BookFormat(kind),
		State:  BookFormatState(book.SlotState(b, kind)),
		File:   slotFile(b, kind),
	}
	if ed := slotEdition(b, kind); ed != nil {
		id := ed.ID
		slot.EditionId = &id
	}
	if slot.State == BookFormatStateDownloading {
		if p, ok := progress[kind]; ok {
			v := float32(p)
			slot.Progress = &v
		}
	}
	switch kind {
	case slotAudio:
		if b.AudiobookReplacingLanguage != "" {
			slot.Replacing = &BookReplacing{Language: b.AudiobookReplacingLanguage}
		}
	default:
		if b.EbookReplacingLanguage != "" {
			slot.Replacing = &BookReplacing{Language: b.EbookReplacingLanguage}
		}
	}
	return slot
}

func slotEdition(b *ent.Book, kind string) *ent.BookEdition {
	if kind == slotAudio {
		return b.Edges.AudiobookEdition
	}
	return b.Edges.EbookEdition
}

func firstPublished(b *ent.Book) uint16 {
	switch {
	case b.ReleaseYear != nil:
		return *b.ReleaseYear
	case b.ReleaseDate != nil:
		return uint16(max(b.ReleaseDate.Year(), 0)) //nolint:gosec // a year fits
	}
	return 0
}

// toAPIBook renders the book detail. progress is the live percentage of each
// slot being downloaded, from Manager.Progress.
func toAPIBook(b *ent.Book, progress map[string]float64) Book {
	out := Book{
		Id:                b.ID,
		HardcoverId:       b.HardcoverID,
		Title:             b.Title,
		OriginalTitle:     optString(b.OriginalTitle),
		Author:            b.AuthorName,
		Kind:              BookKind(b.Kind),
		Genre:             optString(b.Genre),
		FirstPublished:    firstPublished(b),
		Overview:          optString(b.Overview),
		Rating:            ratingFromTenths(b.RatingTenths),
		Contributors:      toBookCredits(b.Edges.Contributions, false),
		Status:            BookItemStatus(book.Status(b, time.Now())),
		Monitor:           BookMonitor(book.Monitor(b)),
		QualityProfile:    optString(b.QualityProfile),
		PreferredLanguage: b.PreferredLanguage,
		AddedAt:           b.CreateTime,
		Formats: []BookFormatSlot{
			toFormatSlot(b, slotEbook, progress),
			toFormatSlot(b, slotAudio, progress),
		},
		Editions: toBookEditions(b),
	}
	if s := b.Edges.Series; s != nil {
		ref := BookSeriesRef{Id: s.ID, Title: s.Title}
		if b.SeriesPosition != nil {
			ref.Number = float32(*b.SeriesPosition)
		}
		out.Series = &ref
	}
	return out
}

func toAPIVolume(v *ent.Book, now time.Time) BookVolume {
	out := BookVolume{
		Id:     v.ID,
		Status: BookVolumeStatus(book.VolumeStatus(v, now)),
	}
	if v.SeriesPosition != nil {
		out.Number = float32(*v.SeriesPosition)
	}
	if v.ReleaseDate != nil {
		d := openapi_types.Date{Time: *v.ReleaseDate}
		out.ReleaseDate = &d
	}
	return out
}

// seriesRating is the mean of the volumes' ratings, derived because Hardcover
// publishes none for a series.
func seriesRating(s *ent.BookSeries) *float32 {
	var sum, n int
	for _, v := range s.Edges.Volumes {
		if v.RatingTenths != nil && *v.RatingTenths > 0 {
			sum += int(*v.RatingTenths)
			n++
		}
	}
	if n == 0 {
		return nil
	}
	mean := (sum + n/2) / n
	t := uint8(min(mean, 50)) //nolint:gosec // capped at 50
	return ratingFromTenths(&t)
}

func seriesSince(s *ent.BookSeries) uint16 {
	if s.Since != nil {
		return *s.Since
	}
	var since uint16
	for _, v := range s.Edges.Volumes {
		if y := firstPublished(v); y != 0 && (since == 0 || y < since) {
			since = y
		}
	}
	return since
}

func toAPISeries(s *ent.BookSeries) BookSeries {
	now := time.Now()
	volumes := make([]BookVolume, 0, len(s.Edges.Volumes))
	for _, v := range s.Edges.Volumes {
		volumes = append(volumes, toAPIVolume(v, now))
	}
	options := book.SeriesEditions(s)
	labels := make([]string, 0, len(options))
	for _, o := range options {
		labels = append(labels, o.Label)
	}
	return BookSeries{
		Id:             s.ID,
		HardcoverId:    s.HardcoverID,
		Title:          s.Title,
		OriginalTitle:  optString(s.OriginalTitle),
		Overview:       optString(s.Overview),
		Author:         s.AuthorName,
		Kind:           BookKind(s.Kind),
		Rating:         seriesRating(s),
		Contributors:   toBookCredits(s.Edges.Contributions, true),
		Status:         BookItemStatus(book.SeriesStatus(s, now)),
		Ongoing:        s.Ongoing,
		Since:          seriesSince(s),
		Monitor:        BookSeriesMonitor(s.Monitor),
		QualityProfile: optString(s.QualityProfile),
		Hydrating:      book.Hydrating(s),
		Edition:        book.SeriesEditionLabel(s),
		Editions:       labels,
		AddedAt:        s.CreateTime,
		Volumes:        volumes,
	}
}

func toShelfItem(r db.ShelfRow, progress map[uint32]float64) ShelfItem {
	item := ShelfItem{
		Type:           BookShelfType(r.Type),
		Id:             r.ID,
		CoverId:        r.CoverID,
		Title:          r.Title,
		Author:         r.Author,
		Kind:           BookKind(r.Kind),
		Status:         BookItemStatus(r.Status),
		AddedAt:        r.AddedAt,
		QualityProfile: optString(r.QualityProfile),
	}
	if r.Type == "series" {
		have, out := r.VolumesHave, r.VolumesOut
		item.VolumesHave, item.VolumesOut = &have, &out
		return item
	}
	if r.Year != 0 {
		y := r.Year
		item.Year = &y
	}
	item.Formats = &[]BookShelfFormat{
		{Format: BookFormatEbook, State: BookFormatState(r.EbookState)},
		{Format: BookFormatAudiobook, State: BookFormatState(r.AudiobookState)},
	}
	if p, ok := progress[r.ID]; ok && r.Status == book.StateDownloading {
		v := float32(p)
		item.Progress = &v
	}
	return item
}

func toBookCounts(c db.ShelfCounts) BookCounts {
	authors := make([]BookAuthorCount, 0, len(c.Authors))
	for _, a := range c.Authors {
		authors = append(authors, BookAuthorCount{Name: a.Name, Count: a.Count})
	}
	return BookCounts{
		Total:       c.Total,
		StatusTotal: c.StatusTotal,
		Available:   c.Available,
		Wanted:      c.Wanted,
		Downloading: c.Downloading,
		AuthorTotal: c.AuthorTotal,
		Authors:     authors,
		FormatTotal: c.FormatTotal,
		Ebook:       c.Ebook,
		Audiobook:   c.Audiobook,
	}
}

func toAPILookupHit(h book.LookupHit) BookLookupHit {
	out := BookLookupHit{
		HardcoverId:  h.HardcoverID,
		Type:         BookShelfType(h.Type),
		Title:        h.Title,
		Author:       h.Author,
		AlreadyAdded: h.AlreadyAdded,
		Ongoing:      h.Ongoing,
	}
	out.OriginalTitle = optString(h.OriginalTitle)
	if h.Kind != "" {
		k := BookKind(h.Kind)
		out.Kind = &k
	}
	if h.Year != 0 {
		y := h.Year
		out.Year = &y
	}
	if h.Volumes != 0 {
		v := h.Volumes
		out.Volumes = &v
	}
	if h.AlreadyAdded {
		id := h.LibraryID
		out.LibraryId = &id
		if h.CoverID != 0 {
			c := h.CoverID
			out.CoverId = &c
		}
	}
	return out
}

func toAPILookupDetail(d *book.LookupDetail) BookLookupDetail {
	hit := toAPILookupHit(d.LookupHit)
	out := BookLookupDetail{
		HardcoverId:   hit.HardcoverId,
		Type:          hit.Type,
		Title:         hit.Title,
		OriginalTitle: hit.OriginalTitle,
		Author:        hit.Author,
		Kind:          hit.Kind,
		Year:          hit.Year,
		Volumes:       hit.Volumes,
		Ongoing:       hit.Ongoing,
		AlreadyAdded:  hit.AlreadyAdded,
		LibraryId:     hit.LibraryId,
		CoverId:       hit.CoverId,
		Overview:      optString(d.Overview),
	}
	if len(d.Genres) > 0 {
		g := d.Genres
		out.Genres = &g
	}
	if d.Pages != 0 {
		p := d.Pages
		out.Pages = &p
	}
	if len(d.Editions) > 0 {
		eds := make([]BookLookupEdition, 0, len(d.Editions))
		for _, e := range d.Editions {
			item := BookLookupEdition{
				Language:  e.Language,
				Format:    BookFormat(e.Format),
				Publisher: e.Publisher,
				Year:      e.Year,
			}
			if e.Original {
				t := true
				item.Original = &t
			}
			eds = append(eds, item)
		}
		out.Editions = &eds
	}
	if len(d.VolumeBookIDs) > 0 {
		ids := d.VolumeBookIDs
		out.VolumeBookIds = &ids
	}
	return out
}

// toBookRelease renders one scored release for the shared releases table.
func toBookRelease(r book.ReleaseResult, private map[string]bool) SearchResult {
	reason := ""
	score := r.Score
	if r.Rejected {
		reason = r.Reason
		if score >= 0 {
			score = -1
		}
	}
	item := toBookSearchResult(
		r.SearchResult, library.ParseBookRelease(r.Title), score, reason,
	)
	if p, ok := private[r.ConfiguredIndexer]; ok {
		item.IndexerPrivate = &p
	}
	return item
}

func toRenameOperations(ops []library.RenameOperation) []RenameOperation {
	out := make([]RenameOperation, 0, len(ops))
	for _, op := range ops {
		out = append(out, RenameOperation{
			MediaFileId: op.MediaFileID, From: op.From, To: op.To,
		})
	}
	return out
}

// firstCreator is the person an upcoming book is listed under: the name is the
// book's display author, the id the first author-role contribution, else the
// first writer's, else the first artist's. The contributions arrive ordered.
func firstCreator(b *ent.Book) (uint32, string) {
	for _, role := range []bookcontribution.Role{
		bookcontribution.RoleAuthor,
		bookcontribution.RoleWriter,
		bookcontribution.RoleArtist,
	} {
		for _, c := range b.Edges.Contributions {
			if c.Role != role || c.Edges.Author == nil {
				continue
			}
			name := b.AuthorName
			if name == "" {
				name = c.Edges.Author.Name
			}
			return c.Edges.Author.ID, name
		}
	}
	return 0, b.AuthorName
}

func toAPIImportScanBook(sb *ent.ImportScanBook) ImportScanBook {
	out := ImportScanBook{
		Id:             sb.ID,
		FilePaths:      sb.FilePaths,
		Slot:           ImportScanBookSlot(sb.Slot),
		Classification: ImportScanBookClassification(sb.Classification),
		Decision:       ImportScanBookDecision(sb.Decision),
		Outcome:        ImportScanBookOutcome(sb.Outcome),
		ExistingBookId: sb.ExistingBookID,
		CreatedBookId:  sb.CreatedBookID,
		ParsedTitle:    optString(sb.ParsedTitle),
		ParsedAuthor:   optString(sb.ParsedAuthor),
		ParsedIsbn:     optString(sb.ParsedIsbn),
		OutcomeMessage: optString(sb.OutcomeMessage),
	}
	if sb.BookHardcoverID != 0 {
		id := sb.BookHardcoverID
		out.BookHardcoverId = &id
	}
	if sb.DecisionBookHardcoverID != 0 {
		id := sb.DecisionBookHardcoverID
		out.DecisionBookHardcoverId = &id
	}
	if len(sb.Candidates) > 0 {
		cands := make([]ImportScanBookCandidate, 0, len(sb.Candidates))
		for _, c := range sb.Candidates {
			cand := ImportScanBookCandidate{
				BookHardcoverId: c.BookHardcoverID,
				Title:           c.Title,
				Author:          optString(c.Author),
			}
			if c.Year != 0 {
				y := c.Year
				cand.Year = &y
			}
			cands = append(cands, cand)
		}
		out.Candidates = &cands
	}
	created, updated := sb.CreateTime, sb.UpdateTime
	out.CreatedAt, out.UpdatedAt = &created, &updated
	return out
}
