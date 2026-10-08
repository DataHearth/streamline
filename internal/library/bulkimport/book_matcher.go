package bulkimport

import (
	"sort"
	"strings"

	entimportscanbook "github.com/datahearth/streamline/ent/importscanbook"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/metadata"
)

// scoreAuthorAgrees sits between a title prefix and an exact title: two books
// of one title by different authors must rank the right author first.
const scoreAuthorAgrees = 2

// BookClassification is the book analogue of ShowClassification.
type BookClassification struct {
	Kind              entimportscanbook.Classification
	BookHardcoverID   uint32
	AuthorHardcoverID uint32
	ExistingBookID    uint32
	Candidates        []schema.ScannedBookCandidate
}

// ClassifyBook ranks Hardcover search hits for a parsed title and author.
// indexed maps hardcover_id to a tracked book id; as with ClassifyShow, a
// sole match is checked against it only after identification, so a tracked
// book never wins by outranking the one the name actually singles out.
func ClassifyBook(
	title, author string,
	hits []metadata.BookSearchResult,
	indexed map[uint32]uint32,
) BookClassification {
	if len(hits) == 0 {
		return BookClassification{Kind: entimportscanbook.ClassificationUnmatched}
	}
	ranked := rankByScore(hits, func(h metadata.BookSearchResult) int {
		s := matchScore(title, 0, h.Title, nil, 0)
		if authorMatches(author, h.Author) {
			s += scoreAuthorAgrees
		}
		return s
	})
	if len(ranked) > pickerCandidateLimit {
		ranked = ranked[:pickerCandidateLimit]
	}
	cands := make([]schema.ScannedBookCandidate, 0, len(ranked))
	for _, h := range ranked {
		cands = append(cands, bookCandidate(h))
	}

	m, ok := soleBookMatch(ranked, title, author)
	if !ok {
		return BookClassification{
			Kind:       entimportscanbook.ClassificationAmbiguous,
			Candidates: cands,
		}
	}
	return classifyResolvedBook(
		m.HardcoverID, m.AuthorHardcoverID, bookCandidate(m), indexed,
	)
}

// classifyResolvedBook is the outcome once a single Hardcover book is known,
// whether by ISBN or by a sole title-and-author match.
func classifyResolvedBook(
	bookID, authorID uint32,
	cand schema.ScannedBookCandidate,
	indexed map[uint32]uint32,
) BookClassification {
	c := BookClassification{
		Kind:              entimportscanbook.ClassificationConfirmed,
		BookHardcoverID:   bookID,
		AuthorHardcoverID: authorID,
		Candidates:        []schema.ScannedBookCandidate{cand},
	}
	if id, tracked := indexed[bookID]; tracked {
		c.Kind = entimportscanbook.ClassificationExisting
		c.ExistingBookID = id
	}
	return c
}

func bookCandidate(h metadata.BookSearchResult) schema.ScannedBookCandidate {
	return schema.ScannedBookCandidate{
		BookHardcoverID:   h.HardcoverID,
		AuthorHardcoverID: h.AuthorHardcoverID,
		Title:             h.Title,
		Author:            h.Author,
		Year:              h.Year,
	}
}

func soleBookMatch(
	hits []metadata.BookSearchResult,
	title, author string,
) (metadata.BookSearchResult, bool) {
	var matches []metadata.BookSearchResult
	for _, h := range hits {
		if library.TitleMatches(title, h.Title) && authorMatches(author, h.Author) {
			matches = append(matches, h)
		}
	}
	if len(matches) != 1 {
		return metadata.BookSearchResult{}, false
	}
	return matches[0], true
}

// authorMatches compares names order-insensitively, since "Sanderson, Brandon"
// and "Brandon Sanderson" are one author. An unknown author matches nothing:
// a title alone does not single a book out.
func authorMatches(want, got string) bool {
	if strings.TrimSpace(want) == "" || strings.TrimSpace(got) == "" {
		return false
	}
	if library.TitleMatches(want, got) {
		return true
	}
	return sortedNameTokens(want) == sortedNameTokens(got)
}

func sortedNameTokens(name string) string {
	fields := strings.Fields(strings.NewReplacer(",", " ", ".", " ").Replace(
		strings.ToLower(name),
	))
	sort.Strings(fields)
	return strings.Join(fields, " ")
}
