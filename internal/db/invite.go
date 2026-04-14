package db

import (
	"context"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/invite"
)

type CreateInviteParams struct {
	TokenHash   string
	Email       string
	Role        invite.Role
	ExpiresAt   time.Time
	CreatedByID uint32
}

func (db *DB) CreateInvite(
	ctx context.Context,
	p CreateInviteParams,
) (*ent.Invite, error) {
	b := db.client.Invite.Create().
		SetTokenHash(p.TokenHash).
		SetRole(p.Role).
		SetExpiresAt(p.ExpiresAt).
		SetCreatedByID(p.CreatedByID)
	if p.Email != "" {
		b.SetEmail(p.Email)
	}
	return b.Save(ctx)
}

func (db *DB) FindInviteByTokenHash(
	ctx context.Context,
	hash string,
) (*ent.Invite, error) {
	return db.client.Invite.Query().Where(invite.TokenHash(hash)).Only(ctx)
}

// FindUnusedInviteForEmail returns the earliest unused + unexpired invite
// bound to the given email.
func (db *DB) FindUnusedInviteForEmail(
	ctx context.Context,
	email string,
	now time.Time,
) (*ent.Invite, error) {
	return db.client.Invite.Query().
		Where(
			invite.EmailEQ(email),
			invite.UsedAtIsNil(),
			invite.ExpiresAtGT(now),
		).
		First(ctx)
}

func (db *DB) ListInvites(ctx context.Context) ([]*ent.Invite, error) {
	return db.client.Invite.Query().WithCreatedBy().WithUsedBy().All(ctx)
}

// MarkInviteUsed sets used_at on the invite.
func (db *DB) MarkInviteUsed(
	ctx context.Context,
	id uint32,
	when time.Time,
) (*ent.Invite, error) {
	return db.client.Invite.UpdateOneID(id).SetUsedAt(when).Save(ctx)
}

// MarkInviteUsedWithUser sets used_at and records the consuming user id.
// Used by RegisterWithInvite inside a transaction.
func (db *DB) MarkInviteUsedWithUser(
	ctx context.Context,
	id, userID uint32,
	when time.Time,
) (*ent.Invite, error) {
	return db.client.Invite.UpdateOneID(id).
		SetUsedAt(when).
		SetUsedByID(userID).
		Save(ctx)
}

// RevokeInvite expires the invite immediately by setting expires_at=now.
func (db *DB) RevokeInvite(ctx context.Context, id uint32, now time.Time) error {
	_, err := db.client.Invite.UpdateOneID(id).SetExpiresAt(now).Save(ctx)
	return err
}
