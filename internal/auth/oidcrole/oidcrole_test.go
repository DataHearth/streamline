package oidcrole

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// seedProvider re-seeds the config singleton with one auth.oidc entry named
// "kc" carrying the given adoption tier and admin ceiling.
func seedProvider(emailLinking string, allowAdmin bool) {
	GinkgoHelper()
	configtest.Setup(map[string]any{
		"auth": map[string]any{
			"oidc": []any{map[string]any{
				"name":          "kc",
				"issuer":        "https://kc.example.com",
				"client_id":     "cid",
				"client_secret": "sec",
				"email_linking": emailLinking,
				"allow_admin":   allowAdmin,
			}},
		},
	})
}

// seedAdopting pins email_linking to the loosest tier: the ceiling must not
// read it at all, so a provider that adopts anything and still carries no
// allow_admin has to come back capped.
func seedAdopting(allowAdmin bool) {
	GinkgoHelper()
	seedProvider(config.OIDCEmailLinkingAll, allowAdmin)
}

// There is deliberately no spec here for "a Role built without Cap": the field
// is unexported and this package hands out no setter, so every such spelling —
// a composite literal, an assignment, a params type invented later — is a
// compile error rather than a test failure. What these cover is the ceiling Cap
// applies and the inertness of the one Role another package *can* name, its
// zero value.
var _ = Describe("Cap", Label("unit", "auth"), func() {
	DescribeTable("picks the highest candidate the provider may confer",
		func(allowAdmin bool, fallback string, candidates []string, want string) {
			seedAdopting(allowAdmin)
			Expect(Cap("kc", fallback, candidates...).String()).To(Equal(want))
		},
		Entry("allow_admin takes the admin candidate",
			true, "", []string{"member", "admin"}, "admin"),
		Entry("no allow_admin falls to the next candidate down",
			false, "", []string{"member", "admin"}, "member"),
		Entry("no allow_admin leaves an admin-only claim set undecided",
			false, "", []string{"admin"}, ""),
		Entry("an unmatched claim set falls back",
			false, "request_only", nil, "request_only"),
		Entry("a candidate outranks the fallback",
			false, "request_only", []string{"member"}, "member"),
		Entry("a lower candidate still beats the fallback",
			false, "member", []string{"request_only"}, "request_only"),
		Entry("an admin fallback is clamped to member",
			false, "admin", nil, "member"),
		Entry("an admin fallback survives with allow_admin",
			true, "admin", nil, "admin"),
		Entry("an admin fallback is clamped even when a claim set matched nothing",
			false, "admin", []string{"admin"}, "member"),
		Entry("no candidate and no fallback decides nothing",
			false, "", nil, ""),
		Entry("an unrecognised fallback decides nothing",
			false, "superuser", nil, ""),
		Entry("an unrecognised candidate is ignored",
			false, "member", []string{"superuser"}, "member"),
	)

	It("reports the zero value as empty", func() {
		seedAdopting(false)
		Expect(Role{}.Empty()).To(BeTrue())
		Expect(Cap("kc", "member").Empty()).To(BeFalse())
	})

	// The zero value is what a caller outside this package can still name, so
	// it has to be the harmless one: empty, and an empty ent role that every
	// write path skips rather than a role it applies.
	It("leaves an undecidable role inert rather than privileged", func() {
		seedAdopting(true)
		r := Cap("kc", "superuser")
		Expect(r).To(Equal(Role{}))
		Expect(r.Empty()).To(BeTrue())
		Expect(r.EntRole()).To(BeEmpty())
	})

	It("converts to the ent role the store writes", func() {
		seedAdopting(true)
		Expect(Cap("kc", "", "admin").EntRole()).To(Equal(entuser.RoleAdmin))
		Expect(*Cap("kc", "", "admin").EntRolePtr()).To(Equal(entuser.RoleAdmin))
	})

	// Monotonicity: email_linking moves the adoption axis only. A provider
	// whose tier the operator tightens or loosens confers exactly the same
	// roles either way, which is what kills the escalation-by-tightening.
	DescribeTable("ignores email_linking entirely",
		func(mode string) {
			seedProvider(mode, false)
			Expect(Cap("kc", "admin", "admin").String()).To(Equal("member"))
			seedProvider(mode, true)
			Expect(Cap("kc", "admin", "admin").String()).To(Equal("admin"))
		},
		Entry("unset", ""),
		Entry("disabled", config.OIDCEmailLinkingDisabled),
		Entry("non_admin", config.OIDCEmailLinkingNonAdmin),
		Entry("all", config.OIDCEmailLinkingAll),
	)

	// Naming a provider the operator never configured must not borrow the
	// ceiling of one they did — that is the whole reason Cap takes the name and
	// resolves it here rather than accepting a config value from the caller.
	It("refuses admin for a provider that is not configured at all", func() {
		seedAdopting(true)
		Expect(Cap("ghost", "admin", "admin").String()).To(Equal("member"))
	})

	It("refuses admin when no config has been loaded", func() {
		config.ResetForTest()
		Expect(Cap("kc", "admin", "admin").String()).To(Equal("member"))
	})
})

var _ = Describe("AtLeast", Label("unit", "auth"), func() {
	DescribeTable("compares a role against a minimum",
		func(role, min string, want bool) {
			Expect(AtLeast(role, min)).To(Equal(want))
		},
		Entry("admin clears member", "admin", "member", true),
		Entry("member clears member", "member", "member", true),
		Entry("request_only does not clear member", "request_only", "member", false),
		Entry("an unrecognised role clears nothing", "superuser", "member", false),
		Entry("an unrecognised minimum is satisfied by nobody",
			"admin", "superuser", false),
	)
})
