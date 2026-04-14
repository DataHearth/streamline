package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/go-jose/go-jose/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/config"
)

// --- OIDC httptest provider --------------------------------------------------

type oidcTestProvider struct {
	server   *httptest.Server
	key      *rsa.PrivateKey
	signer   jose.Signer
	issuer   string
	clientID string
}

func newOIDCTestProvider(clientID string) *oidcTestProvider {
	GinkgoHelper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).ToNot(HaveOccurred())
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"),
	)
	Expect(err).ToNot(HaveOccurred())

	p := &oidcTestProvider{key: key, signer: signer, clientID: clientID}
	mux := http.NewServeMux()
	mux.HandleFunc(
		"/.well-known/openid-configuration",
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                p.issuer,
				"authorization_endpoint":                p.issuer + "/authorize",
				"token_endpoint":                        p.issuer + "/token",
				"jwks_uri":                              p.issuer + "/jwks",
				"response_types_supported":              []string{"code"},
				"subject_types_supported":               []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		},
	)
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwk := jose.JSONWebKey{
			Key:       &key.PublicKey,
			Algorithm: "RS256",
			Use:       "sig",
			KeyID:     "test-key",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).
			Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
	})

	srv := httptest.NewServer(mux)
	p.server = srv
	p.issuer = srv.URL
	return p
}

func (p *oidcTestProvider) close() { p.server.Close() }

// --- Test fixture ------------------------------------------------------------

type testApp struct {
	app     *App
	httpSrv *httptest.Server
}

type appOpts struct {
	regMode      string
	seedEmail    string
	seedPassword string
	oidcIssuer   string
}

func newWebAuthTestApp(opts appOpts) *testApp {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	dataDir := filepath.Join(dir, "data")
	Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
	cfgPath := filepath.Join(dir, "config.yaml")

	oidcBlock := ""
	if opts.oidcIssuer != "" {
		oidcBlock = fmt.Sprintf(`
  oidc:
    - name: test
      issuer: %s
      client_id: streamline-test
      client_secret: test-secret
`, opts.oidcIssuer)
	}

	yaml := fmt.Sprintf(`
server:
  host: 127.0.0.1
  port: 8080
data_dir: %s
auth:
  mode: full
  trusted_role: admin
  registration_mode: %s
  session_ttl: 168h
  oidc_default_role: member
  seed_admin:
    email: %q
    password: %q%s
library:
  movie_path: /tmp/streamline/movies
  movie_naming: "{title}"
  import_mode: hardlink
  default_quality:
    preferred_resolution: 1080p
    min_resolution: 720p
    no_match_cooldown: 6h
    max_grab_failures: 3
schedules:
  rss_sync: 15m
  metadata_refresh: 24h
  download_monitor: 30s
  missing_search: 12h
  cleanup: 24h
log:
  level: info
  format: text
`, dataDir, opts.regMode, opts.seedEmail, opts.seedPassword, oidcBlock)
	Expect(os.WriteFile(cfgPath, []byte(yaml), 0o600)).To(Succeed())

	config.ResetForTest()
	_, err := config.Load(cfgPath)
	Expect(err).ToNot(HaveOccurred())

	ctx := context.Background()
	app, err := NewFromConfig(ctx)
	Expect(err).ToNot(HaveOccurred())
	srv := httptest.NewServer(app.Server.Router())

	return &testApp{app: app, httpSrv: srv}
}

func (t *testApp) close() {
	t.httpSrv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = t.app.Auth.Shutdown(ctx)
	_ = t.app.DB.Close()
	config.ResetForTest()
}

func clientNoRedirect() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// jsonPost helper sends a JSON body to the given URL and returns the response.
func jsonPost(url string, body any) (*http.Response, error) {
	GinkgoHelper()
	buf, err := json.Marshal(body)
	Expect(err).ToNot(HaveOccurred())
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	return clientNoRedirect().Do(req)
}

// loginAsSeedAdmin POSTs JSON credentials and returns the session JWT
// captured from the response cookies.
func loginAsSeedAdmin(app *testApp, email, pw string) string {
	GinkgoHelper()
	resp, err := jsonPost(app.httpSrv.URL+"/auth/login", map[string]string{
		"email":    email,
		"password": pw,
	})
	Expect(err).ToNot(HaveOccurred())
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
	for _, ck := range resp.Cookies() {
		if ck.Name == auth.SessionCookie {
			return ck.Value
		}
	}
	Fail("streamline_session cookie not set on login")
	return ""
}

