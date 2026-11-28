package auth

import (
	"context"
	"sync/atomic"
)

type contextKey string

const (
	claimsKey     contextKey = "claims"
	claimsSlotKey contextKey = "claims_slot"
)

// ContextWithClaims returns a copy of ctx with the request caller's claims
// attached. Used by the HTTP auth middleware after authentication succeeds.
// The first claims attached also fill the slot ContextWithClaimsSlot put in
// ctx, so a later call cannot replace the caller there.
func ContextWithClaims(ctx context.Context, claims *Claims) context.Context {
	if slot, ok := ctx.Value(claimsSlotKey).(*atomic.Pointer[Claims]); ok {
		slot.CompareAndSwap(nil, claims)
	}
	return context.WithValue(ctx, claimsKey, claims)
}

// ClaimsFromContext extracts auth claims from ctx. Returns nil when no
// middleware has run upstream (e.g. /health probes that bypass auth).
func ClaimsFromContext(ctx context.Context) *Claims {
	c, _ := ctx.Value(claimsKey).(*Claims)
	return c
}

// ContextWithClaimsSlot lets a middleware that runs before authentication
// read the caller's claims once the handler returns: auth attaches them to a
// derived context the outer middleware never sees. The returned func yields
// nil until ContextWithClaims runs on ctx or a context derived from it.
func ContextWithClaimsSlot(ctx context.Context) (context.Context, func() *Claims) {
	slot := new(atomic.Pointer[Claims])
	return context.WithValue(ctx, claimsSlotKey, slot), slot.Load
}
