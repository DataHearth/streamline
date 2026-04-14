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

// Classify decides how a parsed filename + TMDB hits + existing-library lookup
// fall into one of the four buckets. Existing-row collision wins over confirmed
// or ambiguous because attaching to an existing row beats creating a duplicate.
func Classify(
	parsed library.ParseResult,
	hits []metadata.MovieResult,
	alreadyAdded map[uint32]uint32,
) Classification {
	if len(hits) == 0 {
		return Classification{Kind: entimportscanfile.ClassificationUnmatched}
	}

	cands := topNCandidates(hits, pickerCandidateLimit)

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

	if parsed.Year != 0 && cands[0].Year == parsed.Year &&
		library.TitleMatches(parsed.Title, cands[0].Title) {
		return Classification{
			Kind:       entimportscanfile.ClassificationConfirmed,
			TMDBID:     cands[0].TMDBID,
			Candidates: cands[:1],
		}
	}

	return Classification{
		Kind:       entimportscanfile.ClassificationAmbiguous,
		Candidates: cands,
	}
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
