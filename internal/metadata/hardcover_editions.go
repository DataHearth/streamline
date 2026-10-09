package metadata

import (
	"slices"
	"strings"
	"time"

	"github.com/datahearth/streamline/internal/utils/numeric"
)

const (
	// maxBookEditions caps the editions stored per book.
	maxBookEditions = 40
)

// RawEdition is an edition as the batch query returns it, before selection.
type RawEdition struct {
	EditionRecord
	ReadingFormatID int
	ReleaseDate     *time.Time
}

type editionKey struct {
	language, publisher, format string
}

type langFormat struct {
	language, format string
}

// SelectEditions reduces the raw editions of one book to the stored set: the
// most popular edition per (language, publisher, format), at most
// maxBookEditions of them with the best of every (language, format) pair
// always kept. digital and physical are the two popularity-ordered pools of
// the query and earliest is the first-released edition, which names the
// original language.
//
// Hardcover has no CBZ or CBR concept, and comic volumes are almost always
// listed as paperback or hardcover only, so a physical edition counts as an
// ebook candidate for a language that has no digital ebook of its own.
// bookYear fills an edition with no year of its own.
func SelectEditions(
	digital, physical []RawEdition,
	earliest *RawEdition,
	bookYear uint16,
) (editions []EditionRecord, originalLanguage string) {
	pool := make([]RawEdition, 0, len(digital)+len(physical)+1)
	seen := map[uint32]bool{}
	add := func(e RawEdition) {
		if e.Language == "" || seen[e.HardcoverID] {
			return
		}
		seen[e.HardcoverID] = true
		pool = append(pool, e)
	}
	for _, e := range digital {
		add(e)
	}
	for _, e := range physical {
		add(e)
	}
	if earliest != nil {
		add(*earliest)
		originalLanguage = earliest.Language
	}

	ebookLangs := map[string]bool{}
	for _, e := range pool {
		if e.ReadingFormatID == hcFormatEbook {
			ebookLangs[e.Language] = true
		}
	}

	winners := map[editionKey]RawEdition{}
	for _, e := range pool {
		format, ok := candidateFormat(e, ebookLangs)
		if !ok {
			continue
		}
		e.Format = format
		if e.Year == 0 {
			e.Year = editionYear(e, bookYear)
		}
		k := editionKey{e.Language, e.Publisher, format}
		if cur, held := winners[k]; !held || beats(e, cur) {
			winners[k] = e
		}
	}

	ranked := make([]RawEdition, 0, len(winners))
	for _, e := range winners {
		ranked = append(ranked, e)
	}
	slices.SortFunc(ranked, func(a, b RawEdition) int {
		if a.Popularity != b.Popularity {
			if a.Popularity > b.Popularity {
				return -1
			}
			return 1
		}
		return int(a.HardcoverID) - int(b.HardcoverID)
	})

	keep := map[uint32]bool{}
	best := map[langFormat]bool{}
	for _, e := range ranked {
		lf := langFormat{e.Language, e.Format}
		if !best[lf] {
			best[lf] = true
			keep[e.HardcoverID] = true
		}
	}
	for _, e := range ranked {
		if len(keep) >= maxBookEditions {
			break
		}
		keep[e.HardcoverID] = true
	}

	originalByFormat := map[string]uint32{}
	for _, e := range ranked {
		if !keep[e.HardcoverID] {
			continue
		}
		if originalLanguage != "" && e.Language == originalLanguage {
			if _, taken := originalByFormat[e.Format]; !taken {
				originalByFormat[e.Format] = e.HardcoverID
			}
		}
	}
	for _, e := range ranked {
		if !keep[e.HardcoverID] {
			continue
		}
		rec := e.EditionRecord
		rec.Original = originalByFormat[rec.Format] == rec.HardcoverID
		editions = append(editions, rec)
	}
	return editions, originalLanguage
}

func candidateFormat(e RawEdition, ebookLangs map[string]bool) (string, bool) {
	switch e.ReadingFormatID {
	case hcFormatAudiobook:
		return FormatAudiobook, true
	case hcFormatEbook:
		return FormatEbook, true
	}
	if ebookLangs[e.Language] {
		return "", false
	}
	return FormatEbook, true
}

func editionYear(e RawEdition, bookYear uint16) uint16 {
	if e.ReleaseDate != nil {
		return numeric.SaturateU16(e.ReleaseDate.Year())
	}
	return bookYear
}

// beats reports whether a outranks b for one (language, publisher, format)
// key: more popular wins, the lower id breaks a tie.
func beats(a, b RawEdition) bool {
	if a.Popularity != b.Popularity {
		return a.Popularity > b.Popularity
	}
	return a.HardcoverID < b.HardcoverID
}

// cleanPublisher normalises the publisher part of the winner key.
func cleanPublisher(s string) string {
	return strings.TrimSpace(s)
}
