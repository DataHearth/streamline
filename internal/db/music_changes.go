package db

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/track"
)

// ErrAlbumInUse refuses to drop an album something was done with.
var ErrAlbumInUse = errors.New("the album is already in the library and in use")

func (db *DB) RepointAlbum(
	ctx context.Context,
	id uint32,
	mbid string,
) (uint32, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return 0, err
	}
	var dropped uint32
	other, err := tx.Album.Query().
		Where(album.MbidEQ(mbid), album.IDNEQ(id)).
		Only(ctx)
	switch {
	case ent.IsNotFound(err):
		err = nil
	case err != nil:
	default:
		var used bool
		used, err = tx.Album.Query().
			Where(album.IDEQ(other.ID), albumInUse()).
			Exist(ctx)
		switch {
		case err != nil:
		case used:
			err = ErrAlbumInUse
		default:
			dropped = other.ID
			err = tx.Album.DeleteOneID(other.ID).Exec(ctx)
		}
	}
	if err == nil {
		err = tx.Album.UpdateOneID(id).SetMbid(mbid).Exec(ctx)
	}
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	return dropped, tx.Commit()
}

func (db *DB) RepointArtist(
	ctx context.Context,
	id uint32,
	mbid string,
) (uint32, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return 0, err
	}
	target, err := tx.Artist.Query().Where(artist.MbidEQ(mbid)).Only(ctx)
	if ent.IsNotFound(err) {
		if err := tx.Artist.UpdateOneID(id).SetMbid(mbid).Exec(ctx); err != nil {
			tx.Rollback()
			return 0, err
		}
		return id, tx.Commit()
	}
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := mergeArtistInto(ctx, tx.Client(), id, target.ID); err != nil {
		tx.Rollback()
		return 0, err
	}
	return target.ID, tx.Commit()
}

// mergeArtistInto hands every album listed under from to into, as owner or
// co-artist alike, and deletes from.
func mergeArtistInto(ctx context.Context, c *ent.Client, from, into uint32) error {
	owned, err := c.Album.Query().
		Where(album.HasArtistWith(artist.IDEQ(from))).
		IDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range owned {
		if err := c.Album.UpdateOneID(id).
			SetArtistID(into).
			RemoveCoArtistIDs(into).
			Exec(ctx); err != nil {
			return err
		}
	}
	shared, err := c.Album.Query().
		Where(album.HasCoArtistsWith(artist.IDEQ(from))).
		All(ctx)
	if err != nil {
		return err
	}
	for _, a := range shared {
		u := c.Album.UpdateOne(a).RemoveCoArtistIDs(from)
		isOwner, err := c.Album.Query().
			Where(album.IDEQ(a.ID), album.HasArtistWith(artist.IDEQ(into))).
			Exist(ctx)
		if err != nil {
			return err
		}
		if !isOwner {
			u = u.AddCoArtistIDs(into)
		}
		if err := u.Exec(ctx); err != nil {
			return err
		}
	}
	return c.Artist.DeleteOneID(from).Exec(ctx)
}

func (db *DB) DeleteAlbumTracks(
	ctx context.Context,
	albumID uint32,
	ids []uint32,
) error {
	_, err := db.client.Track.Delete().
		Where(track.IDIn(ids...), track.HasAlbumWith(album.IDEQ(albumID))).
		Exec(ctx)
	return err
}
