package auth

import "github.com/datahearth/streamline/internal/auth/oidcrole"

// RoleAtLeast reports whether role carries at least the privilege of min.
// Fails closed: an unrecognised role or minimum satisfies nothing.
//
// The ordering itself lives in oidcrole because the OIDC ceiling ranks claim
// candidates against it, and that package cannot import this one.
func RoleAtLeast(role, min string) bool { return oidcrole.AtLeast(role, min) }
