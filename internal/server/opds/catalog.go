package opds

import (
	"cmp"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/author"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/bookcontribution"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/internal/config"
)

const (
	recentLimit = 50

	relAcquisition = "http://opds-spec.org/acquisition"
	relImage       = "http://opds-spec.org/image"

	openSearchType = "application/opensearchdescription+xml"
)

const openSearchDescription = `<?xml version="1.0" encoding="UTF-8"?>
<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/">
  <ShortName>Streamline</ShortName>
  <Description>Search the Streamline ebook catalog</Description>
  <Url type="` + acquisitionType + `" template="/opds/search?q={searchTerms}"/>
</OpenSearchDescription>
`

func ebookContentType(format string) string {
	switch strings.ToUpper(format) {
	case "EPUB":
		return "application/epub+zip"
	case "PDF":
		return "application/pdf"
	case "MOBI":
		return "application/x-mobipocket-ebook"
	case "AZW3":
		return "application/x-mobi8-ebook"
	case "CBZ":
		return "application/vnd.comicbook+zip"
	case "CBR":
		return "application/vnd.comicbook-rar"
	default:
		return "application/octet-stream"
	}
}

func (h *Handler) root(w http.ResponseWriter, r *http.Request) {
	f := &feed{
		ID:    "urn:streamline:opds:root",
		Title: "Streamline",
		Links: []link{
			selfLink("/opds", kindNavigation),
			{Rel: "start", Href: "/opds", Type: navigationType},
			{Rel: "search", Href: "/opds/search.xml", Type: openSearchType},
		},
		Entries: []entry{
			{
				ID:    "urn:streamline:opds:authors",
				Title: "Authors",
				Links: []link{
					{Rel: "subsection", Href: "/opds/authors", Type: navigationType},
				},
			},
			{
				ID:    "urn:streamline:opds:recent",
				Title: "Recently added",
				Links: []link{
					{
						Rel:  "http://opds-spec.org/sort/new",
						Href: "/opds/recent",
						Type: acquisitionType,
					},
				},
			},
		},
	}
	h.send(w, r, kindNavigation, f)
}

func (h *Handler) authors(w http.ResponseWriter, r *http.Request) {
	authors, err := h.client.Author.Query().
		Where(author.HasContributionsWith(
			creatorRole(),
			bookcontribution.HasBookWith(hasEbookFile()),
		)).
		All(r.Context())
	if err != nil {
		h.fail(w, r, "listing OPDS authors failed", err)
		return
	}
	slices.SortFunc(authors, func(a, b *ent.Author) int {
		return cmp.Compare(cmp.Or(a.SortName, a.Name), cmp.Or(b.SortName, b.Name))
	})

	f := &feed{
		ID:    "urn:streamline:opds:authors",
		Title: "Authors",
		Links: []link{
			selfLink("/opds/authors", kindNavigation),
			{Rel: "start", Href: "/opds", Type: navigationType},
		},
	}
	for _, a := range authors {
		f.Entries = append(f.Entries, entry{
			ID:      fmt.Sprintf("urn:streamline:author:%d", a.ID),
			Title:   a.Name,
			Updated: atomTime(a.UpdateTime),
			Links: []link{{
				Rel:  "subsection",
				Href: fmt.Sprintf("/opds/authors/%d", a.ID),
				Type: acquisitionType,
			}},
		})
		f.Updated = newest(f.Updated, a.UpdateTime)
	}
	h.send(w, r, kindNavigation, f)
}

func (h *Handler) authorBooks(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 32)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := h.client.Author.Get(r.Context(), uint32(id))
	if ent.IsNotFound(err) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, "loading OPDS author failed", err)
		return
	}

	books, err := h.ebookBooks().
		Where(book.HasContributionsWith(
			creatorRole(),
			bookcontribution.HasAuthorWith(author.IDEQ(a.ID)),
		)).
		Order(ent.Asc(book.FieldSortTitle), ent.Asc(book.FieldTitle)).
		All(r.Context())
	if err != nil {
		h.fail(w, r, "listing OPDS author books failed", err)
		return
	}
	h.sendAcquisition(
		w,
		r,
		fmt.Sprintf("urn:streamline:author:%d", a.ID),
		a.Name,
		fmt.Sprintf("/opds/authors/%d", a.ID),
		books,
	)
}

func (h *Handler) recent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	const page = recentLimit
	var books []*ent.Book
	seen := map[uint32]bool{}
	for offset := 0; len(books) < recentLimit; offset += page {
		files, err := h.client.MediaFile.Query().
			Where(mediafile.BookKindEQ(mediafile.BookKindEbook), mediafile.HasBook()).
			Order(ent.Desc(mediafile.FieldCreateTime), ent.Desc(mediafile.FieldID)).
			Limit(page).Offset(offset).
			WithBook(func(q *ent.BookQuery) {
				q.WithContributions(withAuthor).WithMediaFiles(ebookFiles)
			}).
			All(ctx)
		if err != nil {
			h.fail(w, r, "listing recent OPDS books failed", err)
			return
		}
		for _, mf := range files {
			bk := mf.Edges.Book
			if seen[bk.ID] || len(books) == recentLimit {
				continue
			}
			seen[bk.ID] = true
			books = append(books, bk)
		}
		if len(files) < page {
			break
		}
	}
	h.sendAcquisition(
		w,
		r,
		"urn:streamline:opds:recent",
		"Recently added",
		"/opds/recent",
		books,
	)
}

