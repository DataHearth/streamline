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
	"github.com/datahearth/streamline/ent/bookedition"
	"github.com/datahearth/streamline/ent/externalid"
	"github.com/datahearth/streamline/ent/predicate"
)

// ExternalIDOwner names the row kind an id set hangs off; exactly one owner
// edge is set, as on Credit.
type ExternalIDOwner string

const (
	ExternalIDsOfMovie       ExternalIDOwner = "movie"
	ExternalIDsOfSeries      ExternalIDOwner = "series"
	ExternalIDsOfArtist      ExternalIDOwner = "artist"
	ExternalIDsOfAlbum       ExternalIDOwner = "album"
	ExternalIDsOfBookEdition ExternalIDOwner = "book_edition"
)

var errUnknownExternalIDOwner = errors.New("unknown external id owner")

// ExternalIDs maps a source (metadata.Source*) to a site's id, never the
// provider's own. A write replaces the owner's set; a nil map leaves it.
type ExternalIDs map[string]string

func externalIDsOf(
	owner ExternalIDOwner,
	ownerID uint32,
) (predicate.ExternalID, error) {
	return externalIDsOfAny(owner, []uint32{ownerID})
}

func externalIDsOfAny(
	owner ExternalIDOwner,
	ownerIDs []uint32,
) (predicate.ExternalID, error) {
	switch owner {
	case ExternalIDsOfMovie:
		return externalid.MovieIDIn(ownerIDs...), nil
	case ExternalIDsOfSeries:
		return externalid.TvShowIDIn(ownerIDs...), nil
	case ExternalIDsOfArtist:
		return externalid.ArtistIDIn(ownerIDs...), nil
	case ExternalIDsOfAlbum:
		return externalid.AlbumIDIn(ownerIDs...), nil
	case ExternalIDsOfBookEdition:
		return externalid.BookEditionIDIn(ownerIDs...), nil
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
	case ExternalIDsOfBookEdition:
		return r.BookEditionID
	default:
		return r.AlbumID
	}
}

// replaceExternalIDs writes inside the caller's transaction, like replaceCast.
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
			// A source missing from the enum costs a link, never the write.
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
		case ExternalIDsOfBookEdition:
			b = b.SetBookEditionID(ownerID)
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

// TitleExternalIDs returns one row's ids, empty when it has none.
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

// ExternalIDsFor reads many rows' ids in one query, keyed by row id.
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

// ListArtistsAwaitingLinks pages the artists never read for links, by id
// after a cursor, so a failed one is not listed twice in a pass.
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

// ListAlbumsAwaitingLinks is its twin for hydrated albums; a stub gets its
// links with its tracks.
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

// ListEditionsAwaitingLinks is its twin for the editions a slot follows,
// the only ones a link opens.
func (db *DB) ListEditionsAwaitingLinks(
	ctx context.Context,
	afterID uint32,
	limit int,
) ([]*ent.BookEdition, error) {
	return db.client.BookEdition.Query().
		Where(
			bookedition.LinksFetchedAtIsNil(),
			bookedition.Or(bookedition.HasEbookOf(), bookedition.HasAudiobookOf()),
			bookedition.IDGT(afterID),
		).
		Order(ent.Asc(bookedition.FieldID)).
		Limit(limit).
		All(ctx)
}

// SetEditionLinks stores a batch of editions' links and stamps them in one
// transaction: one commit per Hardcover request, not one per edition.
func (db *DB) SetEditionLinks(
	ctx context.Context,
	links map[uint32]ExternalIDs,
	at time.Time,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	c := tx.Client()
	for id, ids := range links {
		if err := replaceExternalIDs(ctx, c, ExternalIDsOfBookEdition, id, ids); err != nil {
			tx.Rollback()
			return err
		}
		if err := c.BookEdition.UpdateOneID(id).SetLinksFetchedAt(at).Exec(ctx); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// SetArtistLinks stores an artist's links and stamps it; nil ids only stamp.
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
