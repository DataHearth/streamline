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
	chimw "github.com/go-chi/chi/v5/middleware"
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
// same-origin browser requests (Sec-Fetch-Site: same-origin) — a valid
// streamline_session cookie (401 on failure). All other paths must carry
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
	// browser confirms the request originated from this site. SameSite=Lax on
	// the cookie already blocks cross-origin POSTs; the Sec-Fetch-Site gate
	// adds a second layer that also blocks cross-origin GET-via-fetch and
	// fails closed when the header is absent (legacy/non-fetch contexts).
	if r.Header.Get("Sec-Fetch-Site") == "same-origin" {
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
	ip := ClientIP(r)
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

// ClientIP returns the client IP resolved by the chi ClientIPFrom* middleware
// wired in the server, falling back to the connecting RemoteAddr when none was
// set (no proxy in front, or the XFF chain too short to trust).
func ClientIP(r *http.Request) net.IP {
	if ip := chimw.GetClientIP(r.Context()); ip != "" {
		return net.ParseIP(ip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return net.ParseIP(r.RemoteAddr)
	}
	return net.ParseIP(host)
}