func (h *Handler) searchDescription(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", openSearchType)
	if _, err := w.Write([]byte(openSearchDescription)); err != nil {
		slog.ErrorContext(
			r.Context(),
			"writing OpenSearch description failed",
			"error",
			err,
		)
	}
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		h.sendAcquisition(
			w,
			r,
			"urn:streamline:opds:search",
			"Search",
			"/opds/search",
			nil,
		)
		return
	}
	books, err := h.ebookBooks().
		Where(book.Or(
			book.TitleContainsFold(q),
			book.HasContributionsWith(
				bookcontribution.HasAuthorWith(author.NameContainsFold(q)),
			),
		)).
		Order(ent.Asc(book.FieldSortTitle), ent.Asc(book.FieldTitle)).
		All(r.Context())
	if err != nil {
		h.fail(w, r, "searching OPDS books failed", err)
		return
	}
	h.sendAcquisition(
		w,
		r,
		"urn:streamline:opds:search",
		"Search: "+q,
		"/opds/search?q="+url.QueryEscape(q),
		books,
	)
}

// creatorRole matches the contributions an author is listed under in the
// catalog: the people who wrote the book, not those who coloured its pages.
func creatorRole() predicate.BookContribution {
	return bookcontribution.RoleIn(
		bookcontribution.RoleAuthor, bookcontribution.RoleWriter,
	)
}

func withAuthor(q *ent.BookContributionQuery) { q.WithAuthor() }

// atomMakers are the roles an entry credits.
var atomMakers = []bookcontribution.Role{
	bookcontribution.RoleAuthor,
	bookcontribution.RoleWriter,
	bookcontribution.RoleArtist,
	bookcontribution.RoleColorist,
	bookcontribution.RoleCover,
}

func hasEbookFile() predicate.Book {
	return book.HasMediaFilesWith(mediafile.BookKindEQ(mediafile.BookKindEbook))
}

func ebookFiles(q *ent.MediaFileQuery) {
	q.Where(mediafile.BookKindEQ(mediafile.BookKindEbook))
}

func (h *Handler) ebookBooks() *ent.BookQuery {
	return h.client.Book.Query().
		Where(hasEbookFile()).
		WithContributions(withAuthor).
		WithMediaFiles(ebookFiles)
}

func (h *Handler) sendAcquisition(
	w http.ResponseWriter,
	r *http.Request,
	id, title, self string,
	books []*ent.Book,
) {
	f := &feed{
		ID:    id,
		Title: title,
		Links: []link{
			selfLink(self, kindAcquisition),
			{Rel: "start", Href: "/opds", Type: navigationType},
		},
	}
	for _, bk := range books {
		e := bookEntry(bk)
		if e == nil {
			continue
		}
		f.Entries = append(f.Entries, *e)
		f.Updated = newest(f.Updated, bk.UpdateTime)
	}
	h.send(w, r, kindAcquisition, f)
}

func bookEntry(bk *ent.Book) *entry {
	best := bestFile(bk.Edges.MediaFiles)
	if best == nil {
		return nil
	}
	e := &entry{
		ID:      fmt.Sprintf("urn:streamline:book:%d", bk.ID),
		Title:   bk.Title,
		Updated: atomTime(bk.UpdateTime),
		Links: []link{
			{
				Rel: relAcquisition,
				Href: fmt.Sprintf(
					"/opds/download/%d/%s", bk.ID, strings.ToLower(best.Quality),
				),
				Type: ebookContentType(best.Quality),
			},
			{
				Rel:  relImage,
				Href: fmt.Sprintf("/opds/cover/%d", bk.ID),
				Type: "image/jpeg",
			},
		},
	}
	for _, c := range bk.Edges.Contributions {
		if c.Edges.Author != nil && slices.Contains(atomMakers, c.Role) &&
			!slices.ContainsFunc(e.Authors, func(a atomAuthor) bool {
				return a.Name == c.Edges.Author.Name
			}) {
			e.Authors = append(e.Authors, atomAuthor{Name: c.Edges.Author.Name})
		}
	}
	if bk.Overview != "" {
		e.Content = &content{Type: "text", Text: bk.Overview}
	}
	return e
}

func bestFile(files []*ent.MediaFile) *ent.MediaFile {
	rank := func(format string) int {
		if i := slices.Index(config.EbookFormats, strings.ToUpper(format)); i >= 0 {
			return i
		}
		return len(config.EbookFormats)
	}
	var best *ent.MediaFile
	for _, f := range files {
		if best == nil || rank(f.Quality) < rank(best.Quality) {
			best = f
		}
	}
	return best
}

func newest(cur atomTime, t time.Time) atomTime {
	if t.After(time.Time(cur)) {
		return atomTime(t)
	}
	return cur
}

func (h *Handler) send(
	w http.ResponseWriter,
	r *http.Request,
	kind feedKind,
	f *feed,
) {
	if err := writeFeed(w, kind, f); err != nil {
		slog.ErrorContext(r.Context(), "writing OPDS feed failed", "error", err)
	}
}

func (h *Handler) fail(
	w http.ResponseWriter,
	r *http.Request,
	msg string,
	err error,
) {
	slog.ErrorContext(r.Context(), msg, "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
