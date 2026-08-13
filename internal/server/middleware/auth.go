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
// apiLimiter meters /api/v1 credential failures per client address; nil turns
// that off (tests, and any caller that has not wired one).
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
	apiLimiter auth.Limiter,
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
				authenticateAPI(svc, apiLimiter, next, w, r)
				return
			}
			authenticateWeb(svc, next, w, r)
		})
	}
}

func authenticateAPI(
	svc Authenticator,
	limiter auth.Limiter,
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
			rejectAPI(limiter, w, r, "invalid API key")
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
			rejectAPI(limiter, w, r, "invalid token")
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
			rejectAPI(limiter, w, r, "unauthorized")
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
	rejectAPI(limiter, w, r, "unauthorized")
}

// rejectAPI answers an /api/v1 request that failed to authenticate, and meters
// the failure per client address so credential guessing against the API costs
// the same as guessing against /auth/login.
//
// Only failures are charged, so a caller holding a working key is never metered
// and needs no exemption: a bulk consumer, an e2e run and the SPA's own polling
// all pass through untouched. That is also what makes the ceiling generous
// enough to be safe — an expired session fires every query on the page at once,
// and each of those 401s is charged, so a limit as tight as login's five would
// turn one stale tab into a throttled address.
//
// Charging here rather than on the way in is what buys that, and it costs the
// credential check itself: a request that is already over budget has had its key
// hashed and looked up before this refuses it. That is affordable precisely
// because nothing on the API path is bcrypt — ValidateAPIKey is a sha256 and one
// indexed row, ValidateToken an HMAC verify — which is the whole reason
// /auth/login could not be metered this way and needed the charge-then-refund
// dance instead.
//
// A refused request never reaches the handler. It does not bound work by an
// *authenticated* caller: anyone holding a valid key can still make requests as
// fast as the server answers them, which is the same trust the API's RBAC
// already extends. Metering that too is a per-user quota, not this.
func rejectAPI(
	limiter auth.Limiter,
	w http.ResponseWriter,
	r *http.Request,
	msg string,
) {
	if limiter == nil {
		http.Error(w, msg, http.StatusUnauthorized)
		return
	}
	if ok, wait := limiter.Allow(httputil.ClientIPString(r)); !ok {
		w.Header().Set("Retry-After", httputil.RetryAfterSeconds(wait))
		http.Error(
			w,
			"too many failed attempts",
			http.StatusTooManyRequests,
		)
		return
	}
	http.Error(w, msg, http.StatusUnauthorized)
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
	raw := r.URL.Path
	if r.URL.RawQuery != "" {
		raw += "?" + r.URL.RawQuery
	}
	next, ok := httputil.SafeNextPath(raw)
	if !ok {
		next = "/"
	}
	http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusFound)
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
