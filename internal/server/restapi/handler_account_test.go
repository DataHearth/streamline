package restapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/auth"
)

var _ = Describe(
	"Handler: Account",
	Label("unit", "server", "account"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			app = newAPIKeyApp()
		})

		// --- PATCH /api/v1/auth/me ---------------------------------------------
		Describe("PATCH /api/v1/auth/me", func() {
			It("persists display_name and returns the updated user", func() {
				dn := "Alice"
				updated := &ent.User{
					ID:          app.adminID,
					Email:       "admin@test.com",
					DisplayName: dn,
					Role:        "admin",
					AuthMethod:  "local",
				}
				app.auth.EXPECT().
					UpdateProfile(mock.Anything, app.adminID, dn).
					Return(updated, nil).
					Once()

				req := app.req(
					http.MethodPatch,
					"/api/v1/auth/me",
					app.adminKey,
					strings.NewReader(`{"display_name":"Alice"}`),
				)
				req.Header.Set("Content-Type", "application/json")

				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var u User
				Expect(json.NewDecoder(resp.Body).Decode(&u)).To(Succeed())
				Expect(u.DisplayName).NotTo(BeNil())
				Expect(*u.DisplayName).To(Equal("Alice"))
			})

			It("returns 401 without auth", func() {
				req := app.req(
					http.MethodPatch,
					"/api/v1/auth/me",
					"invalid-token",
					strings.NewReader(`{}`),
				)
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			})
		})

		// --- POST /api/v1/auth/password ----------------------------------------
		Describe("POST /api/v1/auth/password", func() {
			It("returns 401 when current password is wrong", func() {
				app.auth.EXPECT().
					ChangePassword(
						mock.Anything, app.adminID, "wrongone", "newpassw0rd", "admin-jti",
					).
					Return(auth.ErrPasswordInvalid).
					Once()

				resp := postJSON(
					app, app.adminKey,
					"/api/v1/auth/password",
					`{"current_password":"wrongone","new_password":"newpassw0rd"}`,
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			})

			It("returns 422 on weak new password", func() {
				app.auth.EXPECT().
					ChangePassword(
						mock.Anything, app.adminID, "password123", "short", "admin-jti",
					).
					Return(auth.ErrPasswordWeak).
					Once()

				resp := postJSON(
					app, app.adminKey,
					"/api/v1/auth/password",
					`{"current_password":"password123","new_password":"short"}`,
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			})

			It("204 on success; old password rejected on subsequent change", func() {
				app.auth.EXPECT().
					ChangePassword(
						mock.Anything, app.adminID, "password123", "newPassw0rd!", "admin-jti",
					).
					Return(nil).
					Once()
				app.auth.EXPECT().
					ChangePassword(
						mock.Anything, app.adminID, "password123", "anotherPass9!", "admin-jti",
					).
					Return(auth.ErrPasswordInvalid).
					Once()

				resp := postJSON(
					app,
					app.adminKey,
					"/api/v1/auth/password",
					`{"current_password":"password123","new_password":"newPassw0rd!"}`,
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

				resp2 := postJSON(
					app,
					app.adminKey,
					"/api/v1/auth/password",
					`{"current_password":"password123","new_password":"anotherPass9!"}`,
				)
				defer resp2.Body.Close()
				Expect(resp2.StatusCode).To(Equal(http.StatusUnauthorized))
			})
		})

		// --- /api/v1/auth/me/api-keys ------------------------------------------
		Describe("/api/v1/auth/me/api-keys", func() {
			It("GET returns the caller's own key(s)", func() {
				app.auth.EXPECT().
					ListAPIKeys(mock.Anything, app.adminID).
					Return([]*ent.ApiKey{
						{ID: 1, Name: "test-admin", CreateTime: time.Now()},
					}, nil).
					Once()

				resp := authGET(app, app.adminKey, "/api/v1/auth/me/api-keys")
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var keys []ApiKey
				Expect(json.NewDecoder(resp.Body).Decode(&keys)).To(Succeed())
				Expect(keys).To(HaveLen(1))
			})

			It(
				"POST returns raw_token once; subsequent GET lists without raw",
				func() {
					createdAt := time.Now()
					app.auth.EXPECT().
						CreateAPIKey(mock.Anything, app.adminID, "cli").
						Return("raw-cli-token", &ent.ApiKey{
							ID:         2,
							Name:       "cli",
							CreateTime: createdAt,
						}, nil).
						Once()
					app.auth.EXPECT().
						ListAPIKeys(mock.Anything, app.adminID).
						Return([]*ent.ApiKey{
							{ID: 1, Name: "test-admin", CreateTime: createdAt},
							{ID: 2, Name: "cli", CreateTime: createdAt},
						}, nil).
						Once()

					resp := postJSON(
						app, app.adminKey,
						"/api/v1/auth/me/api-keys",
						`{"name":"cli"}`,
					)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusCreated))
					var created ApiKeyCreated
					Expect(json.NewDecoder(resp.Body).Decode(&created)).To(Succeed())
					Expect(created.RawToken).To(Equal("raw-cli-token"))
					Expect(created.Name).To(Equal("cli"))

					listResp := authGET(
						app, app.adminKey, "/api/v1/auth/me/api-keys",
					)
					defer listResp.Body.Close()
					var keys []ApiKey
					Expect(
						json.NewDecoder(listResp.Body).Decode(&keys),
					).To(Succeed())
					Expect(keys).To(HaveLen(2))
				},
			)

			It("POST authenticated via API key returns 401", func() {
				resp := postJSON(
					app, app.adminAPIKey,
					"/api/v1/auth/me/api-keys",
					`{"name":"escalation"}`,
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			})

			It("DELETE for non-existent id returns 404", func() {
				app.auth.EXPECT().
					RevokeAPIKeyByID(mock.Anything, app.adminID, uint32(999999)).
					Return(auth.ErrAPIKeyNotFound).
					Once()

				req := app.req(
					http.MethodDelete,
					"/api/v1/auth/me/api-keys/999999",
					app.adminKey,
					nil,
				)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})

		// --- /api/v1/auth/me/sessions ------------------------------------------
		Describe("/api/v1/auth/me/sessions", func() {
			It(
				"GET lists sessions (empty initially when auth is via API key)",
				func() {
					app.auth.EXPECT().
						ListUserSessions(mock.Anything, app.adminID).
						Return(nil, nil).
						Once()

					resp := authGET(app, app.adminKey, "/api/v1/auth/me/sessions")
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusOK))
					var sessions []Session
					Expect(
						json.NewDecoder(resp.Body).Decode(&sessions),
					).To(Succeed())
					Expect(sessions).To(BeEmpty())
				},
			)

			It("DELETE for non-existent id returns 404", func() {
				app.auth.EXPECT().
					RevokeSessionByID(mock.Anything, app.adminID, uint32(999999)).
					Return(auth.ErrSessionNotFound).
					Once()

				req := app.req(
					http.MethodDelete,
					"/api/v1/auth/me/sessions/999999",
					app.adminKey,
					nil,
				)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})

			It("DELETE a peer session revokes it", func() {
				const peerID uint32 = 42
				app.auth.EXPECT().
					ListUserSessions(mock.Anything, app.adminID).
					Return([]*ent.Session{
						{ID: peerID, Jti: "peer-jti", CreateTime: time.Now()},
					}, nil).
					Once()
				app.auth.EXPECT().
					RevokeSessionByID(mock.Anything, app.adminID, peerID).
					Return(nil).
					Once()

				listResp := authGET(app, app.adminKey, "/api/v1/auth/me/sessions")
				defer listResp.Body.Close()
				var sessions []Session
				Expect(
					json.NewDecoder(listResp.Body).Decode(&sessions),
				).To(Succeed())
				Expect(sessions).To(HaveLen(1))
				dropID := sessions[0].Id

				req := app.req(
					http.MethodDelete,
					fmt.Sprintf("/api/v1/auth/me/sessions/%d", dropID),
					app.adminKey,
					nil,
				)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			})
		})

		Describe("/api/v1/account/subsonic-password", func() {
			const path = "/api/v1/account/subsonic-password"
			const secret = "abcdefghijklmnopqrstuvwx"

			type state struct {
				Enabled  bool   `json:"enabled"`
				Password string `json:"password"`
			}
			decode := func(resp *http.Response) state {
				GinkgoHelper()
				defer resp.Body.Close()
				var st state
				Expect(json.NewDecoder(resp.Body).Decode(&st)).To(Succeed())
				return st
			}

			It("POST returns the rotated password", func() {
				app.auth.EXPECT().
					RotateSubsonicPassword(mock.Anything, app.adminID).
					Return(secret, nil).
					Once()
				resp := app.do(app.req(http.MethodPost, path, app.adminKey, nil))
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(
					decode(resp),
				).To(Equal(state{Enabled: true, Password: secret}))
			})

			It("GET reports an enabled password", func() {
				app.auth.EXPECT().
					GetUserByID(mock.Anything, app.adminID).
					Return(&ent.User{ID: app.adminID, SubsonicPassword: secret}, nil).
					Once()
				resp := authGET(app, app.adminKey, path)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(
					decode(resp),
				).To(Equal(state{Enabled: true, Password: secret}))
			})

			It("GET reports disabled when no password is set", func() {
				app.auth.EXPECT().
					GetUserByID(mock.Anything, app.adminID).
					Return(&ent.User{ID: app.adminID}, nil).
					Once()
				resp := authGET(app, app.adminKey, path)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(decode(resp).Enabled).To(BeFalse())
			})

			It("DELETE disables the password", func() {
				app.auth.EXPECT().
					DisableSubsonicPassword(mock.Anything, app.adminID).
					Return(nil).
					Once()
				resp := app.do(app.req(http.MethodDelete, path, app.adminKey, nil))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			})

			DescribeTable("refuses callers without a session",
				func(method, key string) {
					resp := app.do(app.req(method, path, key, nil))
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
				},
				Entry("GET without auth", http.MethodGet, "invalid-token"),
				Entry("POST without auth", http.MethodPost, "invalid-token"),
				Entry("DELETE without auth", http.MethodDelete, "invalid-token"),
			)

			It("refuses API-key identities on every verb", func() {
				for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
					resp := app.do(app.req(m, path, app.adminAPIKey, nil))
					resp.Body.Close()
					Expect(resp.StatusCode).To(
						BeElementOf(
							http.StatusUnauthorized,
							http.StatusForbidden,
						), m)
				}
			})
		})

		Describe("/api/v1/account/opds-token", func() {
			const path = "/api/v1/account/opds-token"
			const secret = "abcdefghijklmnopqrstuvwx"

			type state struct {
				Enabled bool   `json:"enabled"`
				Token   string `json:"token"`
			}
			decode := func(resp *http.Response) state {
				GinkgoHelper()
				defer resp.Body.Close()
				var st state
				Expect(json.NewDecoder(resp.Body).Decode(&st)).To(Succeed())
				return st
			}

			It("POST returns the rotated token", func() {
				app.auth.EXPECT().
					RotateOPDSToken(mock.Anything, app.adminID).
					Return(secret, nil).
					Once()
				resp := app.do(app.req(http.MethodPost, path, app.adminKey, nil))
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(
					decode(resp),
				).To(Equal(state{Enabled: true, Token: secret}))
			})

			It("GET reports an enabled token", func() {
				app.auth.EXPECT().
					GetUserByID(mock.Anything, app.adminID).
					Return(&ent.User{ID: app.adminID, OpdsToken: secret}, nil).
					Once()
				resp := authGET(app, app.adminKey, path)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(
					decode(resp),
				).To(Equal(state{Enabled: true, Token: secret}))
			})

			It("GET reports disabled when no token is set", func() {
				app.auth.EXPECT().
					GetUserByID(mock.Anything, app.adminID).
					Return(&ent.User{ID: app.adminID}, nil).
					Once()
				resp := authGET(app, app.adminKey, path)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(decode(resp).Enabled).To(BeFalse())
			})

			It("DELETE disables the token", func() {
				app.auth.EXPECT().
					DisableOPDSToken(mock.Anything, app.adminID).
					Return(nil).
					Once()
				resp := app.do(app.req(http.MethodDelete, path, app.adminKey, nil))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			})

			DescribeTable("refuses callers without a session",
				func(method, key string) {
					resp := app.do(app.req(method, path, key, nil))
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
				},
				Entry("GET without auth", http.MethodGet, "invalid-token"),
				Entry("POST without auth", http.MethodPost, "invalid-token"),
				Entry("DELETE without auth", http.MethodDelete, "invalid-token"),
			)

			It("refuses API-key identities on every verb", func() {
				for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
					resp := app.do(app.req(m, path, app.adminAPIKey, nil))
					resp.Body.Close()
					Expect(resp.StatusCode).To(
						BeElementOf(
							http.StatusUnauthorized,
							http.StatusForbidden,
						), m)
				}
			})
		})
	},
)

// --- helpers -----------------------------------------------------------------

func postJSON(app *apiKeyApp, apiKey, path, body string) *http.Response {
	GinkgoHelper()
	req := app.req(http.MethodPost, path, apiKey, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return app.do(req)
}

func authGET(app *apiKeyApp, apiKey, path string) *http.Response {
	GinkgoHelper()
	return app.do(app.req(http.MethodGet, path, apiKey, nil))
}
