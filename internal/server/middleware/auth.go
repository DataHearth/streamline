package middleware

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/utils/httputil"
)

// Authenticator is the minimum surface the middleware needs from the auth
// service. Defined here (consumer side) so unit tests can mock it without
// depending on the full Manager.
type Authenticator interface {
	ValidateAPIKey(ctx context.Context, key string) (*ent.User, error)
	ValidateToken(token string) (*auth.Claims, error)
	ValidateSession(ctx context.Context, jti string) error
	TouchSessionAsync(jti string)
}

// NewAuth returns the HTTP authentication middleware. Mode and
// trusted-network settings come from the config singleton (auth.mode,
// auth.trusted_networks, auth.trusted_role). excludePaths bypass auth
// entirely (e.g. "/health"); paths ending in "/" match as prefix.
//
// In "disabled" mode all requests pass through without auth.
// In "trusted-network" mode requests from trusted CIDRs are assigned the
// configured role; others must authenticate. In "full" mode requests under
// /api/v1/ must carry a valid Bearer token, X-API-Key header, or — for
// requests a browser proves are same-origin (see sameOriginAPIRequest) — a
// valid streamline_session cookie (401 on failure). All other paths must carry
// a valid streamline_session cookie (302 redirect to /login on failure).
func NewAuth(
	svc Authenticator,
	excludePaths []string,
) func(http.Handler) http.Handler {
	cfg := config.Get()
	mode := cfg.Auth.Mode
	trustedRole := cfg.Auth.TrustedRole

	var trustedNets []*net.IPNet
	for _, cidr := range cfg.Auth.TrustedNetworks {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil {
			trustedNets = append(trustedNets, ipNet)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range excludePaths {
				if r.URL.Path == p ||
					(strings.HasSuffix(p, "/") && strings.HasPrefix(r.URL.Path, p)) {
					next.ServeHTTP(w, r)
					return
				}
			}

			if mode == "disabled" {
				next.ServeHTTP(w, r)
				return
			}

			if mode == "trusted-network" && isTrusted(r, trustedNets) {
				ctx := auth.ContextWithClaims(r.Context(), &auth.Claims{
					Role: trustedRole,
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if strings.HasPrefix(r.URL.Path, "/api/v1/") {
				authenticateAPI(svc, next, w, r)
				return
			}
			authenticateWeb(svc, next, w, r)
		})
	}
}

func authenticateAPI(
	svc Authenticator,
	next http.Handler,
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx := r.Context()
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		u, err := svc.ValidateAPIKey(ctx, apiKey)
		if err != nil {
			slog.InfoContext(
				ctx,
				"api auth rejected",
				"reason",
				"invalid api key",
				"auth.method",
				"api_key",
			)
			http.Error(w, "invalid API key", http.StatusUnauthorized)
			return
		}
		ctx := auth.ContextWithClaims(ctx, &auth.Claims{
			UserID:      u.ID,
			Email:       u.Email,
			DisplayName: u.DisplayName,
			Role:        string(u.Role),
		})
		next.ServeHTTP(w, r.WithContext(ctx))
		return
	}
	if tok := extractBearer(r); tok != "" {
		claims, err := svc.ValidateToken(tok)
		if err != nil {
			slog.InfoContext(
				ctx,
				"api auth rejected",
				"reason",
				"invalid bearer token",
				"auth.method",
				"bearer",
			)
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		if err := svc.ValidateSession(ctx, claims.JTI); err != nil {
			slog.InfoContext(
				ctx,
				"api auth rejected",
				"reason",
				"session invalid",
				"auth.method",
				"bearer",
			)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		svc.TouchSessionAsync(claims.JTI)
		ctx := auth.ContextWithClaims(ctx, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
		return
	}
	// Same-origin browser SPA: accept the session cookie on /api/v1 when the
	// request is confirmed same-origin. SameSite=Lax on the cookie already
	// blocks cross-origin POSTs; this adds a second layer that also blocks
	// cross-origin GET-via-fetch.
	//
	// Sec-Fetch-Site cannot be the only signal. Fetch Metadata is appended
	// only for potentially-trustworthy URLs, so a browser on a plain-http LAN
	// address — the default self-hosted shape — sends none of it, and gating
	// on it alone 401s every SPA call there. Origin is the fallback: browsers
	// attach it to cross-origin requests regardless of scheme, and it is a
	// forbidden header name, so a page cannot forge one. Absent both, this
	// fails closed — a machine client belongs on Bearer or X-API-Key, not on
	// a cookie.
	if sameOriginAPIRequest(r) {
		if c, err := r.Cookie(auth.SessionCookie); err == nil {
			claims, err := svc.ValidateToken(c.Value)
			if err == nil {
				if err := svc.ValidateSession(ctx, claims.JTI); err == nil {
					svc.TouchSessionAsync(claims.JTI)
					ctx := auth.ContextWithClaims(ctx, claims)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
		}
	}
	slog.InfoContext(ctx, "api auth rejected", "reason", "no credentials")
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

func authenticateWeb(
	svc Authenticator,
	next http.Handler,
	w http.ResponseWriter,
	r *http.Request,
) {
	c, err := r.Cookie(auth.SessionCookie)
	if err != nil {
		redirectToLogin(w, r)
		return
	}
	claims, err := svc.ValidateToken(c.Value)
	if err != nil {
		redirectToLogin(w, r)
		return
	}
	if err := svc.ValidateSession(r.Context(), claims.JTI); err != nil {
		redirectToLogin(w, r)
		return
	}
	svc.TouchSessionAsync(claims.JTI)
	ctx := auth.ContextWithClaims(r.Context(), claims)
	next.ServeHTTP(w, r.WithContext(ctx))
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	next := r.URL.Path
	if r.URL.RawQuery != "" {
		next += "?" + r.URL.RawQuery
	}
	if !isSafeNext(next) {
		next = "/"
	}
	http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusFound)
}

func isSafeNext(n string) bool {
	if n == "" || !strings.HasPrefix(n, "/") || strings.HasPrefix(n, "//") {
		return false
	}
	return true
}

func extractBearer(r *http.Request) string {
	if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
		return strings.TrimPrefix(a, "Bearer ")
	}
	return ""
}

func isTrusted(r *http.Request, nets []*net.IPNet) bool {
	ip := httputil.ClientIP(r)
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// sameOriginAPIRequest reports whether a cookie-authenticated /api/v1 request
// demonstrably came from this site.
//
// Sec-Fetch-Site settles it where the browser sends it, but Fetch Metadata is
// only appended for potentially-trustworthy URLs, so a plain-http LAN install
// never sees it. Origin covers that tier: browsers attach it to cross-origin
// requests on any scheme, and scripts cannot set it. With neither header the
// request is refused rather than trusted.
func sameOriginAPIRequest(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin":
		return true
	case "":
		// Fall through to Origin below.
	default:
		// cross-site, same-site and none are all refused outright — a present
		// header is authoritative, so Origin must not be able to override it.
		return false
	}

	origins := r.Header.Values("Origin")
	if len(origins) != 1 {
		return false
	}
	u, err := url.Parse(origins[0])
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}
