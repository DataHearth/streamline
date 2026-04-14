package auth

import (
	"net/http"
)

const transientCookieMaxAge = 600 // 10 min

// SetTransientCookie writes a short-lived httpOnly cookie scoped to
// /auth/oidc/. Used to carry state + nonce + PKCE verifier across the
// provider redirect round-trip.
func SetTransientCookie(w http.ResponseWriter, r *http.Request, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/auth/oidc/",
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   transientCookieMaxAge,
	})
}

// ReadTransientCookies returns a map of name→value for each requested cookie.
// Missing cookies map to empty string.
func ReadTransientCookies(r *http.Request, names ...string) map[string]string {
	out := make(map[string]string, len(names))
	for _, n := range names {
		if c, err := r.Cookie(n); err == nil {
			out[n] = c.Value
		} else {
			out[n] = ""
		}
	}
	return out
}

// ClearTransientCookies expires the named cookies.
func ClearTransientCookies(w http.ResponseWriter, r *http.Request, names ...string) {
	for _, n := range names {
		http.SetCookie(w, &http.Cookie{
			Name:     n,
			Value:    "",
			Path:     "/auth/oidc/",
			HttpOnly: true,
			Secure:   isSecure(r),
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})
	}
}
