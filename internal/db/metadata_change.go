package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/metadatachange"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

var (
	// ErrNotFollowable is a change with nothing to apply: the item is gone
	// upstream, and what is left is finding it again, removing it or keeping
	// it.
	ErrNotFollowable = errors.New("this change has nothing to follow")
	// ErrChangeConflict is a change that cannot be applied as it stands.
	ErrChangeConflict = errors.New("the change cannot be applied")
)

// ChangeOwnerType names the kind of item a metadata change belongs to.
type ChangeOwnerType string

const (
	ChangeOwnerArtist ChangeOwnerType = "artist"
	ChangeOwnerAlbum  ChangeOwnerType = "album"
	ChangeOwnerTVShow ChangeOwnerType = "tv_show"
	ChangeOwnerMovie  ChangeOwnerType = "movie"
	ChangeOwnerBook   ChangeOwnerType = "book"
)

// ChangeOwner is the item a metadata change is about.
type ChangeOwner struct {
	Type ChangeOwnerType
	ID   uint32
}

// ChangeFilter narrows ListChanges; an empty slice matches everything.
type ChangeFilter struct {
	Types []ChangeOwnerType
	Kinds []metadatachange.Kind
}

// ChangeCounts counts the pending changes per owner type and per kind.
type ChangeCounts struct {
	Total  uint32
	ByType map[ChangeOwnerType]uint32
	ByKind map[metadatachange.Kind]uint32
}

func ownerPredicate(o ChangeOwner) predicate.MetadataChange {
	switch o.Type {
	case ChangeOwnerArtist:
		return metadatachange.HasArtistWith(artist.ID(o.ID))
	case ChangeOwnerAlbum:
		return metadatachange.HasAlbumWith(album.ID(o.ID))
	case ChangeOwnerTVShow:
		return metadatachange.HasTvShowWith(tvshow.ID(o.ID))
	case ChangeOwnerMovie:
		return metadatachange.HasMovieWith(movie.ID(o.ID))
	default:
		return metadatachange.HasBookWith(book.ID(o.ID))
	}
}

func typePredicate(t ChangeOwnerType) predicate.MetadataChange {
	switch t {
	case ChangeOwnerArtist:
		return metadatachange.HasArtist()
	case ChangeOwnerAlbum:
		return metadatachange.HasAlbum()
	case ChangeOwnerTVShow:
		return metadatachange.HasTvShow()
	case ChangeOwnerMovie:
		return metadatachange.HasMovie()
	default:
		return metadatachange.HasBook()
	}
}

// ChangeOwnerOf names the owner of a change loaded with its owner edges.
func ChangeOwnerOf(c *ent.MetadataChange) ChangeOwner {
	switch {
	case c.Edges.Artist != nil:
		return ChangeOwner{ChangeOwnerArtist, c.Edges.Artist.ID}
	case c.Edges.Album != nil:
		return ChangeOwner{ChangeOwnerAlbum, c.Edges.Album.ID}
	case c.Edges.TvShow != nil:
		return ChangeOwner{ChangeOwnerTVShow, c.Edges.TvShow.ID}
	case c.Edges.Movie != nil:
		return ChangeOwner{ChangeOwnerMovie, c.Edges.Movie.ID}
	default:
		return ChangeOwner{ChangeOwnerBook, c.Edges.Book.ID}
	}
}

func withChangeOwners(q *ent.MetadataChangeQuery) *ent.MetadataChangeQuery {
	return q.
		WithArtist().
		WithAlbum(func(q *ent.AlbumQuery) { q.WithArtist() }).
		WithTvShow().
		WithMovie().
		WithBook()
}

