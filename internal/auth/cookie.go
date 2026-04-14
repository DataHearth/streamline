package auth

import (
	"net/http"
	"time"
)

// SessionCookie is the name of the httpOnly cookie that carries the webui
// session JWT. API clients (Authorization: Bearer, X-API-Key) must NOT use it.
const SessionCookie = "streamline_session"

// isSecure reports whether the request arrived via TLS, directly or through
// a proxy that sets X-Forwarded-Proto=https.
func isSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
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
