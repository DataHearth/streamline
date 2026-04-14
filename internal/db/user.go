package db

import (
	"context"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
)

type CreateUserParams struct {
	Email        string
	DisplayName  string
	PasswordHash string
	Role         user.Role
	AuthMethod   user.AuthMethod
}

// UserSort selects the column ordering applied to admin user listings.
type UserSort uint8

const (
	UserSortCreated UserSort = iota
	UserSortName
	UserSortRole
	UserSortAuth
)

// UserOrder is the direction applied to UserSort.
type UserOrder uint8

const (
	UserOrderDesc UserOrder = iota
	UserOrderAsc
)

// ListUsersParams is the filter/pagination bundle for admin user listing.
// All fields are optional; Limit defaults to 25 when zero, and is capped at
// 100 by the caller. Sort/Order default to created/desc.
type ListUsersParams struct {
	Q      string
	Role   user.Role
	Limit  uint32
	Offset uint32
	Sort   UserSort
	Order  UserOrder
}

// UpdateUserParams bundles the fields that admin PATCH /users/{uid} may
// mutate in a single SQL UPDATE. Nil fields are left untouched. The Clear*
// flags exist because the underlying ent setters distinguish "set to value"
// from "clear to nil"; setting both ClearLockedUntil and a non-nil
// LockedUntil is a programmer error and the non-nil pointer wins.
type UpdateUserParams struct {
	Role                   *user.Role
	AuthMethod             *user.AuthMethod
	DisplayName            *string
	Email                  *string
	FailedLoginCount       *uint8
	LastFailedLoginAt      *time.Time
	ClearLastFailedLoginAt bool
	LockedUntil            *time.Time
	ClearLockedUntil       bool
}

func (db *DB) FindUserByEmail(ctx context.Context, email string) (*ent.User, error) {
	return db.client.User.Query().Where(user.EmailEQ(email)).Only(ctx)
}

func (db *DB) FindUserByID(ctx context.Context, id uint32) (*ent.User, error) {
	return db.client.User.Get(ctx, id)
}

func (db *DB) CountUsers(ctx context.Context) (int, error) {
	return db.client.User.Query().Count(ctx)
}

func (db *DB) CreateUser(
	ctx context.Context,
	p CreateUserParams,
) (*ent.User, error) {
	b := db.client.User.Create().
		SetEmail(p.Email).
		SetRole(p.Role).
		SetAuthMethod(p.AuthMethod)
	if p.PasswordHash != "" {
		b.SetPasswordHash(p.PasswordHash)
	}
	if p.DisplayName != "" {
		b.SetDisplayName(p.DisplayName)
	}
	return b.Save(ctx)
}

func (db *DB) UpdateUserPassword(ctx context.Context, id uint32, hash string) error {
	return db.client.User.UpdateOneID(id).SetPasswordHash(hash).Exec(ctx)
}

func orderForListUsers(s UserSort, o UserOrder) user.OrderOption {
	var col string
	switch s {
	case UserSortName:
		col = user.FieldDisplayName
	case UserSortRole:
		col = user.FieldRole
	case UserSortAuth:
		col = user.FieldAuthMethod
	default:
		col = user.FieldCreateTime
	}
	if o == UserOrderAsc {
		return ent.Asc(col)
	}
	return ent.Desc(col)
}

// ListUsers returns a filtered, paginated list of users along with the total
// count matching the filter. Sort/Order default to newest-first.
func (db *DB) ListUsers(
	ctx context.Context,
	p ListUsersParams,
) ([]*ent.User, int, error) {
	q := db.client.User.Query()
	if p.Q != "" {
		q = q.Where(
			user.Or(
				user.EmailContainsFold(p.Q),
				user.DisplayNameContainsFold(p.Q),
			),
		)
	}
	if p.Role != "" {
		q = q.Where(user.RoleEQ(p.Role))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	limit := p.Limit
	if limit == 0 {
		limit = 25
	}
	items, err := q.
		Order(orderForListUsers(p.Sort, p.Order)).
		Limit(int(limit)).
		Offset(int(p.Offset)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// CountUsersByRole returns the number of users with the given role. Used to
// protect the last-admin invariant.
func (db *DB) CountUsersByRole(ctx context.Context, role user.Role) (int, error) {
	return db.client.User.Query().Where(user.RoleEQ(role)).Count(ctx)
}

// UpdateUser applies every non-nil field in p in a single SQL UPDATE, so
// admin PATCH /users/{uid} does not fan out into three queries.
func (db *DB) UpdateUser(
	ctx context.Context,
	id uint32,
	p UpdateUserParams,
) (*ent.User, error) {
	upd := db.client.User.UpdateOneID(id)
	if p.Role != nil {
		upd = upd.SetRole(*p.Role)
	}
	if p.AuthMethod != nil {
		upd = upd.SetAuthMethod(*p.AuthMethod)
	}
	if p.DisplayName != nil {
		upd = upd.SetDisplayName(*p.DisplayName)
	}
	if p.Email != nil {
		upd = upd.SetEmail(*p.Email)
	}
	if p.FailedLoginCount != nil {
		upd = upd.SetFailedLoginCount(*p.FailedLoginCount)
	}
	if p.LastFailedLoginAt != nil {
		upd = upd.SetLastFailedLoginAt(*p.LastFailedLoginAt)
	} else if p.ClearLastFailedLoginAt {
		upd = upd.ClearLastFailedLoginAt()
	}
	if p.LockedUntil != nil {
		upd = upd.SetLockedUntil(*p.LockedUntil)
	} else if p.ClearLockedUntil {
		upd = upd.ClearLockedUntil()
	}
	return upd.Save(ctx)
}

// DeleteUser permanently removes the user and all dependent data (api keys,
// oidc identities, requests, sessions) via ON DELETE CASCADE at the schema
// level. Requests the user approved for others are preserved with
// approved_by set to NULL (ON DELETE SET NULL) so the request history of
// other users is not corrupted.
//
// GDPR-compliant: every row that belongs to the user is erased atomically.
func (db *DB) DeleteUser(ctx context.Context, id uint32) error {
	return db.client.User.DeleteOneID(id).Exec(ctx)
}
