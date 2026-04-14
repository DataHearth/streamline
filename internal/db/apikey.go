package db

import (
	"context"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/apikey"
	"github.com/datahearth/streamline/ent/user"
)

type CreateAPIKeyParams struct {
	Name    string
	KeyHash string
	OwnerID uint32
}

func (db *DB) CreateAPIKey(
	ctx context.Context,
	p CreateAPIKeyParams,
) (*ent.ApiKey, error) {
	return db.client.ApiKey.Create().
		SetName(p.Name).
		SetKeyHash(p.KeyHash).
		SetOwnerID(p.OwnerID).
		Save(ctx)
}

func (db *DB) FindAPIKeyByHash(
	ctx context.Context,
	hash string,
) (*ent.ApiKey, error) {
	return db.client.ApiKey.Query().
		Where(apikey.KeyHash(hash)).
		WithOwner().
		Only(ctx)
}

func (db *DB) ListAPIKeysByUser(
	ctx context.Context,
	userID uint32,
) ([]*ent.ApiKey, error) {
	return db.client.ApiKey.Query().
		Where(apikey.HasOwnerWith(user.IDEQ(userID))).
		Order(ent.Desc(apikey.FieldCreateTime)).
		All(ctx)
}

// DeleteAPIKeyByID deletes an API key scoped by userID. Returns the number of
// rows deleted (0 if the key does not exist or belongs to another user).
func (db *DB) DeleteAPIKeyByID(
	ctx context.Context,
	userID, keyID uint32,
) (int, error) {
	return db.client.ApiKey.Delete().
		Where(
			apikey.IDEQ(keyID),
			apikey.HasOwnerWith(user.IDEQ(userID)),
		).
		Exec(ctx)
}
