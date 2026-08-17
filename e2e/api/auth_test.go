package api

import (
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/apptest"
)

type apiKeyCreated struct {
	Id       uint32 `json:"id"`
	Name     string `json:"name"`
	RawToken string `json:"raw_token"`
}

// createOwnAPIKey mints an API key for the admin and schedules its revocation.
// Cleanup is registered before the first assertion so a later failure cannot
// leak the key; 404 covers specs that revoke it themselves.
func createOwnAPIKey(name string) apiKeyCreated {
	GinkgoHelper()
	resp := post("/api/v1/auth/me/api-keys", adminAuth, map[string]any{
		"name": name,
	})
	defer resp.Body.Close()
	var key apiKeyCreated
	// The closure deliberately reads key.Id after decode below populates it —
	// do not move this registration past the decode. A zero id (create failed
	// before decode) revokes nothing and lands on the tolerated 404.
	DeferCleanup(func() {
		cleanup := del(
			fmt.Sprintf("/api/v1/auth/me/api-keys/%d", key.Id),
			adminAuth,
			nil,
		)
		defer cleanup.Body.Close()
		Expect(cleanup.StatusCode).To(BeElementOf(
			http.StatusNoContent, http.StatusNotFound,
		))
	})
	Expect(resp.StatusCode).To(Equal(http.StatusCreated))
	decode(resp, &key)
	return key
}

