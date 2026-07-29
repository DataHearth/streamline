package db

import (
	"context"
	"errors"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/invite"
)

// ErrInviteUsed reports that the guarded consumption UPDATE matched no row:
// the invite was already consumed (or no longer exists).
var ErrInviteUsed = errors.New("invite already used")

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

// ConsumeInvite sets used_at and records the consuming user id. The used_at
// IS NULL predicate makes single-use enforcement atomic: two registrations
// racing on the same token both pass validation, but only the first UPDATE
// matches a row — the loser gets ErrInviteUsed.
func (db *DB) ConsumeInvite(
	ctx context.Context,
	id, userID uint32,
	when time.Time,
) error {
	n, err := db.client.Invite.Update().
		Where(invite.IDEQ(id), invite.UsedAtIsNil()).
		SetUsedAt(when).
		SetUsedByID(userID).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInviteUsed
	}
	return nil
}

// RevokeInvite expires the invite immediately by setting expires_at=now.
func (db *DB) RevokeInvite(ctx context.Context, id uint32, now time.Time) error {
	_, err := db.client.Invite.UpdateOneID(id).SetExpiresAt(now).Save(ctx)
	return err
}
