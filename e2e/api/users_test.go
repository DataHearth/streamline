package api

import (
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/apptest"
)

// createUser provisions a member user and schedules its removal. Cleanup is
// registered before the first assertion so a later failure cannot leak the
// entity; 404 covers specs that delete it themselves.
func createUser(email string) uint32 {
	GinkgoHelper()
	resp := post("/api/v1/users", adminAuth, map[string]any{
		"email":        email,
		"password":     "e2e-Temp-Passw0rd!",
		"role":         "member",
		"display_name": "E2E Temp",
	})
	defer resp.Body.Close()
	var user struct {
		Id uint32 `json:"id"`
	}
	// The closure deliberately reads user.Id after decode below populates it —
	// do not move this registration past the decode. A zero id (create failed
	// before decode) deletes nothing and lands on the tolerated 404.
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
		// authenticates by API key, so it has no current session to observe.
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
