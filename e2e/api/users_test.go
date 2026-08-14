package api

import (
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/apptest"
	"github.com/datahearth/streamline/internal/auth"
)

// createUser provisions a member user and schedules its removal.
func createUser(email string) uint32 {
	GinkgoHelper()
	return createRoleUser(email, "member")
}

var _ = Describe("REST API users", Label("e2e"), func() {
	Describe("GET /users", func() {
		It("returns a paginated list", func() {
			resp := get("/api/v1/users?limit=100&sort=created&order=asc", adminAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var list struct {
				Items []struct {
					Email string `json:"email"`
				} `json:"items"`
				Total uint32 `json:"total"`
			}
			decode(resp, &list)
			Expect(list.Total).To(BeNumerically(">=", 2))
			Expect(list.Items).To(ContainElement(
				HaveField("Email", apptest.AdminEmail),
			))
		})

		It("filters by role and search term", func() {
			resp := get("/api/v1/users?role=request_only&q=e2e-viewer", adminAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var list struct {
				Items []struct {
					Role string `json:"role"`
				} `json:"items"`
			}
			decode(resp, &list)
			Expect(list.Items).NotTo(BeEmpty())
			for _, u := range list.Items {
				Expect(u.Role).To(Equal("request_only"))
			}
		})
	})

	Describe("POST /users", func() {
		It("creates a user", func() {
			id := createUser("e2e-created@streamline.local")
			Expect(id).NotTo(BeZero())
		})

		It("409s a duplicate email", func() {
			resp := post("/api/v1/users", adminAuth, map[string]any{
				"email":    apptest.AdminEmail,
				"password": "e2e-Temp-Passw0rd!",
				"role":     "member",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		It("422s a password below the policy floor", func() {
			resp := post("/api/v1/users", adminAuth, map[string]any{
				"email":    "e2e-weak@streamline.local",
				"password": "short",
				"role":     "member",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})
	})

	Describe("/users/{uid}", func() {
		// Targets the admin, not the viewer: the admin is the identity holding
		// a live session, so is_current is observable here. The viewer
		// authenticates by API key and its session is revoked by the
		// password-rotation spec.
		It("returns the user detail block", func() {
			resp := get(fmt.Sprintf("/api/v1/users/%d", adminUserID), adminAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var detail struct {
				User struct {
					Id    uint32 `json:"id"`
					Email string `json:"email"`
				} `json:"user"`
				Sessions []struct {
					Id        uint32 `json:"id"`
					IsCurrent bool   `json:"is_current"`
				} `json:"sessions"`
			}
			decode(resp, &detail)
			Expect(detail.User.Id).To(Equal(adminUserID))
			Expect(detail.User.Email).To(Equal(apptest.AdminEmail))
			Expect(detail.Sessions).To(ContainElement(HaveField("IsCurrent", true)))
		})

		It("patches the user's role and display name", func() {
			id := createUser("e2e-patch@streamline.local")
			resp := patch(
				fmt.Sprintf("/api/v1/users/%d", id),
				adminAuth,
				map[string]any{
					"role":         "request_only",
					"display_name": "Patched",
				},
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var user struct {
				Role        string  `json:"role"`
				DisplayName *string `json:"display_name"`
			}
			decode(resp, &user)
			Expect(user.Role).To(Equal("request_only"))
			Expect(user.DisplayName).NotTo(BeNil())
			Expect(*user.DisplayName).To(Equal("Patched"))
		})

		It("409s demoting the last admin", func() {
			resp := patch(
				fmt.Sprintf("/api/v1/users/%d", adminUserID),
				adminAuth,
				map[string]any{"role": "member"},
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			var body struct {
				Code string `json:"code"`
			}
			decode(resp, &body)
			Expect(body.Code).To(Equal("last_admin"))
		})

		It("409s deleting yourself", func() {
			resp := del(fmt.Sprintf("/api/v1/users/%d", adminUserID), adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			var body struct {
				Code string `json:"code"`
			}
			decode(resp, &body)
			Expect(body.Code).To(Equal("self_delete_forbidden"))
		})

		It("deletes a user", func() {
			id := createUser("e2e-delete@streamline.local")
			resp := del(fmt.Sprintf("/api/v1/users/%d", id), adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("404s an unknown user", func() {
			read := get("/api/v1/users/999999", adminAuth)
			defer read.Body.Close()
			Expect(read.StatusCode).To(Equal(http.StatusNotFound))

			patched := patch("/api/v1/users/999999", adminAuth, map[string]any{
				"display_name": "nobody",
			})
			defer patched.Body.Close()
			Expect(patched.StatusCode).To(Equal(http.StatusNotFound))

			deleted := del("/api/v1/users/999999", adminAuth, nil)
			defer deleted.Body.Close()
			Expect(deleted.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("POST /users/{uid}/password-reset", func() {
		It("resets a user's password", func() {
			id := createUser("e2e-reset@streamline.local")
			resp := post(
				fmt.Sprintf("/api/v1/users/%d/password-reset", id),
				adminAuth,
				map[string]any{"new_password": "e2e-Reset-Passw0rd!"},
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("422s a password below the policy floor", func() {
			id := createUser("e2e-reset-weak@streamline.local")
			resp := post(
				fmt.Sprintf("/api/v1/users/%d/password-reset", id),
				adminAuth,
				map[string]any{"new_password": "short"},
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("404s an unknown user", func() {
			resp := post(
				"/api/v1/users/999999/password-reset",
				adminAuth,
				map[string]any{"new_password": "e2e-Reset-Passw0rd!"},
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("POST /users/{uid}/unlock", func() {
		It("is idempotent on a user that was never locked", func() {
			id := createUser("e2e-unlock@streamline.local")
			resp := post(fmt.Sprintf("/api/v1/users/%d/unlock", id), adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("404s an unknown user", func() {
			resp := post("/api/v1/users/999999/unlock", adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("DELETE /users/{uid}/api-keys/{kid}", func() {
		It("revokes a key belonging to the target user", func() {
			key := createOwnAPIKey("e2e-admin-revoke-me")

			resp := del(
				fmt.Sprintf("/api/v1/users/%d/api-keys/%d", adminUserID, key.Id),
				adminAuth,
				nil,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("404s a key that does not belong to the user", func() {
			resp := del(
				fmt.Sprintf("/api/v1/users/%d/api-keys/999999", adminUserID),
				adminAuth,
				nil,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("DELETE /users/{uid}/sessions/{sid}", func() {
		It("404s a session that does not belong to the user", func() {
			resp := del(
				fmt.Sprintf("/api/v1/users/%d/sessions/999999", adminUserID),
				adminAuth,
				nil,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	It("403s every user-admin route for a non-admin", func() {
		list := get("/api/v1/users", viewerAuth)
		defer list.Body.Close()
		Expect(list.StatusCode).To(Equal(http.StatusForbidden))

		created := post("/api/v1/users", viewerAuth, map[string]any{
			"email":    "e2e-nope@streamline.local",
			"password": "e2e-Temp-Passw0rd!",
			"role":     "member",
		})
		defer created.Body.Close()
		Expect(created.StatusCode).To(Equal(http.StatusForbidden))

		read := get(fmt.Sprintf("/api/v1/users/%d", viewerUserID), viewerAuth)
		defer read.Body.Close()
		Expect(read.StatusCode).To(Equal(http.StatusForbidden))

		patched := patch(
			fmt.Sprintf("/api/v1/users/%d", viewerUserID),
			viewerAuth,
			map[string]any{"role": "admin"},
		)
		defer patched.Body.Close()
		Expect(patched.StatusCode).To(Equal(http.StatusForbidden))

		deleted := del(fmt.Sprintf("/api/v1/users/%d", adminUserID), viewerAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusForbidden))

		reset := post(
			fmt.Sprintf("/api/v1/users/%d/password-reset", adminUserID),
			viewerAuth,
			map[string]any{"new_password": "e2e-Reset-Passw0rd!"},
		)
		defer reset.Body.Close()
		Expect(reset.StatusCode).To(Equal(http.StatusForbidden))

		unlock := post(
			fmt.Sprintf("/api/v1/users/%d/unlock", adminUserID),
			viewerAuth,
			nil,
		)
		defer unlock.Body.Close()
		Expect(unlock.StatusCode).To(Equal(http.StatusForbidden))

		revokeKey := del(
			fmt.Sprintf("/api/v1/users/%d/api-keys/1", adminUserID),
			viewerAuth,
			nil,
		)
		defer revokeKey.Body.Close()
		Expect(revokeKey.StatusCode).To(Equal(http.StatusForbidden))

		revokeSession := del(
			fmt.Sprintf("/api/v1/users/%d/sessions/1", adminUserID),
			viewerAuth,
			nil,
		)
		defer revokeSession.Body.Close()
		Expect(revokeSession.StatusCode).To(Equal(http.StatusForbidden))
	})
})

// createRoleUser provisions a user at an explicit role and schedules its
// removal. The account is always created with rolePassword, so any spec that
// logs in as it can authenticate with that literal.
func createRoleUser(email, role string) uint32 {
	GinkgoHelper()
	resp := post("/api/v1/users", adminAuth, map[string]any{
		"email":    email,
		"password": rolePassword,
		"role":     role,
	})
	defer resp.Body.Close()
	var user struct {
		Id uint32 `json:"id"`
	}
	// The closure deliberately reads user.Id after decode below populates it.
	// Do not move this registration past the decode: cleanup must be armed
	// before the first assertion so a later failure cannot leak the entity. A
	// zero id (create failed before decode) deletes nothing and lands on the
	// tolerated 404, which also covers specs that delete the user themselves.
	DeferCleanup(func() {
		cleanup := del(fmt.Sprintf("/api/v1/users/%d", user.Id), adminAuth, nil)
		defer cleanup.Body.Close()
		Expect(cleanup.StatusCode).To(BeElementOf(
			http.StatusNoContent, http.StatusNotFound,
		))
	})
	Expect(resp.StatusCode).To(Equal(http.StatusCreated))
	decode(resp, &user)
	return user.Id
}

// cookieGet sends the session JWT the way the SPA does, as the session cookie
// on a same-origin request, rather than as a Bearer token.
func cookieGet(path, jwt string) *http.Response {
	GinkgoHelper()
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	Expect(err).NotTo(HaveOccurred())
	req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: jwt})
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := httpClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}

// cookieGetNoRedirect is cookieGet against a web (non-API) path, where an
// invalid session redirects instead of answering 401.
func cookieGetNoRedirect(path, jwt string) *http.Response {
	GinkgoHelper()
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	Expect(err).NotTo(HaveOccurred())
	req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: jwt})
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}

func setRole(id uint32, role string) {
	GinkgoHelper()
	resp := patch(
		fmt.Sprintf("/api/v1/users/%d", id),
		adminAuth,
		map[string]any{"role": role},
	)
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusOK), bodyText(resp))
}

const rolePassword = "e2e-Role-Passw0rd!"

// Sessions carry the role they were issued with, so a role change that did not
// revoke them would leave the old privileges live until session_ttl expires.
// Successful logins are refunded against the per-IP login budget, so these
// specs cost nothing there.
var _ = Describe("role change session revocation", Label("e2e"), func() {
	It("revokes a demoted user's bearer and cookie sessions", func() {
		id := createRoleUser("e2e-demote-bearer@streamline.local", "admin")
		jwt := login("e2e-demote-bearer@streamline.local", rolePassword)

		before := get("/api/v1/users", identity{bearer: jwt})
		defer before.Body.Close()
		Expect(before.StatusCode).To(Equal(http.StatusOK))

		cookieBefore := cookieGet("/api/v1/users", jwt)
		defer cookieBefore.Body.Close()
		Expect(cookieBefore.StatusCode).To(Equal(http.StatusOK))

		setRole(id, "member")

		// 401, not 403: the revoked session is rejected by ValidateSession in
		// the middleware, before RBAC ever reads the stale admin claim.
		after := get("/api/v1/users", identity{bearer: jwt})
		defer after.Body.Close()
		Expect(after.StatusCode).To(Equal(http.StatusUnauthorized))

		cookieAfter := cookieGet("/api/v1/users", jwt)
		defer cookieAfter.Body.Close()
		Expect(cookieAfter.StatusCode).To(Equal(http.StatusUnauthorized))

		web := cookieGetNoRedirect("/settings", jwt)
		defer web.Body.Close()
		Expect(web.StatusCode).To(Equal(http.StatusFound))
		Expect(web.Header.Get("Location")).To(HavePrefix("/login"))
	})

	It("puts the demotion in force on the next login", func() {
		id := createRoleUser("e2e-demote-relogin@streamline.local", "admin")
		jwt := login("e2e-demote-relogin@streamline.local", rolePassword)

		listed := get("/api/v1/users", identity{bearer: jwt})
		defer listed.Body.Close()
		Expect(listed.StatusCode).To(Equal(http.StatusOK))

		setRole(id, "member")

		fresh := identity{
			bearer: login("e2e-demote-relogin@streamline.local", rolePassword),
		}
		me := get("/api/v1/auth/me", fresh)
		defer me.Body.Close()
		Expect(me.StatusCode).To(Equal(http.StatusOK))

		denied := get("/api/v1/users", fresh)
		defer denied.Body.Close()
		Expect(denied.StatusCode).To(Equal(http.StatusForbidden))
	})

	It("revokes on promotion too", func() {
		id := createRoleUser("e2e-promote@streamline.local", "member")
		jwt := login("e2e-promote@streamline.local", rolePassword)

		before := get("/api/v1/auth/me", identity{bearer: jwt})
		defer before.Body.Close()
		Expect(before.StatusCode).To(Equal(http.StatusOK))

		setRole(id, "admin")

		after := get("/api/v1/auth/me", identity{bearer: jwt})
		defer after.Body.Close()
		Expect(after.StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("keeps sessions alive when the patch repeats the current role", func() {
		id := createRoleUser("e2e-same-role@streamline.local", "member")
		jwt := login("e2e-same-role@streamline.local", rolePassword)

		// The SPA's user-edit form always submits the full patch, role
		// included, so a display-name save must not sign the target out.
		resp := patch(
			fmt.Sprintf("/api/v1/users/%d", id),
			adminAuth,
			map[string]any{"role": "member", "display_name": "Renamed"},
		)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		after := get("/api/v1/auth/me", identity{bearer: jwt})
		defer after.Body.Close()
		Expect(after.StatusCode).To(Equal(http.StatusOK))
	})
})
