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
)

var _ = Describe("Handler: Auth API", Label("unit", "server", "auth"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		app = newAPIKeyApp()
	})

	Describe("AuthMe", func() {
		It("returns current user when authenticated with API key", func() {
			app.auth.EXPECT().
				GetUserByID(mock.Anything, app.adminID).
				Return(&ent.User{
					ID:         app.adminID,
					Email:      "admin@test.com",
					Role:       "admin",
					AuthMethod: "local",
				}, nil).
				Once()

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/auth/me", app.adminKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var u User
			Expect(json.NewDecoder(resp.Body).Decode(&u)).To(Succeed())
			Expect(string(u.Email)).To(Equal("admin@test.com"))
			Expect(string(u.Role)).To(Equal("admin"))
		})

		It("returns 401 without API key", func() {
			resp := app.do(
				app.req(http.MethodGet, "/api/v1/auth/me", "invalid-token", nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})
	})

	Describe("Invite lifecycle (admin)", func() {
		It("lists invites (empty initially)", func() {
			app.auth.EXPECT().
				ListInvites(mock.Anything).
				Return(nil, nil).
				Once()

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/auth/invites", app.adminKey, nil),
			)
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var invites []Invite
			Expect(json.NewDecoder(resp.Body).Decode(&invites)).To(Succeed())
			Expect(invites).To(BeEmpty())
		})

		It(
			"lists invites after creation — exercises toAPIInvite for bound and unbound emails",
			func() {
				app.auth.EXPECT().
					CreateInvite(mock.Anything, app.adminID, "bound@test.com", "member", time.Hour).
					Return("raw-bound", &ent.Invite{
						ID:         1,
						Email:      "bound@test.com",
						Role:       "member",
						ExpiresAt:  time.Now().Add(time.Hour),
						CreateTime: time.Now(),
					}, nil).
					Once()
				app.auth.EXPECT().
					CreateInvite(mock.Anything, app.adminID, "", "admin", time.Hour).
					Return("raw-unbound", &ent.Invite{
						ID:         2,
						Role:       "admin",
						ExpiresAt:  time.Now().Add(time.Hour),
						CreateTime: time.Now(),
					}, nil).
					Once()
				app.auth.EXPECT().
					ListInvites(mock.Anything).
					Return([]*ent.Invite{
						{
							ID:         1,
							Email:      "bound@test.com",
							Role:       "member",
							ExpiresAt:  time.Now().Add(time.Hour),
							CreateTime: time.Now(),
						},
						{
							ID:         2,
							Role:       "admin",
							ExpiresAt:  time.Now().Add(time.Hour),
							CreateTime: time.Now(),
						},
					}, nil).
					Once()

				for _, body := range []string{
					`{"email": "bound@test.com", "role": "member", "ttl": "1h"}`,
					`{"role": "admin", "ttl": "1h"}`,
				} {
					req := app.req(
						http.MethodPost,
						"/api/v1/auth/invites",
						app.adminKey,
						strings.NewReader(body),
					)
					req.Header.Set("Content-Type", "application/json")
					resp := app.do(req)
					Expect(resp.StatusCode).To(Equal(http.StatusCreated))
					resp.Body.Close()
				}

				resp := app.do(
					app.req(
						http.MethodGet,
						"/api/v1/auth/invites",
						app.adminKey,
						nil,
					),
				)
				defer resp.Body.Close()

				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var invites []Invite
				Expect(json.NewDecoder(resp.Body).Decode(&invites)).To(Succeed())
				Expect(invites).To(HaveLen(2))

				var bound, unbound *Invite
				for i := range invites {
					if invites[i].Email != nil {
						bound = &invites[i]
					} else {
						unbound = &invites[i]
					}
				}
				Expect(bound).NotTo(BeNil())
				Expect(string(*bound.Email)).To(Equal("bound@test.com"))
				Expect(string(bound.Role)).To(Equal("member"))
				Expect(bound.UsedAt).To(BeNil())
				Expect(bound.ExpiresAt).NotTo(BeZero())

				Expect(unbound).NotTo(BeNil())
				Expect(string(unbound.Role)).To(Equal("admin"))
			},
		)

		It("creates an invite and returns raw token + URL", func() {
			app.auth.EXPECT().
				CreateInvite(mock.Anything, app.adminID, "invited@test.com", "member", 24*time.Hour).
				Return("raw-invited", &ent.Invite{
					ID:         5,
					Email:      "invited@test.com",
					Role:       "member",
					ExpiresAt:  time.Now().Add(24 * time.Hour),
					CreateTime: time.Now(),
				}, nil).
				Once()

			body := `{"email": "invited@test.com", "role": "member", "ttl": "24h"}`
			req := app.req(
				http.MethodPost,
				"/api/v1/auth/invites",
				app.adminKey,
				strings.NewReader(body),
			)
			req.Header.Set("Content-Type", "application/json")
			resp := app.do(req)
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusCreated))

			var created InviteCreated
			Expect(json.NewDecoder(resp.Body).Decode(&created)).To(Succeed())
			Expect(created.RawToken).To(Equal("raw-invited"))
			Expect(
				created.Url,
			).To(ContainSubstring("/register?token=" + created.RawToken))
			Expect(created.Email).NotTo(BeNil())
			Expect(string(*created.Email)).To(Equal("invited@test.com"))
		})

		It("revokes nonexistent invite → 404", func() {
			app.auth.EXPECT().
				RevokeInvite(mock.Anything, uint32(999)).
				Return(fmt.Errorf("invite not found")).
				Once()

			resp := app.do(
				app.req(
					http.MethodDelete,
					"/api/v1/auth/invites/999",
					app.adminKey,
					nil,
				),
			)
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("revokes existing invite → 204", func() {
			app.auth.EXPECT().
				CreateInvite(mock.Anything, app.adminID, "revoke@test.com", "member", mock.Anything).
				Return("raw-revoke", &ent.Invite{
					ID:         9,
					Email:      "revoke@test.com",
					Role:       "member",
					ExpiresAt:  time.Now().Add(time.Hour),
					CreateTime: time.Now(),
				}, nil).
				Once()
			app.auth.EXPECT().
				RevokeInvite(mock.Anything, uint32(9)).
				Return(nil).
				Once()

			body := `{"email": "revoke@test.com", "role": "member"}`
			req := app.req(
				http.MethodPost,
				"/api/v1/auth/invites",
				app.adminKey,
				strings.NewReader(body),
			)
			req.Header.Set("Content-Type", "application/json")
			resp := app.do(req)
			var created InviteCreated
			Expect(json.NewDecoder(resp.Body).Decode(&created)).To(Succeed())
			resp.Body.Close()

			resp = app.do(
				app.req(
					http.MethodDelete,
					fmt.Sprintf("/api/v1/auth/invites/%d", created.Id),
					app.adminKey,
					nil,
				),
			)
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})
	})

	Describe("Non-admin access to invites", func() {
		It("returns 403 when member tries to list invites", func() {
			app.addMember("member@test.com")

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/auth/invites", app.memberKey, nil),
			)
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})
})