var _ = Describe("REST API auth", Label("e2e"), func() {
	Describe("GET /auth/me", func() {
		It("rejects unauthenticated requests", func() {
			resp := get("/api/v1/auth/me", anon)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("rejects a malformed bearer token", func() {
			resp := get("/api/v1/auth/me", identity{bearer: "not-a-jwt"})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It(
			"authenticates with the session JWT and returns the current user",
			func() {
				resp := get("/api/v1/auth/me", adminAuth)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var user struct {
					Id    uint32 `json:"id"`
					Email string `json:"email"`
					Role  string `json:"role"`
				}
				decode(resp, &user)
				Expect(user.Email).To(Equal(apptest.AdminEmail))
				Expect(user.Role).To(Equal("admin"))
				Expect(user.Id).NotTo(BeZero())
			},
		)

		It("authenticates with an API key", func() {
			resp := get("/api/v1/auth/me", viewerAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var user struct {
				Email string `json:"email"`
				Role  string `json:"role"`
			}
			decode(resp, &user)
			Expect(user.Email).To(Equal(viewerEmail))
			Expect(user.Role).To(Equal("request_only"))
		})
	})

	Describe("PATCH /auth/me", func() {
		It("updates the caller's display name", func() {
			DeferCleanup(func() {
				restore := patch("/api/v1/auth/me", viewerAuth, map[string]any{
					"display_name": "",
				})
				defer restore.Body.Close()
				Expect(restore.StatusCode).To(Equal(http.StatusOK))
			})

			resp := patch("/api/v1/auth/me", viewerAuth, map[string]any{
				"display_name": "E2E Viewer",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var user struct {
				DisplayName *string `json:"display_name"`
			}
			decode(resp, &user)
			Expect(user.DisplayName).NotTo(BeNil())
			Expect(*user.DisplayName).To(Equal("E2E Viewer"))
		})
	})

	Describe("POST /auth/password", func() {
		It("rejects a wrong current password", func() {
			resp := post("/api/v1/auth/password", viewerAuth, map[string]any{
				"current_password": "definitely-not-the-password",
				"new_password":     "another-Passw0rd!",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("rejects a new password below the policy floor", func() {
			resp := post("/api/v1/auth/password", viewerAuth, map[string]any{
				"current_password": viewerPassword,
				"new_password":     "short",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		// Rotates the admin, not the viewer: the rotation deletes every API
		// key of the caller, and viewerAuth is the X-API-Key identity most of
		// the suite authenticates with. The admin holds a Bearer session,
		// which survives its own rotation via keepJTI.
		//
		// A leaked rotation would break every later spec that authenticates as
		// the admin, so the restore is registered before the rotation is even
		// issued. 401 means the rotation never landed.
		It("rotates the caller's password and revokes their API keys", func() {
			const rotated = "e2e-Rotated-Passw0rd!"
			DeferCleanup(func() {
				back := post("/api/v1/auth/password", adminAuth, map[string]any{
					"current_password": rotated,
					"new_password":     apptest.AdminPassword,
				})
				defer back.Body.Close()
				Expect(back.StatusCode).To(BeElementOf(
					http.StatusNoContent, http.StatusUnauthorized,
				))
			})

			key := createOwnAPIKey("e2e-s04-revoked")

			resp := post("/api/v1/auth/password", adminAuth, map[string]any{
				"current_password": apptest.AdminPassword,
				"new_password":     rotated,
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

			probe := get("/api/v1/auth/me", identity{apiKey: key.RawToken})
			defer probe.Body.Close()
			Expect(probe.StatusCode).To(Equal(http.StatusUnauthorized))

			listed := get("/api/v1/auth/me/api-keys", adminAuth)
			defer listed.Body.Close()
			Expect(listed.StatusCode).To(Equal(http.StatusOK))
			var keys []struct {
				Id uint32 `json:"id"`
			}
			decode(listed, &keys)
			Expect(keys).NotTo(ContainElement(HaveField("Id", key.Id)))
		})
	})

	Describe("/auth/me/api-keys", func() {
		It("creates, lists and revokes an API key", func() {
			key := createOwnAPIKey("e2e-account-key")
			Expect(key.Name).To(Equal("e2e-account-key"))
			Expect(key.RawToken).NotTo(BeEmpty())
			Expect(key.Id).NotTo(BeZero())

			listed := get("/api/v1/auth/me/api-keys", adminAuth)
			defer listed.Body.Close()
			Expect(listed.StatusCode).To(Equal(http.StatusOK))
			var keys []struct {
				Id   uint32 `json:"id"`
				Name string `json:"name"`
			}
			decode(listed, &keys)
			Expect(keys).To(ContainElement(HaveField("Id", key.Id)))

			revoked := del(
				fmt.Sprintf("/api/v1/auth/me/api-keys/%d", key.Id),
				adminAuth,
				nil,
			)
			defer revoked.Body.Close()
			Expect(revoked.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("404s revoking an API key the caller does not own", func() {
			resp := del("/api/v1/auth/me/api-keys/999999", adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("/auth/me/sessions", func() {
		It("lists the caller's sessions and flags the current one", func() {
			resp := get("/api/v1/auth/me/sessions", adminAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var sessions []struct {
				Id        uint32 `json:"id"`
				IsCurrent bool   `json:"is_current"`
			}
			decode(resp, &sessions)
			Expect(sessions).NotTo(BeEmpty())
			Expect(sessions).To(ContainElement(HaveField("IsCurrent", true)))
		})

		It("404s revoking a session the caller does not own", func() {
			resp := del("/api/v1/auth/me/sessions/999999", adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	// The happy path rotates the signing secret and truncates every session,
	// which would invalidate the suite-wide admin bearer. Only the RBAC guard
	// is exercised here.
	Describe("POST /auth/jwt/rotate", func() {
		It("403s for a non-admin", func() {
			resp := post("/api/v1/auth/jwt/rotate", viewerAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("/auth/invites", func() {
		It("creates, lists and revokes an invite", func() {
			created := post("/api/v1/auth/invites", adminAuth, map[string]any{
				"email": "e2e-invitee@streamline.local",
				"role":  "member",
				"ttl":   "1h",
			})
			defer created.Body.Close()
			Expect(created.StatusCode).To(Equal(http.StatusCreated))
			var invite struct {
				Id       uint32 `json:"id"`
				Role     string `json:"role"`
				RawToken string `json:"raw_token"`
				Url      string `json:"url"`
			}
			decode(created, &invite)
			Expect(invite.Role).To(Equal("member"))
			Expect(invite.RawToken).NotTo(BeEmpty())
			Expect(invite.Url).To(ContainSubstring("/register?token="))

			listed := get("/api/v1/auth/invites", adminAuth)
			defer listed.Body.Close()
			Expect(listed.StatusCode).To(Equal(http.StatusOK))
			var invites []struct {
				Id uint32 `json:"id"`
			}
			decode(listed, &invites)
			Expect(invites).To(ContainElement(HaveField("Id", invite.Id)))

			revoked := del(
				fmt.Sprintf("/api/v1/auth/invites/%d", invite.Id),
				adminAuth,
				nil,
			)
			defer revoked.Body.Close()
			Expect(revoked.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("404s revoking an unknown invite", func() {
			resp := del("/api/v1/auth/invites/999999", adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("403s listing invites as a non-admin", func() {
			resp := get("/api/v1/auth/invites", viewerAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("403s creating an invite as a non-admin", func() {
			resp := post("/api/v1/auth/invites", viewerAuth, map[string]any{
				"email": "e2e-nope@streamline.local",
				"role":  "member",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})
})
