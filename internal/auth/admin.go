package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/bcrypt"
)

// Admin-only sentinel errors. Handlers translate these to HTTP codes.
var (
	// ErrSelfDeleteForbidden is returned when an admin attempts to delete
	// their own account. Handlers map to 409 self_delete_forbidden.
	ErrSelfDeleteForbidden = errors.New("cannot delete yourself")

	// ErrLastAdmin is returned when demoting or deleting a user would leave
	// the system without any admin. Handlers map to 409 last_admin.
	ErrLastAdmin = errors.New("cannot remove the last admin")

	// ErrUserEmailExists is returned when direct user creation collides with
	// an existing email. Handlers map to 409 email_exists.
	ErrUserEmailExists = errors.New("email already registered")

	// ErrUserNotFound is returned when the target user does not exist.
	// Handlers map to 404.
	ErrUserNotFound = errors.New("user not found")

	// ErrAccountLocked is the sentinel callers compare against using errors.As
	// (errors.Is also works because Unwrap is not defined). The runtime value
	// returned by Login carries the auto-expiry time in LockedUntil.
	ErrAccountLocked = ErrAccountLockedT{}
)

// ErrAccountLockedT is returned by Login when the account has been locked
// after too many failed attempts. LockedUntil reports when the lockout
// auto-expires; handlers surface this to users via Retry-After / banner.
type ErrAccountLockedT struct {
	LockedUntil time.Time
}

func (e ErrAccountLockedT) Error() string {
	return "account locked until " + e.LockedUntil.Format(time.RFC3339)
}

// UserFilter bundles the optional search, role, pagination, and ordering
// parameters accepted by ListUsers.
type UserFilter struct {
	Q      string
	Role   string
	Limit  uint16
	Offset uint32
	Sort   db.UserSort
	Order  db.UserOrder
}

// UserPatch is the partial update bundle accepted by UpdateUser. Nil fields
// are preserved; non-nil fields are applied.
type UserPatch struct {
	Email       *string
	Role        *string
	DisplayName *string
	AuthMethod  *string
}

// ListUsers returns a filtered, paginated slice of users plus the total count
// matching the filter (ignoring pagination).
func (s *auth) ListUsers(
	ctx context.Context,
	f UserFilter,
) ([]*ent.User, int, error) {
	ctx, span := tracer.Start(ctx, "users.list")
	defer span.End()

	items, total, err := s.db.ListUsers(ctx, db.ListUsersParams{
		Q:      f.Q,
		Role:   user.Role(f.Role),
		Limit:  uint32(f.Limit),
		Offset: f.Offset,
		Sort:   f.Sort,
		Order:  f.Order,
	})
	if err != nil {
		return nil, 0, otelx.RecordSpanError(span, fmt.Errorf("list users: %w", err))
	}
	return items, total, nil
}

// CreateUserDirect creates a user with an admin-supplied password, bypassing
// the invite flow. Returns ErrUserEmailExists when the email is already in
// use and ErrPasswordWeak when the password fails policy.
func (s *auth) CreateUserDirect(
	ctx context.Context,
	email, password, role, displayName string,
) (*ent.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))

	ctx, span := tracer.Start(ctx, "users.create",
		trace.WithAttributes(
			semconv.UserEmail(email),
			semconv.UserRoles(role),
		),
	)
	defer span.End()

	if err := validatePassword(password); err != nil {
		return nil, err
	}

	if _, err := s.db.FindUserByEmail(ctx, email); err == nil {
		return nil, ErrUserEmailExists
	} else if !ent.IsNotFound(err) {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("lookup email: %w", err))
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("hash password: %w", err))
	}

	u, err := s.db.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		DisplayName:  strings.TrimSpace(displayName),
		PasswordHash: string(hash),
		Role:         user.Role(role),
		AuthMethod:   user.AuthMethodLocal,
	})
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("create user: %w", err))
	}
	span.SetAttributes(semconv.UserID(fmt.Sprint(u.ID)))
	slog.InfoContext(ctx, "admin_user_created",
		"user.id", u.ID, "user.email", u.Email, "user.role", role)
	return u, nil
}

