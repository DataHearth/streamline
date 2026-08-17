package auth

import (
	"context"

	approle "github.com/datahearth/streamline/internal/role"
)

// RoleAtLeast reports whether role carries at least the privilege of min.
// Fails closed: an unrecognised role or minimum satisfies nothing.
//
// The ordering itself lives in role because the OIDC ceiling ranks claim
// candidates against it, and that package cannot import this one.
func RoleAtLeast(role, min string) bool { return approle.AtLeast(role, min) }

// IsAdmin reports whether ctx carries admin claims. It is the single predicate
// behind every "admin only" gate — the REST role guard, the root-router web
// routes, and the body-limit carve-out — so the three cannot drift apart on
// what admin means. Each caller still writes its own refusal, because a JSON
// API error, a cookie-session error and a middleware short-circuit are not the
// same response.
//
// Ranked with RoleAtLeast rather than compared to the literal, so a role added
// above admin would satisfy it instead of silently locking admins out.
func IsAdmin(ctx context.Context) bool {
	c := ClaimsFromContext(ctx)
	return c != nil && RoleAtLeast(c.Role, "admin")
}
