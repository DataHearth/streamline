package music

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/metadatachange"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
)

var albumKinds = []metadatachange.Kind{
	metadatachange.KindEditionRemoved,
	metadatachange.KindEditionMerged,
	metadatachange.KindEditionPreferred,
	metadatachange.KindTracksUnlisted,
	metadatachange.KindAlbumRemoved,
	metadatachange.KindAlbumMerged,
}

func albumOwner(
	id uint32,
) db.ChangeOwner {
	return db.ChangeOwner{Type: db.ChangeOwnerAlbum, ID: id}
}

func artistOwner(
	id uint32,
) db.ChangeOwner {
	return db.ChangeOwner{Type: db.ChangeOwnerArtist, ID: id}
}

func countFiles(a *ent.Album) int {
	n := 0
	for _, t := range a.Edges.Tracks {
		n += len(db.PresentFiles(t.Edges.MediaFiles))
	}
	return n
}

// keptDetails is what a refresh writes over an album it must leave as it is:
// the album's own facts, its genres kept, and no tracks, which leaves the
// stored ones alone. Writing it stamps the album fetched, so the backfill
// does not ask for it again in the same run.
func keptDetails(a *ent.Album) *metadata.ReleaseGroupDetails {
	d := storedDetails(a)
	if a.Genres != nil {
		d.Genres = a.Genres
	}
	return d
}

func editionSummary(e metadata.ReleaseEdition) map[string]any {
	return map[string]any{
		"mbid":    e.MBID,
		"title":   e.Title,
		"country": e.Country,
		"date":    e.Date,
		"formats": e.Formats,
		"tracks":  e.Tracks,
	}
}

func storedEdition(a *ent.Album) map[string]any {
	return map[string]any{
		"mbid":    cmpOr(a.EditionMbid, a.ReleaseMbid),
		"title":   a.Title,
		"country": a.Country,
		"date":    a.EditionDate,
		"tracks":  len(a.Edges.Tracks),
	}
}

