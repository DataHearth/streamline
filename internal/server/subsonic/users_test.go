package subsonic

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/appaccess"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = g.Describe("user management", g.Label("integration"), func() {
	var (
		f     *fixture
		ctx   context.Context
		admin *ent.User
	)

	g.BeforeEach(func() {
		ctx = context.Background()
		f = newFixture(ctx)
		configtest.Setup(nil)
		accounts, err := auth.New(db.New(f.client))
		Expect(err).NotTo(HaveOccurred())
		f.handler = New(Deps{
			Ent:      f.client,
			Tracker:  appaccess.NewTracker(f.client),
			Accounts: accounts,
		}).Routes()
		admin = f.addUser("admin@example.com", user.RoleAdmin)
	})

	reload := func(email string) *ent.User {
		g.GinkgoHelper()
		return f.client.User.Query().Where(user.EmailEQ(email)).OnlyX(ctx)
	}

	g.It("lets a member read only their own account and an admin read any", func() {
		me := obj(
			okResp(
				f.call("getUser", url.Values{"username": {"LISTENER@example.com"}}),
			),
			"user",
		)
		Expect(me["adminRole"]).To(BeFalse())
		Expect(me["streamRole"]).To(BeTrue())
		Expect(errCode(f.call("getUser", url.Values{"username": {admin.Email}}))).
			To(BeEquivalentTo(50))
		// A missing account reads the same as someone else's.
		Expect(
			errCode(
				f.call("getUser", url.Values{"username": {"ghost@example.com"}}),
			),
		).
			To(BeEquivalentTo(50))
		Expect(errCode(f.call("getUsers", nil))).To(BeEquivalentTo(50))

		other := obj(okResp(f.callAs(admin.Email, "getUser", url.Values{
			"username": {"listener@example.com"},
		})), "user")
		Expect(other["username"]).To(Equal("listener@example.com"))
		all := list(
			obj(okResp(f.callAs(admin.Email, "getUsers", nil)), "users"),
			"user",
		)
		Expect(
			ids2(all, "username"),
		).To(ConsistOf("admin@example.com", "listener@example.com"))
	})

	g.It("refuses every account change, even to an admin", func() {
		for _, call := range []struct {
			endpoint string
			params   url.Values
		}{
			{"createUser", url.Values{
				"username": {"bob"}, "email": {"bob@example.com"}, "password": {"password1"},
				"adminRole": {"true"},
			}},
			{"updateUser", url.Values{"username": {"listener@example.com"}, "adminRole": {"true"}}},
			{"deleteUser", url.Values{"username": {"listener@example.com"}}},
		} {
			resp := f.callAs(admin.Email, call.endpoint, call.params)
			Expect(errCode(resp)).To(BeEquivalentTo(50), call.endpoint)
			Expect(
				resp["error"].(map[string]any)["message"],
			).To(ContainSubstring("web UI"))
		}
		Expect(
			f.client.User.Query().Where(user.EmailEQ("bob@example.com")).ExistX(ctx),
		).
			To(BeFalse())
		Expect(reload("listener@example.com").Role).To(Equal(user.RoleMember))
	})

	g.It("changes only the caller's own Subsonic password", func() {
		okResp(f.call("changePassword", url.Values{
			"username": {"Listener@example.com"},
			"password": {"enc:" + hex.EncodeToString([]byte("fresh-pass"))},
		}))
		u := reload("listener@example.com")
		Expect(u.SubsonicPassword).To(Equal("fresh-pass"))
		Expect(u.PasswordHash).To(BeEmpty(), "the login password is not this API's")
		okResp(
			f.callWith("ping", signedAs("listener@example.com", "fresh-pass", nil)),
		)

		// Not someone else's, admin or not.
		Expect(errCode(f.callAs(admin.Email, "changePassword", url.Values{
			"username": {"listener@example.com"}, "password": {"admin-set-1"},
		}))).To(BeEquivalentTo(50))
		Expect(
			reload("listener@example.com").SubsonicPassword,
		).To(Equal("fresh-pass"))

		self := func(password string) map[string]any {
			g.GinkgoHelper()
			return f.callWith(
				"changePassword",
				signedAs(
					"listener@example.com",
					"fresh-pass",
					url.Values{
						"username": {"listener@example.com"},
						"password": {password},
					},
				),
			)
		}
		Expect(errCode(self("short"))).To(BeEquivalentTo(0))

		Expect(f.client.User.Update().Where(user.EmailEQ("listener@example.com")).
			SetSubsonicCanChangePassword(false).Exec(ctx)).To(Succeed())
		Expect(errCode(self("another-pass"))).To(BeEquivalentTo(50))
	})

	g.It("reports the permissions the web UI set", func() {
		Expect(f.client.User.Update().Where(user.EmailEQ("listener@example.com")).
			SetSubsonicCanShare(false).
			SetSubsonicMaxBitRate(128).
			Exec(ctx)).To(Succeed())
		me := obj(
			okResp(
				f.call("getUser", url.Values{"username": {"listener@example.com"}}),
			),
			"user",
		)
		Expect(me["shareRole"]).To(BeFalse())
		Expect(me["downloadRole"]).To(BeTrue())
		Expect(me["maxBitRate"]).To(BeEquivalentTo(128))
	})

	g.It("enforces the permissions", func() {
		path := filepath.Join(g.GinkgoT().TempDir(), "01.flac")
		Expect(os.WriteFile(path, []byte("audio"), 0o600)).To(Succeed())
		Expect(f.client.MediaFile.Update().SetPath(path).Exec(ctx)).To(Succeed())
		Expect(f.client.User.Update().Where(user.EmailEQ("listener@example.com")).
			SetSubsonicCanStream(false).
			SetSubsonicCanDownload(false).
			SetSubsonicCanManagePlaylists(false).
			SetSubsonicCanShare(false).
			SetSubsonicCanRate(false).
			Exec(ctx)).To(Succeed())

		song := url.Values{"id": {tr(f.withFile)}}
		for _, call := range []struct {
			endpoint string
			params   url.Values
		}{
			{"stream", song},
			{"download", song},
			{"createPlaylist", url.Values{"name": {"x"}}},
			{"createShare", song},
			{"setRating", url.Values{"id": {tr(f.withFile)}, "rating": {"3"}}},
		} {
			Expect(
				errCode(f.call(call.endpoint, call.params)),
			).To(BeEquivalentTo(50), call.endpoint)
		}
		// Reading stays open.
		okResp(f.call("getSong", song))
		okResp(f.call("star", song))
	})

	g.It("caps a stream at the account's bit rate, even for raw", func() {
		path := filepath.Join(g.GinkgoT().TempDir(), "01.flac")
		Expect(os.WriteFile(path, []byte("audio"), 0o600)).To(Succeed())
		Expect(f.client.MediaFile.Update().SetPath(path).Exec(ctx)).To(Succeed())
		trans := &fakeTranscoder{path: "/usr/bin/ffmpeg", out: "MP3"}
		f.handler = New(Deps{
			Ent:        f.client,
			Tracker:    appaccess.NewTracker(f.client),
			Transcoder: trans,
		}).Routes()
		Expect(f.client.User.Update().Where(user.EmailEQ("listener@example.com")).
			SetSubsonicMaxBitRate(96).Exec(ctx)).To(Succeed())

		for _, params := range []url.Values{
			{"format": {"raw"}},
			{"maxBitRate": {"256"}},
			{},
		} {
			params.Set("id", tr(f.withFile))
			rec := httptest.NewRecorder()
			f.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
				"/stream?"+signedQuery("sesame", params).Encode(), nil))
			Expect(rec.Body.String()).To(Equal("MP3"))
		}
		Expect(trans.calls).To(HaveLen(3))
		for _, c := range trans.calls {
			Expect(c.BitrateKbps).To(Equal(96))
		}
	})
})

func ids2(items []map[string]any, key string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it[key].(string))
	}
	return out
}
