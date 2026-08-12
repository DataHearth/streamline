package auth

import approle "github.com/datahearth/streamline/internal/role"

// RoleAtLeast reports whether role carries at least the privilege of min.
// Fails closed: an unrecognised role or minimum satisfies nothing.
//
// The ordering itself lives in role because the OIDC ceiling ranks claim
// candidates against it, and that package cannot import this one.
func RoleAtLeast(role, min string) bool { return approle.AtLeast(role, min) }
