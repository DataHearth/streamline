package api

import (
	"bytes"
	"encoding/json"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/testutil/apptest"
)

// auth.Limiter allows 5 /auth/login attempts per 15 minutes per IP; this
// suite spends 1 — keep total login submissions under 5 as specs grow.
var _ = Describe("REST API auth", Label("e2e"), func() {
	It("rejects unauthenticated requests", func() {
		resp := get("/api/v1/auth/me", "")
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("rejects a malformed bearer token", func() {
		resp := get("/api/v1/auth/me", "not-a-jwt")
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("authenticates with the session JWT and returns the current user", func() {
		resp := get("/api/v1/auth/me", adminToken())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var user struct {
			Email string `json:"email"`
		}
		Expect(json.NewDecoder(resp.Body).Decode(&user)).To(Succeed())
		Expect(user.Email).To(Equal(apptest.AdminEmail))
	})
})

func get(path, bearer string) *http.Response {
	GinkgoHelper()
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	Expect(err).NotTo(HaveOccurred())
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}

// adminToken logs the seed admin in through the web endpoint and returns the
// session JWT, which /api/v1 accepts as a Bearer token.
func adminToken() string {
	GinkgoHelper()
	body, err := json.Marshal(map[string]string{
		"email":    apptest.AdminEmail,
		"password": apptest.AdminPassword,
	})
	Expect(err).NotTo(HaveOccurred())
	resp, err := http.Post(
		baseURL+"/auth/login",
		"application/json",
		bytes.NewReader(body),
	)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
	for _, c := range resp.Cookies() {
		if c.Name == auth.SessionCookie {
			return c.Value
		}
	}
	Fail("login response did not set the session cookie")
	return ""
}
