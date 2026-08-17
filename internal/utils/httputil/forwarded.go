package httputil

import (
	"net/http"
	"strings"
)

// ServedOverTLS reports whether the browser actually reached us over https,
// reading only signals that describe *this* request's transport.
//
// It deliberately does not consult a configured public URL. That fallback is
// right for the session cookie, where the failure it prevents is emitting a JWT
// without Secure, so guessing "secure" is the safe error, and auth.isSecure
// layers it on top of this. HSTS biases the other way: emitted once over https
// it pins the host for a year, and a LAN client reaching the same install over
// plain http would be stranded. A year-long pin must not come from
// configuration, so the fallback stays out of here.
func ServedOverTLS(r *http.Request) bool {
	return r.TLS != nil || ForwardedProto(r) == "https"
}

// ForwardedProto reads the browser-facing scheme a reverse proxy reported,
// lowercased, or "" when it reported none or the peer is not a configured
// proxy. Every header read here is free-form client input off one, so the trust
// gate is part of the function rather than the caller's to remember.
//
// Several spellings all mean https and all reach us in the wild, so an exact
// match on X-Forwarded-Proto: https quietly disarms both HSTS and the session
// cookie's Secure flag on an https-only install. Traefik and HAProxy can emit
// RFC 7239 Forwarded instead; Apache mod_ssl deployments and several appliances
// send X-Forwarded-Ssl: on; the scheme token is case-insensitive, so "HTTPS" is
// legal; and a proxy that appends rather than replaces leaves a chain like
// "https,http".
//
// Order is first-signal-wins, not any-signal-wins: RFC 7239 is the standard and
// settles it alone, even when it says http and a stale X-Forwarded-* left by an
// earlier hop disagrees. "Any header that says https" would instead let the
// *most* stale signal arm a year-long pin.
func ForwardedProto(r *http.Request) string {
	if !TrustedPeer(r) {
		return ""
	}
	if proto := forwardedHeaderProto(r.Header.Get("Forwarded")); proto != "" {
		return proto
	}
	if xfp := r.Header.Get("X-Forwarded-Proto"); xfp != "" {
		return firstHop(xfp)
	}
	if strings.EqualFold(
		strings.TrimSpace(r.Header.Get("X-Forwarded-Ssl")), "on",
	) {
		return "https"
	}
	return ""
}

// forwardedHeaderProto pulls proto out of the first element of an RFC 7239
// Forwarded header. Parameter names are case-insensitive and the value may be
// a quoted string.
func forwardedHeaderProto(header string) string {
	element, _, _ := strings.Cut(header, ",")
	for param := range strings.SplitSeq(element, ";") {
		name, value, ok := strings.Cut(param, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "proto") {
			continue
		}
		return strings.ToLower(
			strings.Trim(strings.TrimSpace(value), `"`),
		)
	}
	return ""
}

// firstHop returns the leftmost entry of a comma-separated forwarded chain,
// lowercased. Left is the hop nearest the browser — the same convention the
// X-Forwarded-For walk above reads the chain by — and the browser's own scheme
// is the one callers are asking about. That walk cannot be reused: it
// identifies the client by testing each entry against server.trusted_proxies,
// and a bare scheme token carries no address to test.
//
// Behind a proxy that appends rather than replaces, the leftmost entry is
// whatever the client sent. That does not widen the surface: both consumers
// only ever decide something about the requesting browser's own response, so
// forging the header affects nobody but the forger. The header a client cannot
// forge is the one on some *other* user's response, and nothing here reads
// across requests.
func firstHop(chain string) string {
	head, _, _ := strings.Cut(chain, ",")
	return strings.ToLower(strings.TrimSpace(head))
}