func findEdition(editions []metadata.ReleaseEdition, mbid string) map[string]any {
	i := slices.IndexFunc(
		editions,
		func(e metadata.ReleaseEdition) bool { return e.MBID == mbid },
	)
	if i < 0 {
		return map[string]any{"mbid": mbid}
	}
	return editionSummary(editions[i])
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// reviewAlbum checks a refreshed album that holds files against what it
// follows, records what an admin has to decide, and reports whether the
// fetched release data can be written over it.
func (s *Service) reviewAlbum(
	ctx context.Context,
	a *ent.Album,
	d *metadata.ReleaseGroupDetails,
) (bool, error) {
	owner := albumOwner(a.ID)
	files := countFiles(a)
	if d.MBID != "" && d.MBID != a.Mbid {
		target := map[string]any{"mbid": d.MBID, "title": d.Title}
		other, err := s.db.FindAlbumByMBID(ctx, d.MBID)
		if err != nil {
			return false, err
		}
		if other != nil {
			target["id"] = other.ID
		}
		return false, s.db.ReportChange(
			ctx,
			owner,
			metadatachange.KindAlbumMerged,
			d.MBID,
			map[string]any{"files": files, "target": target},
		)
	}
	if err := s.db.ClearChange(
		ctx,
		owner,
		metadatachange.KindAlbumMerged,
		metadatachange.KindAlbumRemoved,
	); err != nil {
		return false, err
	}

	followed := cmpOr(a.EditionMbid, a.ReleaseMbid)
	if followed != "" && !d.EditionListed {
		to, err := s.metadata.ResolveRelease(ctx, followed)
		switch {
		case errors.Is(err, metadata.ErrNotFound) || (err == nil && to == followed):
			return false, s.db.ReportChange(
				ctx,
				owner,
				metadatachange.KindEditionRemoved,
				followed,
				map[string]any{"files": files, "current": storedEdition(a)},
			)
		case err != nil:
			return false, err
		default:
			return false, s.db.ReportChange(
				ctx,
				owner,
				metadatachange.KindEditionMerged,
				to,
				map[string]any{
					"files":    files,
					"current":  storedEdition(a),
					"upstream": findEdition(d.Editions, to),
				},
			)
		}
	}
	if err := s.db.ClearChange(
		ctx,
		owner,
		metadatachange.KindEditionRemoved,
		metadatachange.KindEditionMerged,
	); err != nil {
		return false, err
	}

	if a.EditionMbid == "" && d.Canonical != "" && d.Canonical != d.ReleaseMBID {
		return true, s.db.ReportChange(
			ctx,
			owner,
			metadatachange.KindEditionPreferred,
			d.Canonical,
			map[string]any{
				"files":    files,
				"current":  findEdition(d.Editions, d.ReleaseMBID),
				"upstream": findEdition(d.Editions, d.Canonical),
			},
		)
	}
	return true, s.db.ClearChange(ctx, owner, metadatachange.KindEditionPreferred)
}

// reviewTracks records the tracks holding a file that the release just
// written no longer lists: the hydration kept their rows for the files.
func (s *Service) reviewTracks(
	ctx context.Context,
	albumID uint32,
	d *metadata.ReleaseGroupDetails,
) error {
	a, err := s.db.FindAlbumByID(ctx, albumID)
	if err != nil {
		return err
	}
	listed := make(map[string]bool, len(d.Tracks))
	for _, t := range d.Tracks {
		listed[t.ReleaseTrackMBID] = true
	}
	var (
		items []map[string]any
		ids   []string
	)
	for _, t := range a.Edges.Tracks {
		if t.ReleaseTrackMbid == "" || listed[t.ReleaseTrackMbid] ||
			len(db.PresentFiles(t.Edges.MediaFiles)) == 0 {
			continue
		}
		items = append(items, map[string]any{
			"id":    t.ID,
			"label": fmt.Sprintf("%d·%02d", t.Disc, t.Position),
			"title": t.Title,
		})
		ids = append(ids, strconv.FormatUint(uint64(t.ID), 10))
	}
	owner := albumOwner(albumID)
	if len(items) == 0 {
		return s.db.ClearChange(ctx, owner, metadatachange.KindTracksUnlisted)
	}
	return s.db.ReportChange(
		ctx,
		owner,
		metadatachange.KindTracksUnlisted,
		strings.Join(ids, ","),
		map[string]any{"files": countFiles(a), "items": items},
	)
}

// reviewArtist records an artist MusicBrainz merged or deleted and reports
// whether the fetched details are the artist's own and can be written.
func (s *Service) reviewArtist(
	ctx context.Context,
	row *ent.Artist,
	details *metadata.ArtistDetails,
	err error,
) (bool, error) {
	owner := artistOwner(row.ID)
	if errors.Is(err, metadata.ErrNotFound) {
		return false, s.db.ReportChange(
			ctx,
			owner,
			metadatachange.KindArtistRemoved,
			"",
			map[string]any{"albums": len(row.Edges.Albums)},
		)
	}
	if err != nil {
		return false, err
	}
	if details.MBID != "" && details.MBID != row.Mbid {
		target := map[string]any{"mbid": details.MBID, "title": details.Name}
		other, err := s.db.FindArtistByMBID(ctx, details.MBID)
		if err != nil {
			return false, err
		}
		if other != nil {
			target["id"] = other.ID
		}
		return false, s.db.ReportChange(
			ctx,
			owner,
			metadatachange.KindArtistMerged,
			details.MBID,
			map[string]any{"albums": len(row.Edges.Albums), "target": target},
		)
	}
	return true, s.db.ClearChange(ctx, owner,
		metadatachange.KindArtistRemoved, metadatachange.KindArtistMerged)
}

// FollowChange applies an album's or an artist's pending change the way
// MusicBrainz has it now, and forgets the change.
func (s *Service) FollowChange(ctx context.Context, c *ent.MetadataChange) error {
	owner := db.ChangeOwnerOf(c)
	switch c.Kind {
	case metadatachange.KindEditionRemoved, metadatachange.KindEditionPreferred:
		return s.SetAlbumEdition(ctx, owner.ID, "")
	case metadatachange.KindEditionMerged:
		return s.SetAlbumEdition(ctx, owner.ID, c.Fingerprint)
	case metadatachange.KindTracksUnlisted:
		var ids []uint32
		for part := range strings.SplitSeq(c.Fingerprint, ",") {
			id, err := strconv.ParseUint(part, 10, 32)
			if err != nil {
				return err
			}
			ids = append(ids, uint32(id))
		}
		if err := s.db.DeleteAlbumTracks(ctx, owner.ID, ids); err != nil {
			return err
		}
		return s.db.ClearChange(ctx, owner, metadatachange.KindTracksUnlisted)
	case metadatachange.KindAlbumMerged:
		dropped, err := s.db.RepointAlbum(ctx, owner.ID, c.Fingerprint)
		if errors.Is(err, db.ErrAlbumInUse) {
			return fmt.Errorf("%w: %w", db.ErrChangeConflict, err)
		}
		if err != nil {
			return err
		}
		if dropped != 0 {
			if err := s.images.Remove(coverKind, dropped); err != nil {
				return err
			}
		}
		if err := s.db.ClearChange(ctx, owner, albumKinds...); err != nil {
			return err
		}
		_, err = s.hydrateAlbum(ctx, owner.ID, hydrateFollow)
		return err
	case metadatachange.KindArtistMerged:
		kept, err := s.db.RepointArtist(ctx, owner.ID, c.Fingerprint)
		if err != nil {
			return err
		}
		if kept == owner.ID {
			if err := s.db.ClearChange(
				ctx,
				owner,
				metadatachange.KindArtistMerged,
			); err != nil {
				return err
			}
		}
		_, err = s.RefreshOne(ctx, kept)
		return err
	}
	return db.ErrNotFollowable
}
