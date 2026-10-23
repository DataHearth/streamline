package web

import (
	"log/slog"
	"mime"
	"net/http"

	"github.com/datahearth/streamline/internal/utils/httputil"
)

// csrfGuard rejects state-changing requests a browser issued from another
// site. SameSite=Lax only governs when an *existing* cookie is sent; it does
// nothing to stop a cross-site POST from making the server mint a new session,
// which lands the victim inside the attacker's account.
//
// Origin is the primary check and carries the whole guard on the deployment
// shape this project ships by default. Every browser attaches it to every
// non-GET/HEAD request, including a plain cross-site HTML form post, and Fetch
// marks it a forbidden request-header name, so a page's script can neither set
// nor clear it.
//
// Sec-Fetch-Site is an optimisation layered on top, available only to some
// deployments — never a replacement. Fetch Metadata is appended only when the
// request URL is *potentially trustworthy*: https, or a localhost address.
// Against a plain-http LAN address no Sec-Fetch-* header is emitted at all
// (measured on Chrome 147 against http://192.168.1.127:19092 — urlencoded,
// multipart and text/plain form posts, no-cors fetch, bodyless fetch and
// sendBeacon each arrived carrying Origin and no Sec-Fetch-Site). So:
//
//   - Plain http on a LAN name or bare IP — the ordinary self-hosted install —
//     never sees Sec-Fetch-Site. httputil.CrossSiteRequest's Origin comparison is the entire
//     defence there, not a legacy fallback, and removing it would leave these
//     deployments with none.
//   - https, or http://localhost — a reverse-proxied install, or a browser on
//     the host itself — gets Sec-Fetch-Site too and settles on it before
//     Origin is consulted. It reports the initiator relationship the browser
//     computed rather than a value we compare ourselves, so it needs no notion
//     of which hosts we answer to — but it is not strictly stronger here:
//     the Origin comparison is exact-host equality, so a sibling-subdomain
//     post (evil.media.example against media.example) is refused either way.
//
// A request carrying neither header did not come from a browser, and a
// non-browser client has no ambient cookie jar to abuse. Those requests are
// let through because POST /auth/login and POST /auth/register are the only
// way a machine client can obtain a JWT (there is no token endpoint under
// /api/v1, and minting an API key already requires one), so refusing them
// would leave curl, mobile and CLI clients no way in at all.
//
// Note this guard does not cover every session-minting path: GET
// /auth/oidc/{name}/callback also calls auth.SetSession. That is deliberate —
// an IdP redirect is inherently cross-site, and it carries its own state,
// nonce and PKCE cookies instead.
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if httputil.CrossSiteRequest(r) {
			slog.InfoContext(r.Context(), "cross-site auth request rejected",
				"sec_fetch_site", r.Header.Get("Sec-Fetch-Site"),
				"origin", r.Header.Values("Origin"))
			writeError(w, r, http.StatusForbidden,
				"Cross-site request blocked", "cross_site_blocked")
			return
		}
		if !jsonContentType(r) {
			writeError(w, r, http.StatusUnsupportedMediaType,
				"Content-Type must be application/json", "unsupported_media_type")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// jsonContentType demands a JSON body, which no cross-site HTML form can
// produce — forms are limited to urlencoded, multipart and text/plain. A
// request with neither body nor Content-Type (the SPA's logout and Plex-PIN
// POSTs) passes: a form always sends both, so nothing is let through by the
// exemption.
func jsonContentType(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return r.ContentLength == 0
	}
	mt, _, err := mime.ParseMediaType(ct)
	return err == nil && mt == "application/json"
}
