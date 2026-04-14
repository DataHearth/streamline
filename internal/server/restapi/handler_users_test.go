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
	"Handler: Users",
	Label("unit", "server", "users"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			app = newAPIKeyApp()
		})

		Describe("GET /api/v1/users", func() {
			It("returns paginated list for admin", func() {
				app.auth.EXPECT().
					ListUsers(mock.Anything, mock.AnythingOfType("auth.UserFilter")).
					Return([]*ent.User{
						{ID: app.adminID, Email: "admin@test.com", Role: "admin"},
					}, 1, nil).
					Once()

				req := app.req(http.MethodGet, "/api/v1/users", app.adminKey, nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var list UserList
				Expect(json.NewDecoder(resp.Body).Decode(&list)).To(Succeed())
				Expect(list.Total).To(BeNumerically(">=", 1))
				Expect(list.Items).NotTo(BeEmpty())
			})

			It("returns 403 for non-admin caller", func() {
				app.addMember("member@test.com")
				req := app.req(http.MethodGet, "/api/v1/users", app.memberKey, nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			})
		})

		Describe("POST /api/v1/users", func() {
			It("creates a user with 201", func() {
				app.auth.EXPECT().
					CreateUserDirect(mock.Anything, "new@test.com", "password123", "member", "").
					Return(&ent.User{
						ID:    10,
						Email: "new@test.com",
						Role:  "member",
					}, nil).
					Once()

				req := app.req(
					http.MethodPost,
					"/api/v1/users",
					app.adminKey,
					strings.NewReader(
						`{"email":"new@test.com","password":"password123","role":"member"}`,
					),
				)
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var u User
				Expect(json.NewDecoder(resp.Body).Decode(&u)).To(Succeed())
				Expect(string(u.Email)).To(Equal("new@test.com"))
			})

			It("returns 409 email_exists on duplicate", func() {
				app.auth.EXPECT().
					CreateUserDirect(mock.Anything, "dup@test.com", "password123", "member", "").
					Return(&ent.User{
						ID:    11,
						Email: "dup@test.com",
						Role:  "member",
					}, nil).
					Once()
				app.auth.EXPECT().
					CreateUserDirect(mock.Anything, "dup@test.com", "password123", "member", "").
					Return(nil, auth.ErrUserEmailExists).
					Once()

				body := `{"email":"dup@test.com","password":"password123","role":"member"}`
				req := app.req(http.MethodPost, "/api/v1/users", app.adminKey,
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				req = app.req(http.MethodPost, "/api/v1/users", app.adminKey,
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp = app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
				var e Error
				Expect(json.NewDecoder(resp.Body).Decode(&e)).To(Succeed())
				Expect(e.Code).NotTo(BeNil())
				Expect(*e.Code).To(Equal("email_exists"))
			})

			It("returns 422 weak_password", func() {
				app.auth.EXPECT().
					CreateUserDirect(mock.Anything, "weak@test.com", "short", "member", "").
					Return(nil, auth.ErrPasswordWeak).
					Once()

				req := app.req(
					http.MethodPost,
					"/api/v1/users",
					app.adminKey,
					strings.NewReader(
						`{"email":"weak@test.com","password":"short","role":"member"}`,
					),
				)
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			})

			It("returns 403 for non-admin", func() {
				app.addMember("m2@test.com")
				req := app.req(
					http.MethodPost,
					"/api/v1/users",
					app.memberKey,
					strings.NewReader(
						`{"email":"x@test.com","password":"password123","role":"member"}`,
					),
				)
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			})
		})

		Describe("GET /api/v1/users/{uid}", func() {
			It("returns user detail with api keys and sessions", func() {
				app.auth.EXPECT().
					GetUserDetail(mock.Anything, app.adminID).
					Return(
						&ent.User{
							ID:    app.adminID,
							Email: "admin@test.com",
							Role:  "admin",
						},
						[]*ent.ApiKey{
							{ID: 1, Name: "test-admin", CreateTime: time.Now()},
						},
						nil,
						nil,
					).
					Once()

				req := app.req(http.MethodGet,
					fmt.Sprintf("/api/v1/users/%d", app.adminID),
					app.adminKey, nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var detail UserDetail
				Expect(json.NewDecoder(resp.Body).Decode(&detail)).To(Succeed())
				Expect(detail.User.Id).To(Equal(app.adminID))
				Expect(detail.ApiKeys).NotTo(BeEmpty())
			})

			It("returns 404 on unknown id", func() {
				app.auth.EXPECT().
					GetUserDetail(mock.Anything, uint32(99999)).
					Return(nil, nil, nil, auth.ErrUserNotFound).
					Once()

				req := app.req(
					http.MethodGet,
					"/api/v1/users/99999",
					app.adminKey,
					nil,
				)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})

		Describe("PATCH /api/v1/users/{uid}", func() {
			It("patches display name", func() {
				app.addMember("patch@test.com")

				app.auth.EXPECT().
					UpdateUser(mock.Anything, app.memberID, mock.AnythingOfType("auth.UserPatch")).
					Return(nil).
					Once()
				app.auth.EXPECT().
					GetUserByID(mock.Anything, app.memberID).
					Return(&ent.User{
						ID:          app.memberID,
						Email:       "patch@test.com",
						Role:        "member",
						DisplayName: "Patched",
					}, nil).
					Once()

				req := app.req(http.MethodPatch,
					fmt.Sprintf("/api/v1/users/%d", app.memberID),
					app.adminKey,
					strings.NewReader(`{"display_name":"Patched"}`))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var u User
				Expect(json.NewDecoder(resp.Body).Decode(&u)).To(Succeed())
				Expect(u.DisplayName).NotTo(BeNil())
				Expect(*u.DisplayName).To(Equal("Patched"))
			})

			It("rejects demoting the only admin with 409 last_admin", func() {
				app.auth.EXPECT().
					UpdateUser(mock.Anything, app.adminID, mock.AnythingOfType("auth.UserPatch")).
					Return(auth.ErrLastAdmin).
					Once()

				req := app.req(http.MethodPatch,
					fmt.Sprintf("/api/v1/users/%d", app.adminID),
					app.adminKey,
					strings.NewReader(`{"role":"member"}`))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
				var e Error
				Expect(json.NewDecoder(resp.Body).Decode(&e)).To(Succeed())
				Expect(e.Code).NotTo(BeNil())
				Expect(*e.Code).To(Equal("last_admin"))
			})
		})

		Describe("DELETE /api/v1/users/{uid}", func() {
			It("rejects self-delete with 409 self_delete_forbidden", func() {
				app.auth.EXPECT().
					DeleteUser(mock.Anything, app.adminID, app.adminID).
					Return(auth.ErrSelfDeleteForbidden).
					Once()

				req := app.req(http.MethodDelete,
					fmt.Sprintf("/api/v1/users/%d", app.adminID),
					app.adminKey, nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
				var e Error
				Expect(json.NewDecoder(resp.Body).Decode(&e)).To(Succeed())
				Expect(*e.Code).To(Equal("self_delete_forbidden"))
			})

			It("deletes a member user and returns 204", func() {
				app.addMember("bye@test.com")
				app.auth.EXPECT().
					DeleteUser(mock.Anything, app.memberID, app.adminID).
					Return(nil).
					Once()

				req := app.req(http.MethodDelete,
					fmt.Sprintf("/api/v1/users/%d", app.memberID),
					app.adminKey, nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			})
		})

		Describe("POST /api/v1/users/{uid}/password-reset", func() {
			It("rotates password and returns 204", func() {
				app.addMember("reset@test.com")
				app.auth.EXPECT().
					AdminResetPassword(mock.Anything, app.memberID, "newpassword123").
					Return(nil).
					Once()

				req := app.req(http.MethodPost,
					fmt.Sprintf("/api/v1/users/%d/password-reset", app.memberID),
					app.adminKey,
					strings.NewReader(`{"new_password":"newpassword123"}`))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			})

			It("rejects weak password with 422", func() {
				app.addMember("weakreset@test.com")
				app.auth.EXPECT().
					AdminResetPassword(mock.Anything, app.memberID, "short").
					Return(auth.ErrPasswordWeak).
					Once()

				req := app.req(http.MethodPost,
					fmt.Sprintf("/api/v1/users/%d/password-reset", app.memberID),
					app.adminKey,
					strings.NewReader(`{"new_password":"short"}`))
				req.Header.Set("Content-Type", "application/json")
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			})
		})

		Describe("DELETE /api/v1/users/{uid}/api-keys/{kid}", func() {
			It("returns 404 for unknown key", func() {
				app.addMember("rk@test.com")
				app.auth.EXPECT().
					AdminRevokeAPIKey(mock.Anything, app.memberID, uint32(99999)).
					Return(auth.ErrAPIKeyNotFound).
					Once()

				req := app.req(http.MethodDelete,
					fmt.Sprintf("/api/v1/users/%d/api-keys/99999", app.memberID),
					app.adminKey, nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})

		Describe("DELETE /api/v1/users/{uid}/sessions/{sid}", func() {
			It("returns 404 for unknown session", func() {
				app.addMember("rs@test.com")
				app.auth.EXPECT().
					AdminRevokeSession(mock.Anything, app.memberID, uint32(99999)).
					Return(auth.ErrSessionNotFound).
					Once()

				req := app.req(http.MethodDelete,
					fmt.Sprintf("/api/v1/users/%d/sessions/99999", app.memberID),
					app.adminKey, nil)
				resp := app.do(req)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})
	},
)
