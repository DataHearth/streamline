package library

import (
	"path/filepath"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

const (
	slotEbook     = "ebook"
	slotAudiobook = "audiobook"
)

// NamingForBook is what the template of one slot reads off a book: the slot
// edition's title and language (the book's own when the slot has none), the
// book's makers, its first-published year, and the series it is a volume of.
// The book is expected with its slot editions and series loaded.
func NamingForBook(b *ent.Book, kind string) BookNaming {
	n := BookNaming{Author: b.AuthorName, Title: b.Title}
	ed := b.Edges.EbookEdition
	if kind == slotAudiobook {
		ed = b.Edges.AudiobookEdition
	}
	if ed != nil {
		n.Title, n.Language = ed.Title, ed.Language
	}
	switch {
	case b.ReleaseYear != nil:
		n.Year = *b.ReleaseYear
	case b.ReleaseDate != nil:
		n.Year = numeric.SaturateU16(b.ReleaseDate.Year())
	}
	if s := b.Edges.Series; s != nil {
		n.Series, n.Volume = s.Title, b.SeriesPosition
	}
	return n
}

// BookTemplate picks the naming template of one slot: the series template for
// the ebook of a series volume, else the slot's own.
func BookTemplate(lib config.LibraryConfig, b *ent.Book, kind string) string {
	switch {
	case kind == slotAudiobook:
		return lib.AudiobookNaming
	case b.Edges.Series != nil:
		return lib.BookSeriesNaming
	}
	return lib.EbookNaming
}

// BookRoot is the library root of one slot.
func BookRoot(lib config.LibraryConfig, kind string) string {
	if kind == slotAudiobook {
		return lib.AudiobookPath
	}
	return lib.EbookPath
}

// BookDestination renders the naming template under root, one sanitised
// segment per "/", and refuses a result that escapes root.
func BookDestination(root, naming string, n BookNaming) (string, error) {
	segments := strings.Split(ApplyTemplate(naming, BuildBookVars(n)), "/")
	for i, seg := range segments {
		segments[i] = SanitizePath(seg)
	}
	dest := filepath.Join(root, filepath.Join(segments...))
	if !PathUnderRoot(dest, root) {
		return "", ErrUnsafePath
	}
	return dest, nil
}
