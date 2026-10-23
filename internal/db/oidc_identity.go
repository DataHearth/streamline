package db

import (
	"context"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/oidcidentity"
)

type CreateOIDCIdentityParams struct {
	Provider string
	Subject  string
	Email    string
	OwnerID  uint32
}

// FindOIDCIdentity returns the identity matching (provider, subject) with its
// owner preloaded.
func (db *DB) FindOIDCIdentity(
	ctx context.Context,
	provider, subject string,
) (*ent.OIDCIdentity, error) {
	return db.client.OIDCIdentity.Query().
		Where(
			oidcidentity.Provider(provider),
			oidcidentity.Subject(subject),
		).
		WithOwner().
		Only(ctx)
}

func (db *DB) CreateOIDCIdentity(
	ctx context.Context,
	p CreateOIDCIdentityParams,
) (*ent.OIDCIdentity, error) {
	return db.client.OIDCIdentity.Create().
		SetProvider(p.Provider).
		SetSubject(p.Subject).
		SetEmail(p.Email).
		SetOwnerID(p.OwnerID).
		Save(ctx)
}
