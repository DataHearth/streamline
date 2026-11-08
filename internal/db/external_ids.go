package db

import (
	"context"
	"fmt"
	"strconv"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/externalid"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/tvshow"
)

// ExternalIDs are the other sites' ids a title's provider maps it to, kept in
// the external_ids table. The provider's own id is not among them: a movie's
// TMDB id is movies.tmdb_id and a series' TVDB id tv_shows.tvdb_id, so TMDBID
// is only ever set on a series.
//
// Every metadata write replaces a title's set with what the provider
// answered, empty included, so a re-identified title never keeps the previous
// one's ids.
type ExternalIDs struct {
	TMDBID     uint32
	IMDbID     string
	WikidataID string
}

// CastOwner names the title kind here as it does for credits: ExternalID has
// the same two alternative owner edges.
func externalIDsOf(owner CastOwner, ownerID uint32) (predicate.ExternalID, error) {
	switch owner {
	case CastOwnerMovie:
		return externalid.HasMovieWith(movie.IDEQ(ownerID)), nil
	case CastOwnerSeries:
		return externalid.HasTvShowWith(tvshow.IDEQ(ownerID)), nil
	default:
		return nil, fmt.Errorf("%w: %q", errUnknownCastOwner, owner)
	}
}

// replaceExternalIDs writes a title's ids against a client the caller's
// transaction already holds (the creates and the metadata updates), for the
// same single-writer reason replaceCast takes one.
func replaceExternalIDs(
	ctx context.Context,
	cl *ent.Client,
	owner CastOwner,
	ownerID uint32,
	ids ExternalIDs,
) error {
	ownedBy, err := externalIDsOf(owner, ownerID)
	if err != nil {
		return err
	}
	if _, err := cl.ExternalID.Delete().Where(ownedBy).Exec(ctx); err != nil {
		return fmt.Errorf("delete external ids: %w", err)
	}

	values := map[externalid.Source]string{
		externalid.SourceImdb:     ids.IMDbID,
		externalid.SourceWikidata: ids.WikidataID,
	}
	if ids.TMDBID != 0 {
		values[externalid.SourceTmdb] = strconv.FormatUint(uint64(ids.TMDBID), 10)
	}
	creates := make([]*ent.ExternalIDCreate, 0, len(values))
	for source, value := range values {
		if value == "" {
			continue
		}
		b := cl.ExternalID.Create().SetSource(source).SetValue(value)
		switch owner {
		case CastOwnerMovie:
			b = b.SetMovieID(ownerID)
		case CastOwnerSeries:
			b = b.SetTvShowID(ownerID)
		}
		creates = append(creates, b)
	}
	if len(creates) == 0 {
		return nil
	}
	if _, err := cl.ExternalID.CreateBulk(creates...).Save(ctx); err != nil {
		return fmt.Errorf("create external ids: %w", err)
	}
	return nil
}

// TitleExternalIDs returns the ids stored for one title, zero-valued for a
// source it has none for.
func (db *DB) TitleExternalIDs(
	ctx context.Context,
	owner CastOwner,
	ownerID uint32,
) (ExternalIDs, error) {
	ownedBy, err := externalIDsOf(owner, ownerID)
	if err != nil {
		return ExternalIDs{}, err
	}
	rows, err := db.client.ExternalID.Query().Where(ownedBy).All(ctx)
	if err != nil {
		return ExternalIDs{}, fmt.Errorf("title external ids: %w", err)
	}
	var out ExternalIDs
	for _, r := range rows {
		switch r.Source {
		case externalid.SourceImdb:
			out.IMDbID = r.Value
		case externalid.SourceWikidata:
			out.WikidataID = r.Value
		case externalid.SourceTmdb:
			n, _ := strconv.ParseUint(r.Value, 10, 32)
			out.TMDBID = uint32(n)
		}
	}
	return out, nil
}
