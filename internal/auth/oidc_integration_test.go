package auth

import (
	"context"
	"os"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
)

// LoginOIDC integration smoke — exercise full pipeline (real DB user create +
// real OIDC identity persistence + real session/JWT issuance). Per-branch
// coverage lives in oidc_test.go via MockStore.
var _ = Describe("LoginOIDC end-to-end", Label("integration", "auth"), func() {
	var (
		ctx      context.Context
		svc      *auth
		dbClient *ent.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		dbClient, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { dbClient.Close() })
		svc = newTestService(dbClient)

		config.ResetForTest()
		loadOIDCTestConfig("open", "member", config.OIDCEmailLinkingDisabled)
		DeferCleanup(config.ResetForTest)
	})

	It("creates user + OIDC identity + session row in open mode", func() {
		u, tok, err := svc.LoginOIDC(
			ctx,
			"google",
			"sub-1",
			"u@x.com",
			"U",
			true,
			nil,
			SessionMeta{},
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(u.Email).To(Equal("u@x.com"))
		Expect(u.AuthMethod).To(Equal(entuser.AuthMethodOidc))
		Expect(tok).NotTo(BeEmpty())

		// Identity persisted with correct (provider, subject) pair.
		id, err := dbClient.OIDCIdentity.Query().Only(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(id.Provider).To(Equal("google"))
		Expect(id.Subject).To(Equal("sub-1"))

		// Session row was created so the JWT we returned is backed.
		claims, err := svc.ValidateToken(tok)
		Expect(err).ToNot(HaveOccurred())
		Expect(svc.ValidateSession(ctx, claims.JTI)).To(Succeed())
	})

	It(
		"refuses to adopt a local account by email without an opt-in, persisting nothing",
		func() {
			u, err := dbClient.User.Create().
				SetEmail("victim@x.com").
				SetPasswordHash("hash").
				SetRole(entuser.RoleMember).
				SetAuthMethod(entuser.AuthMethodLocal).
				Save(ctx)
			Expect(err).ToNot(HaveOccurred())

			_, tok, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-evil",
				"victim@x.com",
				"",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ErrOIDCLinkNotAllowed))
			Expect(tok).To(BeEmpty())

			Expect(dbClient.OIDCIdentity.Query().Count(ctx)).To(Equal(0))
			Expect(dbClient.Session.Query().Count(ctx)).To(Equal(0))
			reloaded, err := dbClient.User.Get(ctx, u.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(reloaded.AuthMethod).To(Equal(entuser.AuthMethodLocal))
		},
	)

	It(
		"adopts the existing local user once the provider opts in (case-insensitive email)",
		func() {
			config.ResetForTest()
			loadOIDCTestConfig("open", "member", config.OIDCEmailLinkingNonAdmin)

			u, err := dbClient.User.Create().
				SetEmail("case@x.com").
				SetPasswordHash("hash").
				SetRole(entuser.RoleMember).
				SetAuthMethod(entuser.AuthMethodLocal).
				Save(ctx)
			Expect(err).ToNot(HaveOccurred())

			got, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-9",
				"CASE@x.com",
				"",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.ID).To(Equal(u.ID))
			Expect(got.AuthMethod).To(Equal(entuser.AuthMethodBoth))
		},
	)

	It("refuses to adopt the seeded admin even with the non_admin opt-in", func() {
		config.ResetForTest()
		loadOIDCTestConfig("open", "member", config.OIDCEmailLinkingNonAdmin)

		admin, err := dbClient.User.Create().
			SetEmail("admin@streamline.local").
			SetPasswordHash("hash").
			SetRole(entuser.RoleAdmin).
			SetAuthMethod(entuser.AuthMethodLocal).
			Save(ctx)
		Expect(err).ToNot(HaveOccurred())

		_, _, err = svc.LoginOIDC(ctx, "google", "sub-evil",
			"admin@streamline.local", "", true, nil, SessionMeta{})
		Expect(err).To(MatchError(ErrOIDCLinkNotAllowed))

		Expect(dbClient.OIDCIdentity.Query().Count(ctx)).To(Equal(0))
		reloaded, err := dbClient.User.Get(ctx, admin.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(reloaded.AuthMethod).To(Equal(entuser.AuthMethodLocal))
	})

	It(
		"adopts under non_admin but never applies an admin mapping",
		func() {
			config.ResetForTest()
			loadOIDCRoleMapConfig(config.OIDCEmailLinkingNonAdmin)

			victim, err := dbClient.User.Create().
				SetEmail("victim@x.com").SetPasswordHash("hash").
				SetRole(entuser.RoleMember).
				SetAuthMethod(entuser.AuthMethodLocal).
				Save(ctx)
			Expect(err).ToNot(HaveOccurred())

			got, tok, err := svc.LoginOIDC(ctx, "kc", "sub-evil", "victim@x.com",
				"Not The Victim", true,
				map[string]any{"groups": []any{"streamline-admins"}},
				SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(tok).NotTo(BeEmpty())
			Expect(got.ID).To(Equal(victim.ID))

			reloaded, err := dbClient.User.Get(ctx, victim.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(reloaded.Role).To(Equal(entuser.RoleMember))
			Expect(reloaded.AuthMethod).To(Equal(entuser.AuthMethodBoth))
		},
	)

	// Lowering a provider's tier must not hand it anything it could not do at
	// the looser one. The account an adoption leaves behind is `both`, which is
	// what disabled keys its refusal on.
	It(
		"keeps an account adopted under non_admin off admin after a flip to disabled",
		func() {
			config.ResetForTest()
			loadOIDCRoleMapConfig(config.OIDCEmailLinkingNonAdmin)

			victim, err := dbClient.User.Create().
				SetEmail("victim@x.com").SetPasswordHash("hash").
				SetRole(entuser.RoleMember).
				SetAuthMethod(entuser.AuthMethodLocal).
				Save(ctx)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = svc.LoginOIDC(ctx, "kc", "sub-evil", "victim@x.com", "",
				true, map[string]any{"groups": []any{"streamline-staff"}},
				SessionMeta{})
			Expect(err).ToNot(HaveOccurred())

			By("the operator closing email_linking back down to disabled")
			config.ResetForTest()
			loadOIDCRoleMapConfig(config.OIDCEmailLinkingDisabled)

			again, _, err := svc.LoginOIDC(ctx, "kc", "sub-evil", "victim@x.com",
				"", true, map[string]any{"groups": []any{"streamline-admins"}},
				SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(again.Role).ToNot(Equal(entuser.RoleAdmin))

			reloaded, err := dbClient.User.Get(ctx, victim.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(reloaded.Role).ToNot(Equal(entuser.RoleAdmin))
		},
	)

	It("refuses to provision a new admin under non_admin", func() {
		config.ResetForTest()
		loadOIDCRoleMapConfig(config.OIDCEmailLinkingNonAdmin)

		u, _, err := svc.LoginOIDC(ctx, "kc", "sub-new", "new@x.com", "N", true,
			map[string]any{"groups": []any{"streamline-admins"}}, SessionMeta{})
		Expect(err).ToNot(HaveOccurred())
		// The admins group is the only one presented, so nothing maps and the
		// user falls back to oidc_default_role.
		Expect(u.Role).To(Equal(entuser.RoleMember))
	})

	// Round 4 POC (B). oidc_default_role is what a provisioning login lands on
	// when no claim maps, and it is operator-set — but it is still a role this
	// provider confers, so the ceiling has to reach it too. Without that, an
	// install whose default is admin inverts: presenting the mapped admin group
	// yields member (the group is barred), while presenting nothing at all
	// yields admin.
	It("caps an oidc_default_role of admin for a provider without the ceiling",
		func() {
			config.ResetForTest()
			loadOIDCRoleMapConfigDefault(
				config.OIDCEmailLinkingNonAdmin,
				false,
				"admin",
			)

			By("a login carrying no mapped group at all")
			u, _, err := svc.LoginOIDC(ctx, "kc", "sub-bare", "bare@x.com", "B",
				true, nil, SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(u.Role).ToNot(Equal(entuser.RoleAdmin))

			reloaded, err := dbClient.User.Get(ctx, u.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(reloaded.Role).ToNot(Equal(entuser.RoleAdmin))
		})

	// Round 4 POC (A), single-provider shape. The account is of federated
	// origin, so nothing about it ever carries a local password — the auth_method
	// an adoption leaves behind is not a usable proxy for "this provider was
	// only ever trusted at the lower tier".
	It(
		"keeps a provider it provisioned under non_admin off admin after a flip to disabled",
		func() {
			config.ResetForTest()
			loadOIDCRoleMapConfig(config.OIDCEmailLinkingNonAdmin)

			By("provisioning the account while non_admin is in force")
			u, _, err := svc.LoginOIDC(ctx, "kc", "sub-evil", "new@x.com", "N",
				true, map[string]any{"groups": []any{"streamline-admins"}},
				SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(u.Role).To(Equal(entuser.RoleMember))
			Expect(u.AuthMethod).To(Equal(entuser.AuthMethodOidc))

			By("the operator tightening the provider back down to disabled")
			config.ResetForTest()
			loadOIDCRoleMapConfig(config.OIDCEmailLinkingDisabled)

			again, _, err := svc.LoginOIDC(ctx, "kc", "sub-evil", "new@x.com", "N",
				true, map[string]any{"groups": []any{"streamline-admins"}},
				SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(again.Role).ToNot(Equal(entuser.RoleAdmin))

			reloaded, err := dbClient.User.Get(ctx, u.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(reloaded.Role).ToNot(Equal(entuser.RoleAdmin))
		},
	)

	// Round 4 POC (A), two-provider shape: the escalation completes by making
	// the setting *stricter*. A weak provider adopts an account the strong one
	// created, which leaves auth_method at oidc; tightening the weak provider to
	// disabled then reads that as "an account of my own population" and hands it
	// the admin group it was refusing a moment earlier.
	It(
		"never lets a second provider promote an account the first one created",
		func() {
			config.ResetForTest()
			loadOIDCTwoProviderConfig(config.OIDCEmailLinkingNonAdmin)

			By("step 0: alice signs in through the trusted provider")
			alice, _, err := svc.LoginOIDC(ctx, "corp", "corp-alice",
				"alice@corp.com", "Alice", true, nil, SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(alice.Role).To(Equal(entuser.RoleMember))
			Expect(alice.AuthMethod).To(Equal(entuser.AuthMethodOidc))

			By("step 2: the attacker self-asserts her address at partner")
			adopted, _, err := svc.LoginOIDC(ctx, "partner", "partner-evil",
				"alice@corp.com", "Not Alice", true,
				map[string]any{"groups": []any{"streamline-admins"}},
				SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(adopted.ID).To(Equal(alice.ID))
			Expect(adopted.Role).To(Equal(entuser.RoleMember))

			By("step 3: the operator tightens partner back to disabled")
			config.ResetForTest()
			loadOIDCTwoProviderConfig(config.OIDCEmailLinkingDisabled)

			By("step 4: the attacker signs in again through partner")
			again, _, err := svc.LoginOIDC(ctx, "partner", "partner-evil",
				"alice@corp.com", "Not Alice", true,
				map[string]any{"groups": []any{"streamline-admins"}},
				SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(again.Role).ToNot(Equal(entuser.RoleAdmin))

			reloaded, err := dbClient.User.Get(ctx, alice.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(reloaded.Role).ToNot(Equal(entuser.RoleAdmin))
		},
	)

	// The escalation any per-request gate misses: the attacker picks which
	// claims each request carries, so a gate reading only this request's claims
	// is defeated by splitting the attack in two. Request 1 withholds the admin
	// group to slip past the tier check and bind the identity; request 2
	// presents it against the link that is now established.
	It(
		"never lets a non_admin provider reach admin, even across two logins",
		func() {
			config.ResetForTest()
			loadOIDCRoleMapConfig(config.OIDCEmailLinkingNonAdmin)

			victim, err := dbClient.User.Create().
				SetEmail("victim@x.com").SetPasswordHash("hash").
				SetRole(entuser.RoleMember).
				SetAuthMethod(entuser.AuthMethodLocal).
				Save(ctx)
			Expect(err).ToNot(HaveOccurred())

			By("request 1: withholding the admin group so adoption is permitted")
			got, tok, err := svc.LoginOIDC(ctx, "kc", "sub-evil", "victim@x.com",
				"Not The Victim", true,
				map[string]any{"groups": []any{"streamline-staff"}},
				SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(got.ID).To(Equal(victim.ID))
			Expect(got.Role).To(Equal(entuser.RoleMember))
			Expect(tok).NotTo(BeEmpty())

			By("request 2: presenting the admin group against the fresh link")
			again, _, err := svc.LoginOIDC(ctx, "kc", "sub-evil", "victim@x.com",
				"Not The Victim", true,
				map[string]any{"groups": []any{"streamline-admins"}},
				SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(again.Role).ToNot(Equal(entuser.RoleAdmin))

			reloaded, err := dbClient.User.Get(ctx, victim.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(reloaded.Role).ToNot(Equal(entuser.RoleAdmin))
		},
	)

	// README, values.yaml, CLAUDE.md, the JSON schema and the OIDCConfig doc all
	// said disabled "reaches only accounts it created itself". It does not: the
	// tier is read at adoption, and every later login matches the identity the
	// adoption left behind. Documenting the migration procedure and that claim
	// at once was a contradiction — this is the shape the procedure relies on.
	It(
		"keeps signing in as an adopted local account after the tier closes",
		func() {
			config.ResetForTest()
			loadOIDCRoleMapConfig(config.OIDCEmailLinkingNonAdmin)

			victim, err := dbClient.User.Create().
				SetEmail("victim@x.com").SetPasswordHash("hash").
				SetRole(entuser.RoleMember).
				SetAuthMethod(entuser.AuthMethodLocal).
				Save(ctx)
			Expect(err).ToNot(HaveOccurred())

			By("the one pass the migration procedure asks for")
			adopted, _, err := svc.LoginOIDC(ctx, "kc", "sub-1",
				"victim@x.com", "", true, nil, SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(adopted.ID).To(Equal(victim.ID))

			By("closing the tier again")
			config.ResetForTest()
			loadOIDCRoleMapConfig(config.OIDCEmailLinkingDisabled)

			again, tok, err := svc.LoginOIDC(ctx, "kc", "sub-1",
				"victim@x.com", "", true, nil, SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(again.ID).To(Equal(victim.ID))
			Expect(tok).NotTo(BeEmpty())
			Expect(again.Role).To(Equal(entuser.RoleMember))
		},
	)

	It("adopts under email_linking: all without raising the role", func() {
		config.ResetForTest()
		loadOIDCRoleMapConfigDefault(config.OIDCEmailLinkingAll, true, "member")

		victim, err := dbClient.User.Create().
			SetEmail("victim@x.com").SetPasswordHash("hash").
			SetRole(entuser.RoleMember).SetAuthMethod(entuser.AuthMethodLocal).
			Save(ctx)
		Expect(err).ToNot(HaveOccurred())

		got, _, err := svc.LoginOIDC(ctx, "kc", "sub-evil", "victim@x.com", "",
			true, map[string]any{"groups": []any{"streamline-admins"}},
			SessionMeta{})
		Expect(err).ToNot(HaveOccurred())
		Expect(got.ID).To(Equal(victim.ID))
		Expect(got.Role).To(Equal(entuser.RoleMember))

		reloaded, err := dbClient.User.Get(ctx, victim.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(reloaded.Role).To(Equal(entuser.RoleMember))
		Expect(reloaded.AuthMethod).To(Equal(entuser.AuthMethodBoth))

		By("applying the mapped role from the next login, the identity now linked")
		// The promotion here is allow_admin's doing, not email_linking's: the
		// operator declared this provider fit to decide who administers
		// Streamline. Adoption only decided which row the login lands on.
		again, _, err := svc.LoginOIDC(ctx, "kc", "sub-evil", "victim@x.com", "",
			true, map[string]any{"groups": []any{"streamline-admins"}},
			SessionMeta{})
		Expect(err).ToNot(HaveOccurred())
		Expect(again.Role).To(Equal(entuser.RoleAdmin))
	})

	// The same adoption with the ceiling left at its default. email_linking is
	// at its loosest, so nothing about the adoption changes — and the role
	// still cannot move, which is the axis split stated as a test.
	It("adopts under email_linking: all and still refuses admin", func() {
		config.ResetForTest()
		loadOIDCRoleMapConfigDefault(config.OIDCEmailLinkingAll, false, "member")

		victim := seedLocalUser(ctx, dbClient, "victim@x.com", entuser.RoleMember)

		got, _, err := svc.LoginOIDC(ctx, "kc", "sub-evil", "victim@x.com", "",
			true, map[string]any{"groups": []any{"streamline-admins"}},
			SessionMeta{})
		Expect(err).ToNot(HaveOccurred())
		Expect(got.ID).To(Equal(victim.ID))

		again, _, err := svc.LoginOIDC(ctx, "kc", "sub-evil", "victim@x.com", "",
			true, map[string]any{"groups": []any{"streamline-admins"}},
			SessionMeta{})
		Expect(err).ToNot(HaveOccurred())
		Expect(again.Role).To(Equal(entuser.RoleMember))
	})

	It("matches an existing account on a claim email padded with spaces", func() {
		admin, err := dbClient.User.Create().
			SetEmail("admin@streamline.local").SetPasswordHash("hash").
			SetRole(entuser.RoleAdmin).SetAuthMethod(entuser.AuthMethodLocal).
			Save(ctx)
		Expect(err).ToNot(HaveOccurred())

		_, _, err = svc.LoginOIDC(ctx, "google", "sub-evil",
			" Admin@Streamline.local ", "", true, nil, SessionMeta{})
		Expect(err).To(MatchError(ErrOIDCLinkNotAllowed))

		Expect(dbClient.User.Query().Count(ctx)).To(Equal(1))
		Expect(dbClient.OIDCIdentity.Query().Count(ctx)).To(Equal(0))
		reloaded, err := dbClient.User.Get(ctx, admin.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(reloaded.Email).To(Equal("admin@streamline.local"))
	})

	// The two specs below pin the boundary of what the entry normalisation
	// reaches. U+212A KELVIN SIGN lowercases to "k" under Go's simple case
	// folding, so it resolves to the local account and the email_linking gate
	// gets to rule on it.
	It("resolves a case-folding homoglyph onto the account it matches", func() {
		seedLocalUser(ctx, dbClient, "kate@x.com", entuser.RoleMember)

		_, _, err := svc.LoginOIDC(ctx, "google", "sub-look-alike",
			"\u212Aate@x.com", "", true, nil, SessionMeta{})
		Expect(err).To(MatchError(ErrOIDCLinkNotAllowed))
		Expect(dbClient.User.Query().Count(ctx)).To(Equal(1))
	})

	// Simple case folding is not NFKC, and nothing canonicalises the domain, so
	// these forms stay separate users. Deliberately: folding them would let two
	// addresses the IdP treats as distinct resolve onto one local account,
	// trading a confusing duplicate row for a match against somebody else's
	// account.
	DescribeTable("forks a row for a look-alike simple folding does not reach",
		func(claimEmail string) {
			seedLocalUser(ctx, dbClient, "kate@x.com", entuser.RoleMember)

			u, _, err := svc.LoginOIDC(ctx, "google", "sub-look-alike",
				claimEmail, "", true, nil, SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(u.Email).To(Equal(strings.ToLower(claimEmail)))
			Expect(dbClient.User.Query().Count(ctx)).To(Equal(2))
		},
		Entry("a fullwidth a", "k\uFF41te@x.com"),
		Entry("a trailing root dot", "kate@x.com."),
	)

	It("stores the trimmed address when provisioning a new user", func() {
		u, _, err := svc.LoginOIDC(ctx, "google", "sub-pad", " U@X.com ", "U",
			true, nil, SessionMeta{})
		Expect(err).ToNot(HaveOccurred())
		Expect(u.Email).To(Equal("u@x.com"))

		id, err := dbClient.OIDCIdentity.Query().Only(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(id.Email).To(Equal("u@x.com"))
	})

	It(
		"assigns the claim-mapped role to a new user, overriding the default",
		func() {
			config.ResetForTest()
			loadOIDCRoleMapConfigDefault(
				config.OIDCEmailLinkingDisabled,
				true,
				"member",
			)

			u, _, err := svc.LoginOIDC(ctx, "kc", "sub-a", "a@x.com", "A", true,
				map[string]any{"groups": []any{"streamline-admins"}}, SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			// oidc_default_role is "member"; the admins group maps to admin.
			Expect(u.Role).To(Equal(entuser.RoleAdmin))
		},
	)

	// The same signup with the ceiling at its default. email_linking is at its
	// strictest here and irrelevant either way — a provider nobody trusted with
	// admin does not get to name one, whatever it is set to.
	It("drops an admin mapping for a new user without allow_admin", func() {
		config.ResetForTest()
		loadOIDCRoleMapConfig(config.OIDCEmailLinkingDisabled)

		u, _, err := svc.LoginOIDC(ctx, "kc", "sub-a", "a@x.com", "A", true,
			map[string]any{"groups": []any{"streamline-admins"}}, SessionMeta{})
		Expect(err).ToNot(HaveOccurred())
		Expect(u.Role).To(Equal(entuser.RoleMember))
	})

	// Re-sync is a property of an already-linked identity, so the provider here
	// opts out of email linking entirely.
	It("re-syncs an already-linked user's role from claims on login", func() {
		u, err := dbClient.User.Create().
			SetEmail("b@x.com").SetPasswordHash("h").
			SetRole(entuser.RoleAdmin).SetAuthMethod(entuser.AuthMethodBoth).
			Save(ctx)
		Expect(err).ToNot(HaveOccurred())
		_, err = dbClient.OIDCIdentity.Create().
			SetProvider("kc").SetSubject("sub-b").SetEmail("b@x.com").
			SetOwnerID(u.ID).Save(ctx)
		Expect(err).ToNot(HaveOccurred())
		// A second admin so the demotion is not refused by the last-admin
		// guard, which the spec below covers on its own.
		seedLocalUser(ctx, dbClient, "other-admin@x.com", entuser.RoleAdmin)
		config.ResetForTest()
		loadOIDCRoleMapConfig(config.OIDCEmailLinkingDisabled)

		// Group now maps to member → the admin is demoted on login.
		got, _, err := svc.LoginOIDC(ctx, "kc", "sub-b", "b@x.com", "B", true,
			map[string]any{"groups": []any{"streamline-staff"}}, SessionMeta{})
		Expect(err).ToNot(HaveOccurred())
		Expect(got.ID).To(Equal(u.ID))
		Expect(got.Role).To(Equal(entuser.RoleMember))
	})

	// What turning allow_admin off does to an admin that is already linked,
	// measured rather than asserted — the README states both halves and this is
	// where they come from.
	Describe("an existing admin under a provider without allow_admin", func() {
		var admin *ent.User

		BeforeEach(func() {
			var err error
			admin, err = dbClient.User.Create().
				SetEmail("boss@x.com").SetPasswordHash("h").
				SetRole(entuser.RoleAdmin).SetAuthMethod(entuser.AuthMethodBoth).
				Save(ctx)
			Expect(err).ToNot(HaveOccurred())
			_, err = dbClient.OIDCIdentity.Create().
				SetProvider("kc").SetSubject("sub-boss").SetEmail("boss@x.com").
				SetOwnerID(admin.ID).Save(ctx)
			Expect(err).ToNot(HaveOccurred())
			config.ResetForTest()
			loadOIDCRoleMapConfig(config.OIDCEmailLinkingDisabled)
		})

		It("keeps the role when the claims map only to admin", func() {
			got, _, err := svc.LoginOIDC(ctx, "kc", "sub-boss", "boss@x.com", "",
				true, map[string]any{"groups": []any{"streamline-admins"}},
				SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Role).To(Equal(entuser.RoleAdmin))

			reloaded, err := dbClient.User.Get(ctx, admin.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(reloaded.Role).To(Equal(entuser.RoleAdmin))
		})

		It("demotes to the highest role the provider may still confer", func() {
			seedLocalUser(ctx, dbClient, "other-admin@x.com", entuser.RoleAdmin)

			got, _, err := svc.LoginOIDC(ctx, "kc", "sub-boss", "boss@x.com", "",
				true,
				map[string]any{
					"groups": []any{"streamline-admins", "streamline-staff"},
				},
				SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Role).To(Equal(entuser.RoleMember))
		})
	})

	// A claim change at the IdP must not be able to leave the instance with no
	// admin: there is no promote CLI and BootstrapSeedAdmin only re-seeds an
	// empty user table, so the demotion would be unrecoverable.
	Describe("the last admin's claims map to a lower role", func() {
		var admin *ent.User

		BeforeEach(func() {
			var err error
			admin, err = dbClient.User.Create().
				SetEmail("boss@x.com").SetPasswordHash("h").
				SetRole(entuser.RoleAdmin).SetAuthMethod(entuser.AuthMethodBoth).
				Save(ctx)
			Expect(err).ToNot(HaveOccurred())
			_, err = dbClient.OIDCIdentity.Create().
				SetProvider("kc").SetSubject("sub-boss").SetEmail("boss@x.com").
				SetOwnerID(admin.ID).Save(ctx)
			Expect(err).ToNot(HaveOccurred())
			config.ResetForTest()
			loadOIDCRoleMapConfig(config.OIDCEmailLinkingDisabled)
		})

		It("preserves the sole admin and still logs them in", func() {
			got, tok, err := svc.LoginOIDC(ctx, "kc", "sub-boss", "boss@x.com",
				"", true,
				map[string]any{"groups": []any{"streamline-staff"}},
				SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Role).To(Equal(entuser.RoleAdmin))

			reloaded, err := dbClient.User.Get(ctx, admin.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(reloaded.Role).To(Equal(entuser.RoleAdmin))

			Expect(tok).NotTo(BeEmpty())
			claims, err := svc.ValidateToken(tok)
			Expect(err).ToNot(HaveOccurred())
			Expect(svc.ValidateSession(ctx, claims.JTI)).To(Succeed())
		})

		It("resumes the demotion once another admin exists", func() {
			seedLocalUser(ctx, dbClient, "other-admin@x.com", entuser.RoleAdmin)

			got, _, err := svc.LoginOIDC(ctx, "kc", "sub-boss", "boss@x.com",
				"", true,
				map[string]any{"groups": []any{"streamline-staff"}},
				SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Role).To(Equal(entuser.RoleMember))

			reloaded, err := dbClient.User.Get(ctx, admin.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(reloaded.Role).To(Equal(entuser.RoleMember))
			Expect(dbClient.User.Query().
				Where(entuser.RoleEQ(entuser.RoleAdmin)).Count(ctx)).
				To(Equal(1))
		})
	})
})

// seedLocalUser inserts a password-backed local account, the shape every
// adoption spec starts from.
func seedLocalUser(
	ctx context.Context,
	dbClient *ent.Client,
	email string,
	role entuser.Role,
) *ent.User {
	GinkgoHelper()
	u, err := dbClient.User.Create().
		SetEmail(email).
		SetPasswordHash("hash").
		SetRole(role).
		SetAuthMethod(entuser.AuthMethodLocal).
		Save(ctx)
	Expect(err).ToNot(HaveOccurred())
	return u
}

// loadOIDCTwoProviderConfig seeds two providers sharing one role mapping:
// "corp", the trusted IdP that provisions its own population, and "partner",
// the weak one whose adoption tier the operator moves. Neither is trusted with
// admin, which is the whole point — an account "corp" created must not become
// reachable for promotion just because "partner" adopted it.
func loadOIDCTwoProviderConfig(partnerLinking string) {
	GinkgoHelper()
	yaml := `
data_dir: ` + os.TempDir() + `
auth:
  mode: disabled
  trusted_role: admin
  session_ttl: 168h
  registration_mode: open
  oidc_default_role: member
  oidc:
    - name: corp
      issuer: https://corp.example.com
      client_id: streamline
      client_secret: secret
      email_linking: disabled
    - name: partner
      issuer: https://partner.example.com
      client_id: streamline
      client_secret: secret
      role_claim: groups
      role_mapping:
        streamline-admins: admin
        streamline-staff: member
      email_linking: ` + partnerLinking + `
library:
  movie_path: /x
  movie_naming: m
  import_mode: hardlink
schedules:
  rss_sync: 15m
  metadata_refresh: 24h
  download_monitor: 30s
  missing_search: 12h
  cleanup: 24h
log:
  level: info
  format: text
`
	Expect(config.LoadReader(strings.NewReader(yaml))).To(Succeed())
}

// loadOIDCRoleMapConfig seeds a provider "kc" with claim-based role mapping
// and the given email_linking setting, at the default admin ceiling (none).
func loadOIDCRoleMapConfig(emailLinking string) {
	GinkgoHelper()
	loadOIDCRoleMapConfigDefault(emailLinking, false, "member")
}

// loadOIDCRoleMapConfigDefault is loadOIDCRoleMapConfig with the provider's
// admin ceiling and auth.oidc_default_role spelled out.
func loadOIDCRoleMapConfigDefault(
	emailLinking string,
	allowAdmin bool,
	defaultRole string,
) {
	GinkgoHelper()
	yaml := `
data_dir: ` + os.TempDir() + `
auth:
  mode: disabled
  trusted_role: admin
  session_ttl: 168h
  registration_mode: open
  oidc_default_role: ` + defaultRole + `
  oidc:
    - name: kc
      issuer: https://kc.example.com
      client_id: streamline
      client_secret: secret
      role_claim: groups
      role_mapping:
        streamline-admins: admin
        streamline-staff: member
      email_linking: ` + emailLinking + `
      allow_admin: ` + strconv.FormatBool(allowAdmin) + `
library:
  movie_path: /x
  movie_naming: m
  import_mode: hardlink
schedules:
  rss_sync: 15m
  metadata_refresh: 24h
  download_monitor: 30s
  missing_search: 12h
  cleanup: 24h
log:
  level: info
  format: text
`
	Expect(config.LoadReader(strings.NewReader(yaml))).To(Succeed())
}

// loadOIDCTestConfig populates the config singleton with the minimum required
// fields for LoginOIDC + helpers to run, declaring provider "google" with the
// given email_linking setting.
func loadOIDCTestConfig(regMode, oidcDefaultRole, emailLinking string) {
	GinkgoHelper()
	yaml := `
data_dir: ` + os.TempDir() + `
auth:
  mode: disabled
  trusted_role: admin
  session_ttl: 168h
  registration_mode: ` + regMode + `
  oidc_default_role: ` + oidcDefaultRole + `
  oidc:
    - name: google
      issuer: https://accounts.google.com
      client_id: streamline
      client_secret: secret
      email_linking: ` + emailLinking + `
library:
  movie_path: /x
  movie_naming: m
  import_mode: hardlink
  default_quality:
    preferred_resolution: 1080p
    min_resolution: 720p
    no_match_cooldown: 6h
    max_grab_failures: 3
schedules:
  rss_sync: 15m
  metadata_refresh: 24h
  download_monitor: 30s
  missing_search: 12h
  cleanup: 24h
log:
  level: info
  format: text
`
	err := config.LoadReader(strings.NewReader(yaml))
	Expect(err).ToNot(HaveOccurred())
}
