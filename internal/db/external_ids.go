package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/externalid"
	"github.com/datahearth/streamline/ent/predicate"
)

// ExternalIDOwner names the kind of row a set of external ids hangs off.
// ExternalID carries one optional edge per kind and exactly one is ever set,
// the Credit and MediaEvent pattern, so the owner is named explicitly.
type ExternalIDOwner string

const (
	ExternalIDsOfMovie  ExternalIDOwner = "movie"
	ExternalIDsOfSeries ExternalIDOwner = "series"
	ExternalIDsOfArtist ExternalIDOwner = "artist"
	ExternalIDsOfAlbum  ExternalIDOwner = "album"
)

var errUnknownExternalIDOwner = errors.New("unknown external id owner")

// ExternalIDs maps a source (metadata.Source*) to that site's id for one
// row: what the row's own provider cross-references (TMDB for a movie, TVDB
// for a series, MusicBrainz for an artist or an album). The provider's own id
// is never among them; it is the row's column.
//
// A write replaces the owner's set with the map, empty values dropped, so a
// re-identified title never keeps the previous one's ids. A nil map is "not
// fetched this time" and leaves the stored set alone, which is how an album
// re-hydrated from its stored facts keeps its links.
type ExternalIDs map[string]string

func externalIDsOf(owner ExternalIDOwner, ownerID uint32) (predicate.ExternalID, error) {
	return externalIDsOfAny(owner, []uint32{ownerID})
}

func externalIDsOfAny(owner ExternalIDOwner, ownerIDs []uint32) (predicate.ExternalID, error) {
	switch owner {
	case ExternalIDsOfMovie:
		return externalid.MovieIDIn(ownerIDs...), nil
	case ExternalIDsOfSeries:
		return externalid.TvShowIDIn(ownerIDs...), nil
	case ExternalIDsOfArtist:
		return externalid.ArtistIDIn(ownerIDs...), nil
	case ExternalIDsOfAlbum:
		return externalid.AlbumIDIn(ownerIDs...), nil
	default:
		return nil, fmt.Errorf("%w: %q", errUnknownExternalIDOwner, owner)
	}
}

func ownerIDOf(owner ExternalIDOwner, r *ent.ExternalID) uint32 {
	switch owner {
	case ExternalIDsOfMovie:
		return r.MovieID
	case ExternalIDsOfSeries:
		return r.TvShowID
	case ExternalIDsOfArtist:
		return r.ArtistID
	default:
		return r.AlbumID
	}
}

