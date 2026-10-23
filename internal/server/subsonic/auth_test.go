package subsonic

import (
	"crypto/md5" //nolint:gosec // the Subsonic token scheme is defined as md5(password+salt)
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
)

var _ = g.Describe("auth", g.Label("unit"), func() {
	g.It("accepts a valid token: t = md5(password + salt)", func() {
		sum := md5.Sum([]byte("sesame" + "abc123"))
		token := hex.EncodeToString(sum[:])
		Expect(
			checkCredentials("sesame", url.Values{"t": {token}, "s": {"abc123"}}),
		).To(BeTrue())
	})

	g.It("accepts legacy p= plain and enc: hex forms", func() {
		Expect(checkCredentials("sesame", url.Values{"p": {"sesame"}})).To(BeTrue())
		Expect(
			checkCredentials(
				"sesame",
				url.Values{"p": {"enc:" + hex.EncodeToString([]byte("sesame"))}},
			),
		).To(BeTrue())
	})

	g.It("rejects wrong token and empty stored password", func() {
		Expect(
			checkCredentials("sesame", url.Values{"t": {"beef"}, "s": {"x"}}),
		).To(BeFalse())
		Expect(checkCredentials("", url.Values{"p": {""}})).To(BeFalse())
	})
})

var _ = g.Describe("auth access tracking", g.Label("integration"), func() {
	var f *fixture

	g.BeforeEach(func(ctx g.SpecContext) {
		f = newFixture(ctx)
	})

	listener := func(ctx g.SpecContext) *ent.User {
		return f.client.User.Query().
			Where(user.EmailEQ("listener@example.com")).OnlyX(ctx)
	}

	ping := func(password string) {
		q := signedQuery(password, url.Values{"c": {"Symfonium"}})
		f.handler.ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "/ping?"+q.Encode(), nil),
		)
	}

	g.It("stamps the client name on success", func(ctx g.SpecContext) {
		ping("sesame")
		Eventually(func() string {
			return listener(ctx).SubsonicLastClient
		}).Should(Equal("Symfonium"))
		Expect(listener(ctx).SubsonicLastUsedAt).NotTo(BeNil())
	})

	g.It("stamps nothing when the credentials are wrong", func(ctx g.SpecContext) {
		ping("wrong")
		Consistently(func() string {
			return listener(ctx).SubsonicLastClient
		}, "300ms", "50ms").Should(BeEmpty())
		Expect(listener(ctx).SubsonicLastUsedAt).To(BeNil())
	})
})
