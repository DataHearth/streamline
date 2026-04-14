package hygiene

import (
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/metadata"
)

const showCandidateLimit = 5

// showClassification is the series analogue of bulkimport.Classification.
type showClassification struct {
	Kind             entimportscanshow.Classification
	TVDBID           uint32
	ExistingTvshowID uint32
	Candidates       []schema.ScannedShowCandidate
}

// classifyShow ranks TVDB results for a parsed folder into one of the four
// buckets. trackedByTVDB maps tvdb_id → tracked tvshow id; a candidate already
// in that set wins over confirmed/ambiguous because linking to the existing
// show beats creating a duplicate (mirrors bulkimport.Classify for movies).
func classifyShow(
	title string, year uint16,
	hits []metadata.TVResult, trackedByTVDB map[uint32]uint32,
) showClassification {
	if len(hits) == 0 {
		return showClassification{Kind: entimportscanshow.ClassificationUnmatched}
	}

	cands := make([]schema.ScannedShowCandidate, 0, showCandidateLimit)
	for i, h := range hits {
		if i >= showCandidateLimit {
			break
		}
		cands = append(cands, schema.ScannedShowCandidate{
			TVDBID: h.TVDBID, Title: h.Title, Year: h.Year,
		})
	}

	for _, c := range cands {
		if id, ok := trackedByTVDB[c.TVDBID]; ok {
			return showClassification{
				Kind:             entimportscanshow.ClassificationExisting,
				TVDBID:           c.TVDBID,
				ExistingTvshowID: id,
				Candidates:       cands,
			}
		}
	}

	top := hits[0]
	if year != 0 && top.Year == year && library.TitleMatches(title, top.Title) {
		return showClassification{
			Kind:       entimportscanshow.ClassificationConfirmed,
			TVDBID:     top.TVDBID,
			Candidates: cands[:1],
		}
	}

	return showClassification{
		Kind:       entimportscanshow.ClassificationAmbiguous,
		Candidates: cands,
	}
}