// GetUserDetail returns the user plus every API key and session owned by
// them, for the admin detail page. Returns ErrUserNotFound when the id does
// not resolve.
func (s *auth) GetUserDetail(
	ctx context.Context,
	id uint32,
) (*ent.User, []*ent.ApiKey, []*ent.Session, error) {
	u, err := s.db.FindUserByID(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil, nil, ErrUserNotFound
		}
		return nil, nil, nil, err
	}
	keys, err := s.db.ListAPIKeysByUser(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	sessions, err := s.db.ListUserSessions(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	return u, keys, sessions, nil
}

// UpdateUser applies a partial patch (role, display name, auth method) to the
// target user. Demoting the sole remaining admin is rejected with
// ErrLastAdmin.
func (s *auth) UpdateUser(
	ctx context.Context,
	id uint32,
	p UserPatch,
) error {
	ctx, span := tracer.Start(ctx, "users.update",
		trace.WithAttributes(semconv.UserID(fmt.Sprint(id))),
	)
	defer span.End()

	current, err := s.db.FindUserByID(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrUserNotFound
		}
		return otelx.RecordSpanError(span, fmt.Errorf("load user: %w", err))
	}

	if p.Role != nil && *p.Role != string(current.Role) {
		if current.Role == user.RoleAdmin && *p.Role != string(user.RoleAdmin) {
			n, err := s.db.CountUsersByRole(ctx, user.RoleAdmin)
			if err != nil {
				return otelx.RecordSpanError(
					span,
					fmt.Errorf("count admins: %w", err),
				)
			}
			if n <= 1 {
				return ErrLastAdmin
			}
		}
	}

	params := db.UpdateUserParams{}
	if p.Role != nil {
		r := user.Role(*p.Role)
		params.Role = &r
	}
	if p.AuthMethod != nil {
		a := user.AuthMethod(*p.AuthMethod)
		params.AuthMethod = &a
	}
	if p.DisplayName != nil {
		dn := strings.TrimSpace(*p.DisplayName)
		params.DisplayName = &dn
	}
	if p.Email != nil {
		email := strings.ToLower(strings.TrimSpace(*p.Email))
		if email != strings.ToLower(current.Email) {
			existing, err := s.db.FindUserByEmail(ctx, email)
			if err != nil && !ent.IsNotFound(err) {
				return otelx.RecordSpanError(
					span,
					fmt.Errorf("check email uniqueness: %w", err),
				)
			}
			if existing != nil && existing.ID != id {
				return ErrUserEmailExists
			}
			params.Email = &email
		}
	}

	if _, err := s.db.UpdateUser(ctx, id, params); err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("update user: %w", err))
	}

	if p.Role != nil && *p.Role != string(current.Role) {
		slog.InfoContext(ctx, "user_role_changed",
			"user.id", id,
			"old_role", string(current.Role),
			"new_role", *p.Role,
		)
	}
	return nil
}

// DeleteUser permanently removes the user. The caller-provided requesterID
// is the admin performing the action; self-delete is rejected with
// ErrSelfDeleteForbidden. The last admin cannot be deleted (ErrLastAdmin).
// All rows owned by the user (api keys, oidc identities, sessions, requests)
// cascade at the schema level for GDPR compliance.
func (s *auth) DeleteUser(
	ctx context.Context,
	id, requesterID uint32,
) error {
	ctx, span := tracer.Start(ctx, "users.delete",
		trace.WithAttributes(
			semconv.UserID(fmt.Sprint(id)),
			attribute.String("actor.user.id", fmt.Sprint(requesterID)),
		),
	)
	defer span.End()

	if id == requesterID {
		return ErrSelfDeleteForbidden
	}

	target, err := s.db.FindUserByID(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrUserNotFound
		}
		return otelx.RecordSpanError(span, fmt.Errorf("load user: %w", err))
	}

	if target.Role == user.RoleAdmin {
		n, err := s.db.CountUsersByRole(ctx, user.RoleAdmin)
		if err != nil {
			return otelx.RecordSpanError(span, fmt.Errorf("count admins: %w", err))
		}
		if n <= 1 {
			return ErrLastAdmin
		}
	}

	if err := s.db.DeleteUser(ctx, id); err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("delete user: %w", err))
	}
	slog.InfoContext(ctx, "user_deleted",
		"user.id", id, "actor.user.id", requesterID)
	return nil
}

// AdminResetPassword rotates the target user's password without verifying the
// old one and revokes every active session they hold. Returns ErrPasswordWeak
// when the new password fails policy, ErrUserNotFound when the id does not
// resolve.
func (s *auth) AdminResetPassword(
	ctx context.Context,
	id uint32,
	newPassword string,
) error {
	ctx, span := tracer.Start(ctx, "users.reset_password",
		trace.WithAttributes(semconv.UserID(fmt.Sprint(id))),
	)
	defer span.End()

	if err := validatePassword(newPassword); err != nil {
		return err
	}
	if _, err := s.db.FindUserByID(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return ErrUserNotFound
		}
		return otelx.RecordSpanError(span, fmt.Errorf("load user: %w", err))
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("hash password: %w", err))
	}
	if err := s.db.UpdateUserPassword(ctx, id, string(hash)); err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("update password: %w", err))
	}
	if err := s.RevokeAllUserSessions(ctx, id); err != nil {
		// Best-effort: the password is already rotated. Log and carry on so
		// the admin flow succeeds even if the session revoke fails.
		slog.WarnContext(ctx, "revoke_all_sessions_failed",
			"user.id", id, "error", err)
	}
	slog.InfoContext(ctx, "admin_password_reset", "user.id", id)
	return nil
}

// AdminRevokeAPIKey revokes the target user's API key. Delegates to the
// ownership-scoped RevokeAPIKeyByID so the semantics match the self-service
// DELETE /auth/me/api-keys/{id} path.
func (s *auth) AdminRevokeAPIKey(
	ctx context.Context,
	userID, keyID uint32,
) error {
	return s.RevokeAPIKeyByID(ctx, userID, keyID)
}

// AdminRevokeSession revokes the target user's session. Delegates to the
// ownership-scoped RevokeSessionByID so the semantics match the self-service
// DELETE /auth/me/sessions/{id} path.
func (s *auth) AdminRevokeSession(
	ctx context.Context,
	userID, sessionID uint32,
) error {
	return s.RevokeSessionByID(ctx, userID, sessionID)
}
