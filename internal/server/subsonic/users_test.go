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
	"golang.org/x/crypto/bcrypt"

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

	g.It("lets a member read only their own account", func() {
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

	g.It(
		"creates an account with the API's defaults and a Subsonic-only password",
		func() {
			okResp(f.callAs(admin.Email, "createUser", url.Values{
				"username":   {"Bob"},
				"email":      {"Bob@Example.com"},
				"password":   {"enc:" + hex.EncodeToString([]byte("bob-secret"))},
				"maxBitRate": {"128"},
			}))
			bob := reload("bob@example.com")
			Expect(bob.Role).To(Equal(user.RoleMember))
			Expect(bob.DisplayName).To(Equal("Bob"))
			Expect(bob.SubsonicPassword).To(Equal("bob-secret"))
			Expect(bob.SubsonicCreatedAt).NotTo(BeNil())
			// The login password is not the Subsonic one.
			Expect(
				bcrypt.CompareHashAndPassword(
					[]byte(bob.PasswordHash),
					[]byte("bob-secret"),
				),
			).
				NotTo(Succeed())
			Expect(bob.SubsonicCanStream).To(BeTrue())
			Expect(bob.SubsonicCanChangePassword).To(BeTrue())
			Expect(bob.SubsonicCanDownload).To(BeFalse())
			Expect(bob.SubsonicCanShare).To(BeFalse())
			Expect(bob.SubsonicMaxBitRate).To(BeEquivalentTo(128))

			// The new account signs in with what was sent.
			resp := f.callWith(
				"ping",
				signedAs("bob@example.com", "bob-secret", nil),
			)
			Expect(resp["status"]).To(Equal("ok"))

			Expect(errCode(f.callAs(admin.Email, "createUser", url.Values{
				"username": {
					"bob",
				},
				"email":    {"bob@example.com"},
				"password": {"another1"},
			}))).To(BeEquivalentTo(0))
			Expect(errCode(f.callAs(admin.Email, "createUser", url.Values{
				"username": {
					"weak",
				},
				"email":    {"weak@example.com"},
				"password": {"short"},
			}))).To(BeEquivalentTo(0))
			Expect(
				f.client.User.Query().
					Where(user.EmailEQ("weak@example.com")).
					ExistX(ctx),
			).
				To(BeFalse())
			Expect(errCode(f.call("createUser", url.Values{
				"username": {
					"x",
				},
				"email":    {"x@example.com"},
				"password": {"password1"},
			}))).To(BeEquivalentTo(50))
		},
	)

	g.It("grants admin and updates permissions, email and password", func() {
		okResp(f.callAs(admin.Email, "updateUser", url.Values{
			"username":   {"listener@example.com"},
			"adminRole":  {"true"},
			"shareRole":  {"false"},
			"maxBitRate": {"8"},
			"password":   {"new-secret"},
			"email":      {"renamed@example.com"},
		}))
		u := reload("renamed@example.com")
		Expect(u.Role).To(Equal(user.RoleAdmin))
		Expect(u.SubsonicCanShare).To(BeFalse())
		Expect(u.SubsonicCanStream).To(BeTrue(), "an unsent flag keeps its value")
		Expect(u.SubsonicMaxBitRate).To(BeEquivalentTo(32))
		Expect(u.SubsonicPassword).To(Equal("new-secret"))

		okResp(f.callAs(admin.Email, "updateUser", url.Values{
			"username": {"renamed@example.com"}, "adminRole": {"false"},
		}))
		Expect(reload("renamed@example.com").Role).To(Equal(user.RoleMember))
		Expect(errCode(f.callAs(admin.Email, "updateUser", url.Values{
			"username": {"renamed@example.com"}, "maxBitRate": {"999"},
		}))).To(BeEquivalentTo(0))
	})

	g.It(
		"refuses to demote or delete the last admin, and to delete oneself",
		func() {
			Expect(errCode(f.callAs(admin.Email, "updateUser", url.Values{
				"username": {admin.Email}, "adminRole": {"false"},
			}))).To(BeEquivalentTo(0))
			Expect(reload(admin.Email).Role).To(Equal(user.RoleAdmin))
			Expect(errCode(f.callAs(admin.Email, "deleteUser", url.Values{
				"username": {admin.Email},
			}))).To(BeEquivalentTo(0))

			okResp(
				f.callAs(
					admin.Email,
					"deleteUser",
					url.Values{"username": {"listener@example.com"}},
				),
			)
			Expect(
				f.client.User.Query().
					Where(user.EmailEQ("listener@example.com")).
					ExistX(ctx),
			).
				To(BeFalse())
			Expect(
				errCode(
					f.callAs(
						admin.Email,
						"deleteUser",
						url.Values{"username": {"ghost@example.com"}},
					),
				),
			).
				To(BeEquivalentTo(70))
		},
	)

	g.It("changes passwords: one's own when allowed, anyone's for an admin", func() {
		okResp(f.call("changePassword", url.Values{
			"username": {"listener@example.com"}, "password": {"fresh-pass"},
		}))
		Expect(
			reload("listener@example.com").SubsonicPassword,
		).To(Equal("fresh-pass"))

		resp := f.callWith(
			"changePassword",
			signedAs("listener@example.com", "fresh-pass", url.Values{
				"username": {admin.Email}, "password": {"hijacked"},
			}),
		)
		Expect(errCode(resp)).To(BeEquivalentTo(50))

		Expect(f.client.User.Update().Where(user.EmailEQ("listener@example.com")).
			SetSubsonicCanChangePassword(false).Exec(ctx)).To(Succeed())
		resp = f.callWith(
			"changePassword",
			signedAs("listener@example.com", "fresh-pass", url.Values{
				"username": {"listener@example.com"}, "password": {"another-pass"},
			}),
		)
		Expect(errCode(resp)).To(BeEquivalentTo(50))

		okResp(f.callAs(admin.Email, "changePassword", url.Values{
			"username": {"listener@example.com"}, "password": {"admin-set-1"},
		}))
		Expect(
			reload("listener@example.com").SubsonicPassword,
		).To(Equal("admin-set-1"))
	})

	g.It("enforces the permissions it sets", func() {
		path := filepath.Join(g.GinkgoT().TempDir(), "01.flac")
		Expect(os.WriteFile(path, []byte("audio"), 0o600)).To(Succeed())
		Expect(f.client.MediaFile.Update().SetPath(path).Exec(ctx)).To(Succeed())

		okResp(f.callAs(admin.Email, "updateUser", url.Values{
			"username":     {"listener@example.com"},
			"streamRole":   {"false"},
			"downloadRole": {"false"},
			"playlistRole": {"false"},
			"shareRole":    {"false"},
			"commentRole":  {"false"},
		}))
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
