package metadata

import (
	"context"
	"time"
)

// Book kinds, as the classifier reports them.
const (
	BookKindNovel = "novel"
	BookKindBD    = "bd"
	BookKindComic = "comic"
	BookKindManga = "manga"
)

// Slot formats of a book edition.
const (
	FormatEbook     = "ebook"
	FormatAudiobook = "audiobook"
)

// Contribution roles. The first five are makers and belong to a book or a
// series; translators and narrators live on editions, and a series also keeps
// the translators of its volumes.
const (
	RoleAuthor     = "author"
	RoleWriter     = "writer"
	RoleArtist     = "artist"
	RoleColorist   = "colorist"
	RoleCover      = "cover"
	RoleTranslator = "translator"
	RoleNarrator   = "narrator"
)

// BookCredit is one maker of a book, from Hardcover's contributors.
type BookCredit struct {
	AuthorHardcoverID uint32
	Name              string
	ImageURL          string
	Role              string
}

// EditionRecord is the winning edition of one (language, publisher, format)
// triple of a book.
type EditionRecord struct {
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
	// TranslatorID is the Hardcover person behind Translator, so a series can
	// credit translators as people.
	TranslatorID uint32
	ISBN13       string
	ASIN         string
	Popularity   uint32
}

// BookSeriesRef is a series a book belongs to, with the book's position in it.
type BookSeriesRef struct {
	SeriesHardcoverID uint32
	Name              string
	Position          *float64
}

// BookRecord is one book as the batch query returns it.
type BookRecord struct {
	HardcoverID      uint32
	Title            string
	OriginalTitle    string
	Description      string
	ReleaseDate      *time.Time
	ReleaseYear      uint16
	Rating           float64
	UsersCount       uint32
	Compilation      bool
	CoverURL         string
	Genre            string
	Genres           []string
	Kind             string
	OriginalLanguage string
	Credits          []BookCredit
	Series           []BookSeriesRef
	Editions         []EditionRecord

	// creditsGap marks a record whose contributors blob could not name its
	// makers, so they are read through the join form.
	creditsGap bool
}

// SeriesVolumeRef is one numbered entry of a series skeleton.
type SeriesVolumeRef struct {
	Position        float64
	BookHardcoverID uint32
}

// SeriesRecord is a series skeleton: the row and the (position, book id) pairs
// of its numbered volumes. Volume data comes from the batch book query.
type SeriesRecord struct {
	HardcoverID       uint32
	Name              string
	Description       string
	Completed         bool
	PrimaryBooks      uint32
	Books             uint32
	AuthorHardcoverID uint32
	AuthorName        string
	AuthorImageURL    string
	Volumes           []SeriesVolumeRef
}

// BookLookupHit is one hit of the add-flow search, a book or a series.
type BookLookupHit struct {
	HardcoverID uint32
	Type        string // "book" | "series"
	Title       string
	Author      string
	Year        uint16
	Volumes     uint32
	Ongoing     *bool
	Genres      []string
	ImageURL    string
}

// BookSearchResult is a Hardcover book search hit, used to match scanned
// files.
type BookSearchResult struct {
	HardcoverID uint32
	Title       string
	Author      string
	Year        uint16
}

// BookProvider fetches book metadata. Implemented by *Hardcover.
type BookProvider interface {
	// SearchBooks is the scanner's title search.
	SearchBooks(ctx context.Context, query string) ([]BookSearchResult, error)
	// BookByISBN resolves an ISBN-13 to a Hardcover book id; 0 = not found.
	BookByISBN(ctx context.Context, isbn string) (uint32, error)
	// LookupBooks and LookupSeries are the add flow's search, one request
	// each, memoised for a few minutes per normalised query.
	LookupBooks(ctx context.Context, query string) ([]BookLookupHit, error)
	LookupSeries(ctx context.Context, query string) ([]BookLookupHit, error)
	// GetBooks returns the books Hardcover knows among ids, in the order
	// asked, one request per 20. Answers are memoised for ten minutes and must
	// not be mutated; an unknown id is simply absent.
	GetBooks(ctx context.Context, ids []uint32) ([]*BookRecord, error)
	// GetBooksFresh is GetBooks without the memo, for a refresh.
	GetBooksFresh(ctx context.Context, ids []uint32) ([]*BookRecord, error)
	// GetSeries returns the series skeleton in one request; nil when unknown.
	GetSeries(ctx context.Context, id uint32) (*SeriesRecord, error)
}
