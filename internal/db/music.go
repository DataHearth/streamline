package db

import (
	"context"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/track"
)

type TrackSeed struct {
	MBID     string
	Title    string
	Disc     uint8
	Position uint16
	Duration uint32
}

type AlbumSeed struct {
	MBID        string
	ReleaseMBID string
	Title       string
	Type        string
	ReleaseDate *time.Time
	Tracks      []TrackSeed
}

type CreateArtistParams struct {
	MBID           string
	Name           string
	SortName       string
	Overview       string
	Monitored      bool
	Path           string
	QualityProfile string
	Albums         []AlbumSeed
}

// RefreshArtistParams carries provider-sourced fields. Albums already stored
// keep their monitored flag and status; only albums new to the artist are
// created, inheriting the artist's monitored flag.
type RefreshArtistParams struct {
	Name        string
	SortName    string
	Overview    string
	Albums      []AlbumSeed
	RefreshedAt time.Time
}

func createAlbum(
	ctx context.Context,
	c *ent.Client,
	artistID uint32,
	monitored bool,
	a AlbumSeed,
) error {
	b := c.Album.Create().
		SetMbid(a.MBID).
		SetReleaseMbid(a.ReleaseMBID).
		SetTitle(a.Title).
		SetMonitored(monitored).
		SetArtistID(artistID).
		SetNillableReleaseDate(a.ReleaseDate)
	if a.Type != "" {
		b = b.SetType(album.Type(a.Type))
	}
	row, err := b.Save(ctx)
	if err != nil {
		return err
	}
	if len(a.Tracks) == 0 {
		return nil
	}
	creates := make([]*ent.TrackCreate, len(a.Tracks))
	for i, t := range a.Tracks {
		creates[i] = c.Track.Create().
			SetMbid(t.MBID).
			SetTitle(t.Title).
			SetDisc(t.Disc).
			SetPosition(t.Position).
			SetDuration(t.Duration).
			SetAlbumID(row.ID)
	}
	return c.Track.CreateBulk(creates...).Exec(ctx)
}

func (db *DB) CreateArtist(
	ctx context.Context,
	p CreateArtistParams,
) (*ent.Artist, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	row, err := tx.Artist.Create().
		SetMbid(p.MBID).
		SetName(p.Name).
		SetSortName(p.SortName).
		SetOverview(p.Overview).
		SetMonitored(p.Monitored).
		SetPath(p.Path).
		SetQualityProfile(p.QualityProfile).
		Save(ctx)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	for _, a := range p.Albums {
		if err := createAlbum(ctx, tx.Client(), row.ID, p.Monitored, a); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.FindArtistByID(ctx, row.ID)
}

func (db *DB) FindArtistByID(ctx context.Context, id uint32) (*ent.Artist, error) {
	return db.client.Artist.Query().
		Where(artist.IDEQ(id)).
		WithAlbums(func(q *ent.AlbumQuery) {
			q.Order(ent.Asc(album.FieldReleaseDate), ent.Asc(album.FieldTitle)).
				WithTracks(func(tq *ent.TrackQuery) {
					tq.Order(ent.Asc(track.FieldDisc), ent.Asc(track.FieldPosition)).
						WithMediaFiles()
				})
		}).
		Only(ctx)
}

func (db *DB) FindAlbumByID(ctx context.Context, id uint32) (*ent.Album, error) {
	return db.client.Album.Query().
		Where(album.IDEQ(id)).
		WithTracks(func(tq *ent.TrackQuery) {
			tq.Order(ent.Asc(track.FieldDisc), ent.Asc(track.FieldPosition)).
				WithMediaFiles()
		}).
		Only(ctx)
}

func (db *DB) FindArtistByMBID(
	ctx context.Context,
	mbid string,
) (*ent.Artist, error) {
	row, err := db.client.Artist.Query().Where(artist.MbidEQ(mbid)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return row, err
}

func (db *DB) CountArtists(ctx context.Context) (int, error) {
	return db.client.Artist.Query().Count(ctx)
}

func (db *DB) ListArtists(
	ctx context.Context,
	offset, limit uint32,
) ([]*ent.Artist, error) {
	return db.client.Artist.Query().
		Order(ent.Asc(artist.FieldSortName), ent.Asc(artist.FieldName)).
		Offset(int(offset)).Limit(int(limit)).
		WithAlbums().
		All(ctx)
}

func (db *DB) RefreshArtist(
	ctx context.Context,
	id uint32,
	p RefreshArtistParams,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	c := tx.Client()
	row, err := c.Artist.Query().Where(artist.IDEQ(id)).Only(ctx)
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := c.Artist.UpdateOneID(id).
		SetName(p.Name).
		SetSortName(p.SortName).
		SetOverview(p.Overview).
		SetLastRefreshedAt(p.RefreshedAt).
		Exec(ctx); err != nil {
		tx.Rollback()
		return err
	}
	existing, err := c.Album.Query().
		Where(album.HasArtistWith(artist.IDEQ(id))).
		All(ctx)
	if err != nil {
		tx.Rollback()
		return err
	}
	byMBID := make(map[string]*ent.Album, len(existing))
	for _, a := range existing {
		byMBID[a.Mbid] = a
	}
	for _, a := range p.Albums {
		if cur := byMBID[a.MBID]; cur != nil {
			u := c.Album.UpdateOne(cur).
				SetTitle(a.Title).
				SetNillableReleaseDate(a.ReleaseDate)
			if a.Type != "" {
				u = u.SetType(album.Type(a.Type))
			}
			if err := u.Exec(ctx); err != nil {
				tx.Rollback()
				return err
			}
			continue
		}
		if err := createAlbum(ctx, c, id, row.Monitored, a); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) SetArtistMonitored(
	ctx context.Context,
	id uint32,
	monitored bool,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := tx.Artist.UpdateOneID(id).
		SetMonitored(monitored).
		Exec(ctx); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Album.Update().
		Where(album.HasArtistWith(artist.IDEQ(id))).
		SetMonitored(monitored).
		Exec(ctx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (db *DB) SetArtistQualityProfile(
	ctx context.Context,
	id uint32,
	profile string,
) error {
	return db.client.Artist.UpdateOneID(id).SetQualityProfile(profile).Exec(ctx)
}

func (db *DB) SetAlbumMonitored(
	ctx context.Context,
	id uint32,
	monitored bool,
) error {
	return db.client.Album.UpdateOneID(id).SetMonitored(monitored).Exec(ctx)
}

func (db *DB) DeleteArtist(ctx context.Context, id uint32) error {
	return db.client.Artist.DeleteOneID(id).Exec(ctx)
}

func (db *DB) ListUpcomingAlbums(
	ctx context.Context,
	from, to time.Time,
) ([]*ent.Album, error) {
	return db.client.Album.Query().
		Where(
			album.Monitored(true),
			album.ReleaseDateGTE(from),
			album.ReleaseDateLT(to),
		).
		WithArtist().
		Order(ent.Asc(album.FieldReleaseDate)).
		All(ctx)
}