func (db *DB) ReportChange(
	ctx context.Context,
	owner ChangeOwner,
	kind metadatachange.Kind,
	fingerprint string,
	payload map[string]any,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	cur, err := tx.MetadataChange.Query().
		Where(metadatachange.KindEQ(kind), ownerPredicate(owner)).
		Only(ctx)
	switch {
	case ent.IsNotFound(err):
		c := tx.MetadataChange.Create().
			SetKind(kind).
			SetFingerprint(fingerprint).
			SetPayload(payload)
		switch owner.Type {
		case ChangeOwnerArtist:
			c.SetArtistID(owner.ID)
		case ChangeOwnerAlbum:
			c.SetAlbumID(owner.ID)
		case ChangeOwnerTVShow:
			c.SetTvShowID(owner.ID)
		case ChangeOwnerMovie:
			c.SetMovieID(owner.ID)
		case ChangeOwnerBook:
			c.SetBookID(owner.ID)
		default:
			tx.Rollback()
			return fmt.Errorf("metadata change owner %q unknown", owner.Type)
		}
		err = c.Exec(ctx)
	case err != nil:
	case cur.State == metadatachange.StateKept && cur.Fingerprint == fingerprint:
	default:
		err = cur.Update().
			SetState(metadatachange.StatePending).
			SetFingerprint(fingerprint).
			SetPayload(payload).
			Exec(ctx)
	}
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (db *DB) ClearChange(
	ctx context.Context,
	owner ChangeOwner,
	kinds ...metadatachange.Kind,
) error {
	_, err := db.client.MetadataChange.Delete().
		Where(ownerPredicate(owner), metadatachange.KindIn(kinds...)).
		Exec(ctx)
	return err
}

func (db *DB) ListChanges(
	ctx context.Context,
	f ChangeFilter,
	limit, offset uint32,
) ([]*ent.MetadataChange, uint32, error) {
	where := []predicate.MetadataChange{
		metadatachange.StateEQ(metadatachange.StatePending),
	}
	if len(f.Types) > 0 {
		types := make([]predicate.MetadataChange, len(f.Types))
		for i, t := range f.Types {
			types[i] = typePredicate(t)
		}
		where = append(where, metadatachange.Or(types...))
	}
	if len(f.Kinds) > 0 {
		where = append(where, metadatachange.KindIn(f.Kinds...))
	}
	total, err := db.client.MetadataChange.Query().Where(where...).Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := withChangeOwners(db.client.MetadataChange.Query().Where(where...)).
		Order(
			ent.Desc(metadatachange.FieldUpdateTime),
			ent.Desc(metadatachange.FieldID),
		).
		Limit(int(limit)).
		Offset(int(offset)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return rows, numeric.SaturateU32(total), nil
}

func (db *DB) CountChanges(ctx context.Context) (ChangeCounts, error) {
	rows, err := withChangeOwners(db.client.MetadataChange.Query().
		Where(metadatachange.StateEQ(metadatachange.StatePending))).
		Select(metadatachange.FieldID, metadatachange.FieldKind).
		All(ctx)
	if err != nil {
		return ChangeCounts{}, err
	}
	out := ChangeCounts{
		ByType: map[ChangeOwnerType]uint32{},
		ByKind: map[metadatachange.Kind]uint32{},
	}
	for _, r := range rows {
		out.Total++
		out.ByType[ChangeOwnerOf(r).Type]++
		out.ByKind[r.Kind]++
	}
	return out, nil
}

func (db *DB) FindChange(
	ctx context.Context,
	id uint32,
) (*ent.MetadataChange, error) {
	return withChangeOwners(db.client.MetadataChange.Query().
		Where(metadatachange.ID(id))).
		Only(ctx)
}

func (db *DB) KeepChange(ctx context.Context, id uint32) error {
	n, err := db.client.MetadataChange.Update().
		Where(
			metadatachange.ID(id),
			metadatachange.StateEQ(metadatachange.StatePending),
		).
		SetState(metadatachange.StateKept).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return &ent.NotFoundError{}
	}
	return nil
}

func (db *DB) DeleteChange(ctx context.Context, id uint32) error {
	return db.client.MetadataChange.DeleteOneID(id).Exec(ctx)
}
