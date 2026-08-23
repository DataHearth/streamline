package bulkimport

import (
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
	folder string,
	title string, year uint16,
	hits []metadata.TVResult, trackedByTVDB map[uint32]uint32,
) ShowClassification {
	// An id Streamline's own naming wrote into the folder outranks a title
	// re-derived from it (mirrors Classify for movies).
	if embedded := library.ParseEmbeddedIDs(folder).TVDB; embedded != 0 {
		return classifyShowByTVDBID(embedded, hits, trackedByTVDB)
	}

	if len(hits) == 0 {
		return ShowClassification{Kind: entimportscanshow.ClassificationUnmatched}
	}

	ranked := rankShowHits(hits, title, year)
	cands := make([]schema.ScannedShowCandidate, 0, showCandidateLimit)
	for i, h := range ranked {
		if i >= showCandidateLimit {
			break
		}
		cands = append(cands, schema.ScannedShowCandidate{
			TVDBID: h.TVDBID, Title: h.Title, Year: h.Year,
		})
	}

	// Series folders are conventionally named without a year ("Foundation",
	// "Breaking Bad"), so requiring one left every tidy library ambiguous. Match
	// on title across all candidates instead, and let a year — when the folder
	// carries one — break a tie between same-titled shows.
	//
	// Identification comes first and a tracked candidate is only consulted
	// afterwards. Scanning the candidates for a tracked id up front resolved the
	// ambiguity by whichever tracked show happened to rank higher: two library
	// rows named "Spiral (2005)" and "Spiral (2017)" both normalise to "Spiral",
	// and the 2005 folder was reported `existing` — with full confidence —
	// against the 2017 row.
	m, ok := soleShowMatch(ranked[:len(cands)], title, year)
	if !ok {
		return ShowClassification{
			Kind:       entimportscanshow.ClassificationAmbiguous,
			Candidates: cands,
		}
	}
	only := []schema.ScannedShowCandidate{
		{TVDBID: m.TVDBID, Title: m.Title, Year: m.Year},
	}
	if id, tracked := trackedByTVDB[m.TVDBID]; tracked {
		return ShowClassification{
			Kind:             entimportscanshow.ClassificationExisting,
			TVDBID:           m.TVDBID,
			ExistingTvshowID: id,
			Candidates:       only,
		}
	}
	return ShowClassification{
		Kind:       entimportscanshow.ClassificationConfirmed,
		TVDBID:     m.TVDBID,
		Candidates: only,
	}
}

// soleShowMatch returns the one candidate the folder name singles out, matching
// against TVDB aliases and translations as well as the display title — a romaji
// folder ("Kimetsu no Yaiba") only ever reaches its entry that way.
func soleShowMatch(
	hits []metadata.TVResult,
	title string,
	year uint16,
) (metadata.TVResult, bool) {
	var matches []metadata.TVResult
	for _, h := range hits {
		if library.TitleMatchesAny(title, h.Title, h.Aliases) {
			matches = append(matches, h)
		}
	}
	if len(matches) == 0 {
		return metadata.TVResult{}, false
	}
	if year != 0 {
		var byYear []metadata.TVResult
		for _, h := range matches {
			if h.Year == year {
				byYear = append(byYear, h)
			}
		}
		if len(byYear) == 1 {
			return byYear[0], true
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return metadata.TVResult{}, false
}

// classifyShowByTVDBID resolves a folder-embedded id without consulting the
// parsed title.
func classifyShowByTVDBID(
	tvdbID uint32,
	hits []metadata.TVResult,
	trackedByTVDB map[uint32]uint32,
) ShowClassification {
	var cands []schema.ScannedShowCandidate
	for _, h := range hits {
		if h.TVDBID == tvdbID {
			cands = append(cands, schema.ScannedShowCandidate{
				TVDBID: h.TVDBID, Title: h.Title, Year: h.Year,
			})
			break
		}
	}
	if id, ok := trackedByTVDB[tvdbID]; ok {
		return ShowClassification{
			Kind:             entimportscanshow.ClassificationExisting,
			TVDBID:           tvdbID,
			ExistingTvshowID: id,
			Candidates:       cands,
		}
	}
	return ShowClassification{
		Kind:       entimportscanshow.ClassificationConfirmed,
		TVDBID:     tvdbID,
		Candidates: cands,
	}
}

// rankShowHits reorders TVDB's results so title and year agreement outrank its
// own relevance, which ranks short titles badly.
func rankShowHits(
	hits []metadata.TVResult,
	title string,
	year uint16,
) []metadata.TVResult {
	return rankByScore(hits, func(h metadata.TVResult) int {
		return matchScore(title, year, h.Title, h.Aliases, h.Year)
	})
}

// BuildShowParams turns a classified folder into the row to persist for review.
func BuildShowParams(
	folder string,
	p library.ParseResult,
	c ShowClassification,
	fileCount uint16,
) db.CreateImportScanShowParams {
	params := db.CreateImportScanShowParams{
		FolderPath:     folder,
		ParsedTitle:    p.Title,
		Classification: c.Kind,
		Candidates:     c.Candidates,
		FileCount:      fileCount,
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