// --- Specs -------------------------------------------------------------------

var _ = Describe("Webui + API auth", Label("integration", "auth"), func() {
	Context("unauthenticated", func() {
		var app *testApp
		BeforeEach(func() {
			app = newWebAuthTestApp(appOpts{regMode: "disabled"})
			DeferCleanup(app.close)
		})

		It("GET / redirects to /login", func() {
			c := clientNoRedirect()
			resp, err := c.Get(app.httpSrv.URL + "/")
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusFound))
			Expect(resp.Header.Get("Location")).To(HavePrefix("/login"))
		})

		It("GET /login serves the SPA shell", func() {
			c := clientNoRedirect()
			resp, err := c.Get(app.httpSrv.URL + "/login")
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get("Content-Type")).To(HavePrefix("text/html"))
		})

		It("GET /api/docs serves the Scalar shell", func() {
			c := clientNoRedirect()
			resp, err := c.Get(app.httpSrv.URL + "/api/docs")
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get("Content-Type")).To(HavePrefix("text/html"))
		})

		It("GET /api/v1/movies returns 401 JSON", func() {
			c := clientNoRedirect()
			resp, err := c.Get(app.httpSrv.URL + "/api/v1/movies")
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("GET /auth/config returns the registration mode + providers", func() {
			c := clientNoRedirect()
			resp, err := c.Get(app.httpSrv.URL + "/auth/config")
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(
				resp.Header.Get("Content-Type"),
			).To(HavePrefix("application/json"))
			var body struct {
				RegistrationMode string `json:"registration_mode"`
				Providers        []struct {
					Name string `json:"name"`
				} `json:"providers"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.RegistrationMode).To(Equal("disabled"))
			Expect(body.Providers).To(BeEmpty())
		})
	})

	Context("seed admin + login", func() {
		var app *testApp
		BeforeEach(func() {
			app = newWebAuthTestApp(appOpts{
				regMode:      "disabled",
				seedEmail:    "admin@x.com",
				seedPassword: "hunter22pw",
			})
			DeferCleanup(app.close)
		})

		It("POST /auth/login with valid creds sets session cookie + 204", func() {
			resp, err := jsonPost(app.httpSrv.URL+"/auth/login", map[string]string{
				"email":    "admin@x.com",
				"password": "hunter22pw",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			found := false
			for _, ck := range resp.Cookies() {
				if ck.Name == auth.SessionCookie && ck.Value != "" {
					found = true
				}
			}
			Expect(found).To(BeTrue())
		})

		It("POST /auth/login with bad password returns 401 JSON", func() {
			resp, err := jsonPost(app.httpSrv.URL+"/auth/login", map[string]string{
				"email":    "admin@x.com",
				"password": "wrong",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			Expect(
				resp.Header.Get("Content-Type"),
			).To(HavePrefix("application/json"))
		})

		It("6th bad login from same IP returns 429", func() {
			for range 5 {
				resp, err := jsonPost(
					app.httpSrv.URL+"/auth/login",
					map[string]string{
						"email":    "admin@x.com",
						"password": "wrong",
					},
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			}
			resp, err := jsonPost(app.httpSrv.URL+"/auth/login", map[string]string{
				"email":    "admin@x.com",
				"password": "wrong",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusTooManyRequests))
		})

		It("authed GET / returns the SPA shell (no redirect)", func() {
			tok := loginAsSeedAdmin(app, "admin@x.com", "hunter22pw")

			c := clientNoRedirect()
			req, _ := http.NewRequest(http.MethodGet, app.httpSrv.URL+"/", nil)
			req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: tok})
			resp, err := c.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get("Content-Type")).To(HavePrefix("text/html"))
		})

		It("POST /auth/logout clears the session cookie", func() {
			tok := loginAsSeedAdmin(app, "admin@x.com", "hunter22pw")
			c := clientNoRedirect()
			req, _ := http.NewRequest(
				http.MethodPost,
				app.httpSrv.URL+"/auth/logout",
				nil,
			)
			req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: tok})
			resp, err := c.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

			cleared := false
			for _, ck := range resp.Cookies() {
				if ck.Name == auth.SessionCookie && ck.MaxAge < 0 {
					cleared = true
				}
			}
			Expect(cleared).To(BeTrue())
		})
	})

	Context("register flow", func() {
		var app *testApp
		BeforeEach(func() {
			app = newWebAuthTestApp(appOpts{regMode: "open"})
			DeferCleanup(app.close)
		})

		It(
			"POST /auth/register in open mode creates a user and issues a session",
			func() {
				resp, err := jsonPost(
					app.httpSrv.URL+"/auth/register",
					map[string]string{
						"email":    "first@x.com",
						"password": "hunter22pw",
						"confirm":  "hunter22pw",
					},
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
				found := false
				for _, ck := range resp.Cookies() {
					if ck.Name == auth.SessionCookie && ck.Value != "" {
						found = true
					}
				}
				Expect(found).To(BeTrue())
			},
		)

		It("rejects short password with 400 JSON", func() {
			resp, err := jsonPost(
				app.httpSrv.URL+"/auth/register",
				map[string]string{
					"email":    "first@x.com",
					"password": "short",
					"confirm":  "short",
				},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("rejects mismatched confirm with 400 JSON", func() {
			resp, err := jsonPost(
				app.httpSrv.URL+"/auth/register",
				map[string]string{
					"email":    "first@x.com",
					"password": "hunter22pw",
					"confirm":  "hunter33pw",
				},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})

	Context("OIDC redirects", func() {
		var app *testApp
		var oidcSrv *oidcTestProvider
		BeforeEach(func() {
			oidcSrv = newOIDCTestProvider("streamline-test")
			app = newWebAuthTestApp(appOpts{
				regMode:    "open",
				oidcIssuer: oidcSrv.issuer,
			})
			DeferCleanup(app.close)
			DeferCleanup(oidcSrv.close)
		})

		It("GET /auth/oidc/unknown/start returns 404", func() {
			c := clientNoRedirect()
			resp, err := c.Get(app.httpSrv.URL + "/auth/oidc/unknown/start")
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("GET /auth/oidc/test/start redirects to the provider", func() {
			c := clientNoRedirect()
			resp, err := c.Get(app.httpSrv.URL + "/auth/oidc/test/start")
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusFound))
			Expect(
				resp.Header.Get("Location"),
			).To(HavePrefix(oidcSrv.issuer + "/authorize"))
		})

		It(
			"callback without state cookie redirects to /login with state_missing",
			func() {
				c := clientNoRedirect()
				resp, err := c.Get(
					app.httpSrv.URL + "/auth/oidc/test/callback?state=abc&code=x",
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusFound))
				Expect(
					resp.Header.Get("Location"),
				).To(Equal("/login?error=oidc_state_missing"))
			},
		)

		It(
			"callback with state mismatch redirects to /login with state_mismatch",
			func() {
				c := clientNoRedirect()
				req, _ := http.NewRequest(
					http.MethodGet,
					app.httpSrv.URL+"/auth/oidc/test/callback?state=wrong&code=x",
					nil,
				)
				req.AddCookie(
					&http.Cookie{
						Name:  "_oidc_state",
						Value: "expected",
						Path:  "/auth/oidc/",
					},
				)
				resp, err := c.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusFound))
				Expect(
					resp.Header.Get("Location"),
				).To(Equal("/login?error=oidc_state_mismatch"))
			},
		)
	})

	Context("legacy redirects", func() {
		var app *testApp
		BeforeEach(func() {
			app = newWebAuthTestApp(appOpts{
				regMode:      "disabled",
				seedEmail:    "admin@x.com",
				seedPassword: "hunter22pw",
			})
			DeferCleanup(app.close)
		})

		// Smoke-test: authenticated browsers visiting the SPA root land on the
		// shell, no double-redirect through the auth middleware.
		It("authed GET /dashboard returns SPA shell", func() {
			tok := loginAsSeedAdmin(app, "admin@x.com", "hunter22pw")
			c := clientNoRedirect()
			req, _ := http.NewRequest(
				http.MethodGet,
				app.httpSrv.URL+"/dashboard",
				nil,
			)
			req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: tok})
			resp, err := c.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get("Content-Type")).To(HavePrefix("text/html"))
		})
	})
})
