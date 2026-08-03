package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/apptest"
	"github.com/datahearth/streamline/internal/auth"
)

// identity is the credential a request authenticates with; the zero value is
// anonymous. /api/v1 accepts either a session JWT as Bearer or a raw API key.
type identity struct {
	bearer string
	apiKey string
}

var (
	anon identity

	// adminAuth (seed admin, Bearer) and viewerAuth (request_only, X-API-Key)
	// are minted once in BeforeSuite. auth.Limiter allows 5 POST /auth/login
	// attempts per 15 minutes per IP; bootstrapIdentities spends exactly 2, so
	// specs must never log in themselves.
	adminAuth, viewerAuth identity

	// adminUserID and viewerUserID back the /users/{uid} specs.
	adminUserID, viewerUserID uint32

	// A per-suite client rather than http.DefaultClient: a target that drops
	// instead of refusing would otherwise hang forever and burn the whole
	// suite's --timeout budget on one probe.
	httpClient = &http.Client{Timeout: 10 * time.Second}
)

const (
	viewerEmail    = "e2e-viewer@streamline.local"
	viewerPassword = "e2e-Viewer-Passw0rd!"

	// hash40 is a well-formed infohash. The suite runs without a builtin
	// download client, so every /torrents route short-circuits before it is
	// ever looked up.
	hash40 = "0123456789abcdef0123456789abcdef01234567"
)

func (i identity) apply(req *http.Request) {
	if i.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+i.bearer)
	}
	if i.apiKey != "" {
		req.Header.Set("X-API-Key", i.apiKey)
	}
}

// do issues one request against the running app. A non-nil body is marshalled
// as JSON. The caller owns closing the returned body.
func do(method, path string, id identity, body any) *http.Response {
	GinkgoHelper()
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		Expect(err).NotTo(HaveOccurred())
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, baseURL+path, payload)
	Expect(err).NotTo(HaveOccurred())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	id.apply(req)
	resp, err := httpClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}

func get(path string, id identity) *http.Response {
	GinkgoHelper()
	return do(http.MethodGet, path, id, nil)
}

func post(path string, id identity, body any) *http.Response {
	GinkgoHelper()
	return do(http.MethodPost, path, id, body)
}

func put(path string, id identity, body any) *http.Response {
	GinkgoHelper()
	return do(http.MethodPut, path, id, body)
}

func patch(path string, id identity, body any) *http.Response {
	GinkgoHelper()
	return do(http.MethodPatch, path, id, body)
}

func del(path string, id identity, body any) *http.Response {
	GinkgoHelper()
	return do(http.MethodDelete, path, id, body)
}

func decode(resp *http.Response, out any) {
	GinkgoHelper()
	Expect(json.NewDecoder(resp.Body).Decode(out)).To(Succeed())
}

// bootstrapIdentities logs the seed admin in, provisions a request_only user,
// and mints that user an API key. Called once from BeforeSuite.
func bootstrapIdentities() {
	GinkgoHelper()
	adminAuth = identity{bearer: login(apptest.AdminEmail, apptest.AdminPassword)}

	adminUserID = currentUserID(adminAuth)

	created := post("/api/v1/users", adminAuth, map[string]any{
		"email":    viewerEmail,
		"password": viewerPassword,
		"role":     "request_only",
	})
	defer created.Body.Close()
	Expect(created.StatusCode).To(Equal(http.StatusCreated))
	var viewer struct {
		Id uint32 `json:"id"`
	}
	decode(created, &viewer)
	viewerUserID = viewer.Id

	viewerBearer := identity{bearer: login(viewerEmail, viewerPassword)}
	key := post("/api/v1/auth/me/api-keys", viewerBearer, map[string]any{
		"name": "e2e-viewer",
	})
	defer key.Body.Close()
	Expect(key.StatusCode).To(Equal(http.StatusCreated))
	var minted struct {
		RawToken string `json:"raw_token"`
	}
	decode(key, &minted)
	Expect(minted.RawToken).NotTo(BeEmpty())
	viewerAuth = identity{apiKey: minted.RawToken}
}

// currentUserID resolves the caller's own user id via /auth/me.
func currentUserID(id identity) uint32 {
	GinkgoHelper()
	resp := get("/api/v1/auth/me", id)
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusOK))
	var user struct {
		Id uint32 `json:"id"`
	}
	decode(resp, &user)
	Expect(user.Id).NotTo(BeZero())
	return user.Id
}

// login posts to the web login endpoint and returns the session JWT, which
// /api/v1 accepts as a Bearer token.
func login(email, password string) string {
	GinkgoHelper()
	resp := post("/auth/login", anon, map[string]string{
		"email":    email,
		"password": password,
	})
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
