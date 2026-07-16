package db

import (
	"context"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/torrentsession"
)

// CreateTorrentSessionParams carries the persisted identity of one torrent
// held by the builtin engine.
type CreateTorrentSessionParams struct {
	InfoHash      string
	Name          string
	SavePath      string
	SourceMagnet  string
	SourceTorrent []byte
}

func (db *DB) CreateTorrentSession(
	ctx context.Context,
	p CreateTorrentSessionParams,
) (*ent.TorrentSession, error) {
	c := db.client.TorrentSession.Create().
		SetInfoHash(p.InfoHash).
		SetName(p.Name).
		SetSavePath(p.SavePath)
	if p.SourceMagnet != "" {
		c.SetSourceMagnet(p.SourceMagnet)
	}
	if len(p.SourceTorrent) > 0 {
		c.SetSourceTorrent(p.SourceTorrent)
	}
	return c.Save(ctx)
}

func (db *DB) ListTorrentSessions(
	ctx context.Context,
) ([]*ent.TorrentSession, error) {
	return db.client.TorrentSession.Query().All(ctx)
}

func (db *DB) DeleteTorrentSessionByHash(
	ctx context.Context,
	infoHash string,
) error {
	_, err := db.client.TorrentSession.Delete().
		Where(torrentsession.InfoHashEQ(infoHash)).
		Exec(ctx)
	return err
}

func (db *DB) SetTorrentSessionPaused(
	ctx context.Context,
	infoHash string,
	paused bool,
) error {
	_, err := db.client.TorrentSession.Update().
		Where(torrentsession.InfoHashEQ(infoHash)).
		SetPaused(paused).
		Save(ctx)
	return err
}

func (db *DB) SetTorrentSessionName(
	ctx context.Context,
	infoHash, name string,
) error {
	_, err := db.client.TorrentSession.Update().
		Where(torrentsession.InfoHashEQ(infoHash)).
		SetName(name).
		Save(ctx)
	return err
}

func (db *DB) SetTorrentSessionCompleted(
	ctx context.Context,
	infoHash string,
	at time.Time,
) error {
	_, err := db.client.TorrentSession.Update().
		Where(torrentsession.InfoHashEQ(infoHash)).
		SetCompletedAt(at).
		Save(ctx)
	return err
}

func (db *DB) SetTorrentSessionSeedStopped(
	ctx context.Context,
	infoHash string,
) error {
	_, err := db.client.TorrentSession.Update().
		Where(torrentsession.InfoHashEQ(infoHash)).
		SetSeedStopped(true).
		Save(ctx)
	return err
}
