// Package role holds the role a user row may be written with, as a value
// nothing outside this package can build.
//
// db.CreateUserParams and db.UpdateUserParams take a Value rather than an
// ent role, so every write names the authority that decided it: an operator
// acting through the admin API, the first-boot seed, an invite, a
// self-registration, or a login through an OIDC provider. A bare
// `entuser.RoleAdmin` no longer compiles into a params struct, which is what
// this package buys over a lint that looks for one.
//
// The escalation it exists to stop is a role reaching a user row without
// passing the OIDC provider's admin ceiling. Four rounds of that fix kept the
// type in package auth and enforced the rule with a syntax scan over the
// package's AST; four times a reviewer found a spelling the scan did not
// enumerate. The last one needed no new API at all:
//
//	var esc oidcRole
//	esc.role = string(entuser.RoleAdmin)
//	mapped = esc
//
// — legal because the field was reachable from every file that shared the
// package, and invisible because the forged value then left through the
// blessed exit the scan was told to trust.
//
// A package boundary is what makes that unwritable rather than unseen. Value's
// field is unexported and this package exports no setter and no pointer to its
// innards, so the constructors below are the only expressions in the program
// that yield a non-zero Value. Composite literals, field assignment and a
// params type invented next year all fail the same way: the compiler refuses
// them, in package auth and everywhere else.
//
// Three properties hold that up and must survive any edit here:
//
//   - The zero value is the safe one. `var v Value` is "no role decided", which
//     every caller reads as "leave the column alone" — so the shape a forger
//     can still write is inert rather than privileged.
//   - Federated applies the ceiling itself. It is the only federated
//     constructor, so there is no way to obtain an OIDC-sourced Value that
//     skipped it. Splitting "cap it" from "wrap it" would hand the OIDC path an
//     uncapped door spelled in blessed calls.
//   - Federated takes the provider's *name*, not its config. A ceiling passed in
//     as a value could be written at the call site, and
//     Federated(config.OIDCConfig{AllowAdmin: true}, "admin") would be an
//     escalation spelled entirely in blessed calls. Named, the ceiling can only
//     come from what the operator configured for that provider.
//
// What it does NOT buy: the four non-OIDC constructors are exported, so code on
// an OIDC login's path could call one of them instead of Federated. Go cannot
// express "only these packages may call this". That residue is a handful of
// named, greppable calls rather than unbounded syntax, and it is what
// `Describe("OIDC role writes")` in internal/auth checks.
package role

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

// Value is a role that has been decided by a named authority. Its zero value
// means "no role decided" — every caller leaves the column alone.
type Value struct{ role string }

func (v Value) Empty() bool { return v.role == "" }

func (v Value) String() string { return v.role }

// Ent and EntPtr are the only exits: they turn a Value into what
// db.CreateUserParams and db.UpdateUserParams accept. Nothing else here hands
// out the string, so a role that reaches the store through one of them was
// built by a constructor in this file.
func (v Value) Ent() entuser.Role { return entuser.Role(v.role) }

// EntPtr is Ent for db.UpdateUserParams, whose optional fields are pointers. It
// exists so an update path never has to spell out an entuser.Role of its own to
// take the address of.
func (v Value) EntPtr() *entuser.Role {
	r := v.Ent()
	return &r
}

// Operator is a role an authenticated admin chose, through the users API.
// Invited is the role an admin bound to an invite when they issued it. Seed is
// the first-boot admin, decided by config before anyone can log in.
//
// None of them caps anything: each names a decision the operator already made
// directly about a specific account, unlike a claim arriving from an IdP. They
// are separate functions rather than one so a role write says in its own text
// where its authority came from, and so the OIDC guard has names to check for
// rather than shapes.
func Operator(r entuser.Role) Value { return known(string(r)) }

// Seed is the first-boot admin. See Operator.
func Seed(r entuser.Role) Value { return known(string(r)) }

// Invited is the role an invite carried. See Operator.
func Invited(r entuser.Role) Value { return known(string(r)) }

// SelfRegistered is the role an open registration lands on: auth.oidc_default_role,
// applied to an account an anonymous request just created for itself.
//
// It clamps admin to member, exactly as Federated clamps that same key's admin
// down for a provider without allow_admin. The key is read on two paths and
// only the OIDC one has a provider whose allow_admin can vouch for admin, so
// leaving it uncapped here would let an operator who set oidc_default_role:
// admin for a provider they do trust hand admin to whoever posts /auth/register
// first — the inversion where presenting no identity at all outranks a
// federated login.
func SelfRegistered(r entuser.Role) Value {
	if r == entuser.RoleAdmin {
		r = entuser.RoleMember
	}
	return known(string(r))
}

// known wraps a role the caller vouched for, refusing one this build does not
// rank. An unranked string would otherwise reach the column and satisfy no
// RBAC comparison, since AtLeast fails closed on both sides.
func known(r string) Value {
	if _, ok := rank[r]; !ok {
		return Value{}
	}
	return Value{role: r}
}

// Federated is the single gate every role a login through the named provider
// can write passes through, whatever its source: candidates are the roles the
// provider's claims map to, fallback the operator-set role to land on when none
// of them survives (auth.oidc_default_role, or the role an invite carries).
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
func Federated(provider, fallback string, candidates ...string) Value {
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
	return known(best)
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
