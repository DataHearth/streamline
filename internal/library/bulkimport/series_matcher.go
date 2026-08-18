package bulkimport

import (
	"math"

	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/metadata"
)

const showCandidateLimit = 5

// ShowClassification is the series analogue of Classification.
type ShowClassification struct {
	Kind             entimportscanshow.Classification
	TVDBID           uint32
	ExistingTvshowID uint32
	Candidates       []schema.ScannedShowCandidate
}

// ClassifyShow ranks TVDB results for a parsed folder into one of the four
// buckets. trackedByTVDB maps tvdb_id → tracked tvshow id; a candidate already
// in that set wins over confirmed/ambiguous because linking to the existing
// show beats creating a duplicate (mirrors Classify for movies).
func ClassifyShow(
	title string, year uint16,
	hits []metadata.TVResult, trackedByTVDB map[uint32]uint32,
) ShowClassification {
	if len(hits) == 0 {
		return ShowClassification{Kind: entimportscanshow.ClassificationUnmatched}
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
			return ShowClassification{
				Kind:             entimportscanshow.ClassificationExisting,
				TVDBID:           c.TVDBID,
				ExistingTvshowID: id,
				Candidates:       cands,
			}
		}
	}

	top := hits[0]
	if year != 0 && top.Year == year && library.TitleMatches(title, top.Title) {
		return ShowClassification{
			Kind:       entimportscanshow.ClassificationConfirmed,
			TVDBID:     top.TVDBID,
			Candidates: cands[:1],
		}
	}

	return ShowClassification{
		Kind:       entimportscanshow.ClassificationAmbiguous,
		Candidates: cands,
	}
}

// BuildShowParams turns a classified folder into the row to persist for review.
func BuildShowParams(
	folder string,
	p library.ParseResult,
	c ShowClassification,
	fileCount int,
) db.CreateImportScanShowParams {
	params := db.CreateImportScanShowParams{
		FolderPath:     folder,
		ParsedTitle:    p.Title,
		Classification: c.Kind,
		Candidates:     c.Candidates,
		FileCount:      clampFileCount(fileCount),
	}
	if p.Year != 0 {
		year := p.Year
		params.ParsedYear = &year
	}
	if c.TVDBID != 0 {
		id := c.TVDBID
		params.TVDBID = &id
	}
	if c.ExistingTvshowID != 0 {
		id := c.ExistingTvshowID
		params.ExistingTvshowID = &id
	}
	return params
}

// clampFileCount saturates a folder's video-file count into the uint16 that
// mirrors the ent column. A wrapping conversion would turn a pathological
// 65 536-file folder into "0 files" in the review UI; saturating keeps the
// row readable and truthful about being at the ceiling. The early-return
// shape is what gosec's range analysis accepts.
func clampFileCount(n int) uint16 {
	if n <= 0 {
		return 0
	}
	if n >= math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(n)
}
