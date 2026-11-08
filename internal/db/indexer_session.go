package db

import (
	"context"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/indexersession"
)

// IndexerSessionParams is the whole state a built-in indexer's session row
// holds; a put replaces it.
type IndexerSessionParams struct {
	Cookies       map[string]string
	UserAgent     string
	ExpiresAt     *time.Time
	LoginFailures uint8
	RetryAt       *time.Time
	LastError     string
	ConfigHash    string
}

func (db *DB) IndexerSession(
	ctx context.Context,
	name string,
) (*ent.IndexerSession, error) {
	s, err := db.client.IndexerSession.Query().
		Where(indexersession.NameEQ(name)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return s, err
}

func (db *DB) PutIndexerSession(
	ctx context.Context,
	name string,
	p IndexerSessionParams,
) error {
	update := func() (int, error) {
		u := db.client.IndexerSession.Update().
			Where(indexersession.NameEQ(name)).
			SetCookies(p.Cookies).
			SetUserAgent(p.UserAgent).
			SetLoginFailures(p.LoginFailures).
			SetLastError(p.LastError).
			SetConfigHash(p.ConfigHash)
		if p.ExpiresAt == nil {
			u.ClearExpiresAt()
		} else {
			u.SetExpiresAt(*p.ExpiresAt)
		}
		if p.RetryAt == nil {
			u.ClearRetryAt()
		} else {
			u.SetRetryAt(*p.RetryAt)
		}
		return u.Save(ctx)
	}
	n, err := update()
	if err != nil || n > 0 {
		return err
	}
	err = db.client.IndexerSession.Create().
		SetName(name).
		SetCookies(p.Cookies).
		SetUserAgent(p.UserAgent).
		SetNillableExpiresAt(p.ExpiresAt).
		SetLoginFailures(p.LoginFailures).
		SetNillableRetryAt(p.RetryAt).
		SetLastError(p.LastError).
		SetConfigHash(p.ConfigHash).
		Exec(ctx)
	// Two first calls of one indexer both found no row; the loser's create
	// hits the unique name, and its state is the newer one.
	if ent.IsConstraintError(err) {
		_, err = update()
	}
	return err
}

func (db *DB) DeleteIndexerSession(ctx context.Context, name string) error {
	_, err := db.client.IndexerSession.Delete().
		Where(indexersession.NameEQ(name)).
		Exec(ctx)
	return err
}
