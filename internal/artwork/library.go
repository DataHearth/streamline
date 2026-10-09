package artwork

import (
	"context"
	"strconv"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/book"
)

// EntLibrary answers Library from the database: an artist or album by its
// MusicBrainz id (an album is a release group), a book by its Hardcover id.
type EntLibrary struct{ Client *ent.Client }

func (l EntLibrary) Find(
	ctx context.Context,
	kind Kind,
	key string,
) (uint32, bool, error) {
	var (
		id  uint32
		err error
	)
	switch kind {
	case KindArtists:
		id, err = l.Client.Artist.Query().Where(artist.Mbid(key)).FirstID(ctx)
	case KindAlbums:
		id, err = l.Client.Album.Query().Where(album.Mbid(key)).FirstID(ctx)
	case KindBooks:
		hc, perr := strconv.ParseUint(key, 10, 32)
		if perr != nil {
			return 0, false, nil
		}
		id, err = l.Client.Book.Query().
			Where(book.HardcoverID(uint32(hc))).FirstID(ctx)
	default:
		return 0, false, nil
	}
	if ent.IsNotFound(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}
