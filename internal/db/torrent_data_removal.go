package db

import (
	"context"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/torrentdataremoval"
)

// TorrentDataRemovalParams names the files a removed torrent left to delete:
// Info is its bencoded info dictionary, Dir the download directory they sit in.
type TorrentDataRemovalParams struct {
	InfoHash string
	Name     string
	Dir      string
	Info     []byte
}

// KeepTorrentDataRemoval records that a torrent's files are owed deleting. A
// hash already recorded keeps its row.
func (db *DB) KeepTorrentDataRemoval(
	ctx context.Context,
	p TorrentDataRemovalParams,
) error {
	err := db.client.TorrentDataRemoval.Create().
		SetInfoHash(p.InfoHash).
		SetName(p.Name).
		SetDir(p.Dir).
		SetInfo(p.Info).
		Exec(ctx)
	if err != nil && !ent.IsConstraintError(err) {
		return fmt.Errorf("keep torrent data removal: %w", err)
	}
	return nil
}

func (db *DB) ListTorrentDataRemovals(
	ctx context.Context,
) ([]*ent.TorrentDataRemoval, error) {
	return db.client.TorrentDataRemoval.Query().
		Order(ent.Asc(torrentdataremoval.FieldCreateTime)).
		All(ctx)
}

func (db *DB) DeleteTorrentDataRemoval(
	ctx context.Context,
	infoHash string,
) error {
	_, err := db.client.TorrentDataRemoval.Delete().
		Where(torrentdataremoval.InfoHashEQ(infoHash)).
		Exec(ctx)
	return err
}