// replaceExternalIDs writes an owner's ids against a client the caller's
// transaction already holds (the creates, the refreshes, the album
// hydration), for the same single-writer reason replaceCast takes one.
func replaceExternalIDs(
	ctx context.Context,
	cl *ent.Client,
	owner ExternalIDOwner,
	ownerID uint32,
	ids ExternalIDs,
) error {
	if ids == nil {
		return nil
	}
	ownedBy, err := externalIDsOf(owner, ownerID)
	if err != nil {
		return err
	}
	if _, err := cl.ExternalID.Delete().Where(ownedBy).Exec(ctx); err != nil {
		return fmt.Errorf("delete external ids: %w", err)
	}

	creates := make([]*ent.ExternalIDCreate, 0, len(ids))
	for name, value := range ids {
		if value == "" {
			continue
		}
		source := externalid.Source(name)
		if err := externalid.SourceValidator(source); err != nil {
			// A source the provider layer reads but the enum does not carry
			// is a programming error, not the provider's; it costs a link,
			// never the metadata write.
			slog.WarnContext(ctx, "dropping external id of an unknown source",
				"external_id.source", name, "error", err)
			continue
		}
		b := cl.ExternalID.Create().SetSource(source).SetValue(value)
		switch owner {
		case ExternalIDsOfMovie:
			b = b.SetMovieID(ownerID)
		case ExternalIDsOfSeries:
			b = b.SetTvShowID(ownerID)
		case ExternalIDsOfArtist:
			b = b.SetArtistID(ownerID)
		case ExternalIDsOfAlbum:
			b = b.SetAlbumID(ownerID)
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

// TitleExternalIDs returns the ids stored for one row, an empty map when it
// has none.
func (db *DB) TitleExternalIDs(
	ctx context.Context,
	owner ExternalIDOwner,
	ownerID uint32,
) (ExternalIDs, error) {
	ownedBy, err := externalIDsOf(owner, ownerID)
	if err != nil {
		return nil, err
	}
	rows, err := db.client.ExternalID.Query().Where(ownedBy).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("title external ids: %w", err)
	}
	out := make(ExternalIDs, len(rows))
	for _, r := range rows {
		out[string(r.Source)] = r.Value
	}
	return out, nil
}

// ExternalIDsFor returns the ids stored for many rows of one kind in one
// query, keyed by row id; a row with none is absent from the map.
func (db *DB) ExternalIDsFor(
	ctx context.Context,
	owner ExternalIDOwner,
	ownerIDs []uint32,
) (map[uint32]ExternalIDs, error) {
	if len(ownerIDs) == 0 {
		return map[uint32]ExternalIDs{}, nil
	}
	ownedBy, err := externalIDsOfAny(owner, ownerIDs)
	if err != nil {
		return nil, err
	}
	rows, err := db.client.ExternalID.Query().Where(ownedBy).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("external ids: %w", err)
	}
	out := map[uint32]ExternalIDs{}
	for _, r := range rows {
		id := ownerIDOf(owner, r)
		if out[id] == nil {
			out[id] = ExternalIDs{}
		}
		out[id][string(r.Source)] = r.Value
	}
	return out, nil
}

// ListArtistsAwaitingLinks returns up to limit artists whose links were never
// read (links_fetched_at NULL), in id order after afterID: the backfill walks
// them with a cursor, so an artist whose fetch failed is not listed again in
// the same pass.
func (db *DB) ListArtistsAwaitingLinks(
	ctx context.Context,
	afterID uint32,
	limit int,
) ([]*ent.Artist, error) {
	return db.client.Artist.Query().
		Where(artist.LinksFetchedAtIsNil(), artist.IDGT(afterID)).
		Order(ent.Asc(artist.FieldID)).
		Limit(limit).
		All(ctx)
}

// ListAlbumsAwaitingLinks is ListArtistsAwaitingLinks for albums, hydrated
// ones only: an album not hydrated yet gets its links with its tracks.
func (db *DB) ListAlbumsAwaitingLinks(
	ctx context.Context,
	afterID uint32,
	limit int,
) ([]*ent.Album, error) {
	return db.client.Album.Query().
		Where(
			album.LinksFetchedAtIsNil(),
			album.MetadataFetchedAtNotNil(),
			album.IDGT(afterID),
		).
		Order(ent.Asc(album.FieldID)).
		Limit(limit).
		All(ctx)
}

// SetArtistLinks stores an artist's links and stamps links_fetched_at, in
// one transaction. nil ids (MusicBrainz no longer knows the artist) only
// stamp it, so the backfill does not ask again.
func (db *DB) SetArtistLinks(
	ctx context.Context,
	id uint32,
	ids ExternalIDs,
	at time.Time,
) error {
	return db.setLinks(ctx, ExternalIDsOfArtist, id, ids, func(c *ent.Client) error {
		return c.Artist.UpdateOneID(id).SetLinksFetchedAt(at).Exec(ctx)
	})
}

// SetAlbumLinks is SetArtistLinks for an album.
func (db *DB) SetAlbumLinks(
	ctx context.Context,
	id uint32,
	ids ExternalIDs,
	at time.Time,
) error {
	return db.setLinks(ctx, ExternalIDsOfAlbum, id, ids, func(c *ent.Client) error {
		return c.Album.UpdateOneID(id).SetLinksFetchedAt(at).Exec(ctx)
	})
}

func (db *DB) setLinks(
	ctx context.Context,
	owner ExternalIDOwner,
	id uint32,
	ids ExternalIDs,
	stamp func(*ent.Client) error,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := replaceExternalIDs(ctx, tx.Client(), owner, id, ids); err != nil {
		tx.Rollback()
		return err
	}
	if err := stamp(tx.Client()); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
