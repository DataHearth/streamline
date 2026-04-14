package auth

import "context"

type contextKey string

const claimsKey contextKey = "claims"

// ContextWithClaims returns a copy of ctx with claims attached. Used by the
// HTTP auth middleware after authentication succeeds.
func ContextWithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// ClaimsFromContext extracts auth claims from ctx. Returns nil when no
// middleware has run upstream (e.g. /health probes that bypass auth).
func ClaimsFromContext(ctx context.Context) *Claims {
	c, _ := ctx.Value(claimsKey).(*Claims)
	return c
}
