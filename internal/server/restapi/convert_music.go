package restapi

import (
	"github.com/datahearth/streamline/ent"
)

// toAPIImportScanAlbum maps a row. artistIDs resolves artist_mbid to the library
// artist, read live so it cannot go stale between scan and commit.
func toAPIImportScanAlbum(
	al *ent.ImportScanAlbum,
	artistIDs map[string]uint32,
) ImportScanAlbum {
	out := ImportScanAlbum{
		Size:            al.Size,
		Format:          optString(al.Format),
		Id:              al.ID,
		FolderPath:      al.FolderPath,
		Classification:  ImportScanAlbumClassification(al.Classification),
		FileCount:       al.FileCount,
		Decision:        ImportScanAlbumDecision(al.Decision),
		Outcome:         ImportScanAlbumOutcome(al.Outcome),
		ExistingAlbumId: al.ExistingAlbumID,
		CreatedAlbumId:  al.CreatedAlbumID,
	}
	for dst, src := range map[**string]string{
		&out.TaggedArtist:             al.TaggedArtist,
		&out.TaggedAlbum:              al.TaggedAlbum,
		&out.ReleaseGroupMbid:         al.ReleaseGroupMbid,
		&out.ArtistMbid:               al.ArtistMbid,
		&out.DecisionReleaseGroupMbid: al.DecisionReleaseGroupMbid,
		&out.OutcomeMessage:           al.OutcomeMessage,
	} {
		if src != "" {
			v := src
			*dst = &v
		}
	}
	if al.TaggedYear != 0 {
		y := al.TaggedYear
		out.TaggedYear = &y
	}
	if id, ok := artistIDs[al.ArtistMbid]; ok && al.ArtistMbid != "" {
		out.ArtistId = &id
	}
	if len(al.Candidates) > 0 {
		cands := make([]ImportScanAlbumCandidate, 0, len(al.Candidates))
		for _, c := range al.Candidates {
			cand := ImportScanAlbumCandidate{
				ReleaseGroupMbid: c.ReleaseGroupMBID,
				Title:            c.Title,
			}
			if c.ArtistMBID != "" {
				v := c.ArtistMBID
				cand.ArtistMbid = &v
			}
			if c.Artist != "" {
				v := c.Artist
				cand.Artist = &v
			}
			if c.Year != 0 {
				y := c.Year
				cand.Year = &y
			}
			if c.Type != "" {
				t := MusicAlbumType(c.Type)
				cand.Type = &t
			}
			cands = append(cands, cand)
		}
		out.Candidates = &cands
	}
	ct := al.CreateTime
	out.CreatedAt = &ct
	ut := al.UpdateTime
	out.UpdatedAt = &ut
	return out
}
