package opds

import (
	"net/http"
	"net/http/httptest"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/auth"
)

var _ = g.Describe("auth", g.Label("unit"), func() {
	g.It("accepts matching Basic credentials", func() {
		Expect(checkToken(auth.HashOPDSToken("sesame"), "sesame")).To(BeTrue())
	})
	g.It("rejects a wrong or empty stored token", func() {
		Expect(checkToken(auth.HashOPDSToken("sesame"), "nope")).To(BeFalse())
		Expect(checkToken("sesame", "sesame")).To(BeFalse())
		Expect(checkToken("", "")).To(BeFalse())
	})
})

var _ = g.Describe("auth access tracking", g.Label("integration"), func() {
	var f *catalogFixture

	g.BeforeEach(func(ctx g.SpecContext) {
		f = newCatalogFixture(ctx)
	})

	opdsUser := func(ctx g.SpecContext) *ent.User {
		return f.client.User.Query().Where(user.EmailEQ(testEmail)).OnlyX(ctx)
	}

	serve := func(password string) {
		req := httptest.NewRequest(http.MethodGet, "/opds", nil)
		req.SetBasicAuth(testEmail, password)
		req.Header.Set("User-Agent", "KOReader/2024.11 (Linux)")
		f.router.ServeHTTP(httptest.NewRecorder(), req)
	}

	g.It(
		"stamps the first product token of the User-Agent on success",
		func(ctx g.SpecContext) {
			serve("sesame")
			Eventually(func() string {
				return opdsUser(ctx).OpdsLastClient
			}).Should(Equal("KOReader"))
			Expect(opdsUser(ctx).OpdsLastUsedAt).NotTo(BeNil())
		},
	)

	g.It("stamps nothing when the password is wrong", func(ctx g.SpecContext) {
		serve("nope")
		Consistently(func() string {
			return opdsUser(ctx).OpdsLastClient
		}, "300ms", "50ms").Should(BeEmpty())
		Expect(opdsUser(ctx).OpdsLastUsedAt).To(BeNil())
	})
})
