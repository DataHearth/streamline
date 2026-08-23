package bulkimport

import (
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/metadata"
)

const pickerCandidateLimit = 5

// Classification holds the matcher's decision for a single scanned file.
type Classification struct {
	Kind            entimportscanfile.Classification
	Candidates      []schema.ScannedCandidate
	TMDBID          uint32
	ExistingMovieID uint32
}

// Classify decides how a path + parsed filename + TMDB hits + existing-library
// lookup fall into one of the four buckets. Existing-row collision wins over
// confirmed or ambiguous because attaching to an existing row beats creating a
// duplicate.
func Classify(
	path string,
	parsed library.ParseResult,
	hits []metadata.MovieResult,
	alreadyAdded map[uint32]uint32,
) Classification {
	// An id Streamline's own naming template wrote into the path outranks
	// anything re-derived from the filename: the path was rendered *from* that
	// id, so a title parsed back out of it can only lose information.
	if embedded := library.ParseEmbeddedIDs(path).TMDB; embedded != 0 {
		return classifyByTMDBID(embedded, hits, alreadyAdded)
	}

	if len(hits) == 0 {
		return Classification{Kind: entimportscanfile.ClassificationUnmatched}
	}

	cands := topNCandidates(
		rankMovieHits(hits, parsed.Title, parsed.Year),
		pickerCandidateLimit,
	)

	for _, c := range cands {
		if movieID, hit := alreadyAdded[c.TMDBID]; hit {
			return Classification{
				Kind:            entimportscanfile.ClassificationExisting,
				TMDBID:          c.TMDBID,
				ExistingMovieID: movieID,
				Candidates:      cands,
			}
		}
	}

	// Scan every candidate, not only the top one: TMDB frequently ranks a
	// same-titled decoy first ("Fantasia" 1940 above "Fantasia 2000"), and the
	// year is what tells them apart.
	if match, ok := soleMatch(cands, parsed.Title, parsed.Year); ok {
		return Classification{
			Kind:       entimportscanfile.ClassificationConfirmed,
			TMDBID:     match.TMDBID,
			Candidates: []schema.ScannedCandidate{match},
		}
	}

	return Classification{
		Kind:       entimportscanfile.ClassificationAmbiguous,
		Candidates: cands,
	}
}

// soleMatch returns the one candidate the parsed title and year single out, if
// there is exactly one. With a year it must agree; without one, a single
// title match is enough — insisting on a year is what left every year-less
// name ambiguous.
func soleMatch(
	cands []schema.ScannedCandidate,
	title string,
	year uint16,
) (schema.ScannedCandidate, bool) {
	var titleMatches []schema.ScannedCandidate
	for _, c := range cands {
		if library.TitleMatches(title, c.Title) {
			titleMatches = append(titleMatches, c)
		}
	}
	if len(titleMatches) == 0 {
		return schema.ScannedCandidate{}, false
	}
	if year != 0 {
		var byYear []schema.ScannedCandidate
		for _, c := range titleMatches {
			if c.Year == year {
				byYear = append(byYear, c)
			}
		}
		if len(byYear) == 1 {
			return byYear[0], true
		}
		// A year that agrees with nothing is more likely mis-parsed than
		// authoritative, so fall through to the title-only decision.
	}
	if len(titleMatches) == 1 {
		return titleMatches[0], true
	}
	return schema.ScannedCandidate{}, false
}

// classifyByTMDBID resolves a path-embedded id without consulting the parsed
// title. The id decides; the hits only supply a display title when one of them
// happens to be the same movie.
func classifyByTMDBID(
	tmdbID uint32,
	hits []metadata.MovieResult,
	alreadyAdded map[uint32]uint32,
) Classification {
	var cands []schema.ScannedCandidate
	for _, h := range hits {
		if h.TMDBID == tmdbID {
			cands = append(cands, schema.ScannedCandidate{
				TMDBID: h.TMDBID, Title: h.Title, Year: h.Year,
			})
			break
		}
	}
	if movieID, hit := alreadyAdded[tmdbID]; hit {
		return Classification{
			Kind:            entimportscanfile.ClassificationExisting,
			TMDBID:          tmdbID,
			ExistingMovieID: movieID,
			Candidates:      cands,
		}
	}
	return Classification{
		Kind:       entimportscanfile.ClassificationConfirmed,
		TMDBID:     tmdbID,
		Candidates: cands,
	}
}

// rankMovieHits reorders TMDB's results so title and year agreement outrank
// provider relevance, then leaves the provider's order to break ties.
func rankMovieHits(
	hits []metadata.MovieResult,
	title string,
	year uint16,
) []metadata.MovieResult {
	return rankByScore(hits, func(h metadata.MovieResult) int {
		return matchScore(title, year, h.Title, nil, h.Year)
	})
}

func topNCandidates(hits []metadata.MovieResult, n int) []schema.ScannedCandidate {
	if len(hits) > n {
		hits = hits[:n]
	}
	out := make([]schema.ScannedCandidate, 0, len(hits))
	for _, h := range hits {
		out = append(out, schema.ScannedCandidate{
			TMDBID: h.TMDBID,
			Title:  h.Title,
			Year:   h.Year,
		})
	}
	return out
}
