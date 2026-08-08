// Package oidcrole holds the role an OIDC login is allowed to confer, as a
// value nothing outside this package can build.
//
// The escalation it exists to stop is a role reaching a user row without
// passing the provider's admin ceiling. Four rounds of that fix kept the type
// in package auth and enforced the rule with a syntax scan over the package's
// AST; four times a reviewer found a spelling the scan did not enumerate. The
// last one needed no new API at all:
//
//	var esc oidcRole
//	esc.role = string(entuser.RoleAdmin)
//	mapped = esc
//
// — legal because the field was reachable from every file that shared the
// package, and invisible because the forged value then left through the
// blessed exit the scan was told to trust.
//
// A package boundary is what makes that unwritable rather than unseen. Role's
// field is unexported and this package exports no setter, no constructor
// taking a role, and no pointer to its innards, so Cap is the only expression
// in the program that yields a non-zero Role. Composite literals, field
// assignment and a params type invented next year all fail the same way: the
// compiler refuses them, in package auth and everywhere else.
//
// Two properties hold that up and must survive any edit here:
//
//   - The zero value is the safe one. `var r Role` is "no role decided", which
//     every caller reads as "leave the column alone" — so the shape a forger
//     can still write is inert rather than privileged.
//   - Cap takes the provider's *name*, not its config. A ceiling passed in as
//     a value could be written at the call site, and Cap(config.OIDCConfig{
//     AllowAdmin: true}, "admin") would be an escalation spelled entirely in
//     blessed calls. Named, the ceiling can only come from what the operator
//     configured for that provider.
package oidcrole

import (
	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/config"
)

// rank orders roles by privilege, so the ceiling can pick the
// highest-privilege candidate a provider may confer and RBAC can compare a
// session's role against an endpoint's minimum. Both readings share one table
// on purpose: a role added to the product ranks once, not twice.
var rank = map[string]int{"request_only": 1, "member": 2, "admin": 3}

// AtLeast reports whether role carries at least the privilege of min.
// Fails closed: an unrecognised role or minimum satisfies nothing.
func AtLeast(role, min string) bool {
	r, ok := rank[role]
	m, okMin := rank[min]
	return ok && okMin && r >= m
}

// Role is a Streamline role that has passed a provider's admin ceiling. Its
// zero value means "no role decided" — every caller leaves the column alone.
type Role struct{ role string }

func (r Role) Empty() bool { return r.role == "" }

func (r Role) String() string { return r.role }

// EntRole and EntRolePtr are the only exits: they turn a capped Role into what
// db.CreateUserParams and db.UpdateUserParams accept. Nothing else in this
// package hands out the string, so a role that reaches the store through one
// of them went through Cap.
func (r Role) EntRole() entuser.Role { return entuser.Role(r.role) }

// EntRolePtr is EntRole for db.UpdateUserParams, whose optional fields are
// pointers. It exists so the update path never has to spell out an
// entuser.Role of its own to take the address of.
func (r Role) EntRolePtr() *entuser.Role {
	role := r.EntRole()
	return &role
}

// Cap is the single gate every role a login through the named provider can
// write passes through, whatever its source: candidates are the roles the
// provider's claims map to, fallback the operator-set role to land on when
// none of them survives (auth.oidc_default_role, or the role an invite
// carries).
//
// The winner is the highest-privilege candidate the provider may confer, and
// fallback only when no candidate qualifies. A provider without allow_admin
// never yields admin from either:
//
//   - Among candidates, admin is dropped rather than downgraded. A user in both
//     an admin group and a member group still lands on member, and one in an
//     admin group alone matches nothing — so a claim the provider may not
//     honour leaves the role it already had untouched instead of re-ranking it.
//   - A fallback of admin is clamped to member, because a provisioning login
//     has to land somewhere. An operator who sets auth.oidc_default_role: admin
//     and leaves allow_admin off gets members, not the perverse inversion where
//     withholding every mapped group outranks presenting one.
//
// It reads nothing about the account and nothing about the request — not the
// auth_method, not the email_linking tier, not which claims arrived. That is
// the point: a ceiling derived from any of those moves when an attacker or an
// operator moves them, and each of the first three rounds of this fix fell to a
// different ordering that exploited exactly that.
func Cap(provider, fallback string, candidates ...string) Role {
	allowAdmin := providerAllowsAdmin(provider)
	admin := string(entuser.RoleAdmin)
	best := ""
	for _, c := range candidates {
		if c == admin && !allowAdmin {
			continue
		}
		if rank[c] > rank[best] {
			best = c
		}
	}
	if best == "" {
		best = fallback
	}
	if best == admin && !allowAdmin {
		best = string(entuser.RoleMember)
	}
	if _, ok := rank[best]; !ok {
		return Role{}
	}
	return Role{role: best}
}

// providerAllowsAdmin reports whether the operator opted the named provider
// into granting admin.
//
// It fails closed twice over: a provider missing from the config — one removed
// mid-flight, or a name nobody configured — confers no admin rather than
// inheriting whatever replaced it, and a config singleton that was never
// loaded answers the same way instead of panicking a login path.
func providerAllowsAdmin(provider string) bool {
	cfg := config.Get()
	if cfg == nil {
		return false
	}
	for _, p := range cfg.Auth.OIDC {
		if p.Name == provider {
			return p.AllowAdmin
		}
	}
	return false
}
