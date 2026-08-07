package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/utils/httputil"
)

// SessionCookie is the name of the httpOnly cookie that carries the webui
// session JWT. API clients (Authorization: Bearer, X-API-Key) must NOT use it.
const SessionCookie = "streamline_session"

// isSecure reports whether the browser reached us over https, so the session
// cookie can carry Secure.
//
// A configured public https URL settles it on its own: it is the operator
// stating the browser-facing scheme, and it covers the case that actually
// bites — a TLS-terminating proxy that forwards no X-Forwarded-Proto at all,
// which would otherwise emit the session JWT without Secure. The header is
// believed only from a configured proxy, since off one it is attacker-supplied.
func isSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if strings.HasPrefix(config.PublicURL(), "https://") {
		return true
	}
	return httputil.TrustedPeer(r) &&
		r.Header.Get("X-Forwarded-Proto") == "https"
}

// SetSession writes the session cookie. Secure flag tracks the current
// request's transport so local http development still works.
func SetSession(
	w http.ResponseWriter,
	r *http.Request,
	token string,
	ttl time.Duration,
) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
	})
}

// ClearSession expires the session cookie.
func ClearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
