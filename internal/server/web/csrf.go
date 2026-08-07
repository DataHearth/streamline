package web

import (
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/datahearth/streamline/internal/config"
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
//     never sees Sec-Fetch-Site. originMatchesServedHost below is the entire
//     defence there, not a legacy fallback, and removing it would leave these
//     deployments with none.
//   - https, or http://localhost — a reverse-proxied install, or a browser on
//     the host itself — gets Sec-Fetch-Site too and settles on it before
//     Origin is consulted. It reports the initiator relationship the browser
//     computed rather than a value we compare ourselves, so it needs no notion
//     of which hosts we answer to — but it is not strictly stronger here:
//     originMatchesServedHost is exact-host equality, so a sibling-subdomain
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
		if crossSiteRequest(r) {
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

// crossSiteRequest reports whether a browser told us this request came from
// somewhere other than this exact origin.
func crossSiteRequest(r *http.Request) bool {
	site := r.Header.Get("Sec-Fetch-Site")
	if site == "same-origin" {
		// Settled without consulting Origin, deliberately. Demanding the two
		// agree looks like strictly more checking but refuses every proxied
		// install that leaves STREAMLINE_PUBLIC_URL unset: the browser reports
		// same-origin for the hostname the user typed, while servedHosts sees
		// only the internal name the proxy rewrote r.Host to, so the Origin
		// comparison disagrees on exactly the deployments this branch serves.
		return false
	}
	if site != "" {
		// "cross-site"; "none", which no POST a browser initiates can carry;
		// and "same-site", refused because a sibling subdomain posting here is
		// the deployment shape that makes SameSite=Lax insufficient to begin
		// with.
		return true
	}
	// No Sec-Fetch-Site: either a browser on a plain-http deployment, which
	// still sends Origin, or a non-browser client, which sends neither and
	// cannot be steered by an attacker's page.
	origins := r.Header.Values("Origin")
	switch len(origins) {
	case 0:
		return false
	case 1:
		return !originMatchesServedHost(r, origins[0])
	default:
		// A browser sends Origin exactly once. Several means something ahead of
		// us smuggled one in, and picking any of them decides the request on an
		// attacker's terms — Header.Get would silently take the first.
		return true
	}
}

// originMatchesServedHost compares an Origin against the hosts this deployment
// answers to. Only the host is compared, never the scheme: matching the scheme
// too would reject every deployment behind a TLS-terminating proxy, which sees
// plain http on the inside.
func originMatchesServedHost(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	// An opaque origin serialises to "null", which parses with an empty host,
	// so it is refused here. Sandboxed iframes are the obvious source, but not
	// the common one: Chrome's default referrer policy also sends "null" when
	// an https initiator posts to a non-https target — i.e. an https attacker
	// page against a plain-http LAN install, which is this app's likeliest
	// real attack shape.
	if err != nil || u.Host == "" {
		return false
	}
	// An origin serialises as scheme "://" host [ ":" port ] and nothing else.
	// url.Parse is far more permissive, and each part it accepts beyond that
	// shape still reports our host in u.Host while reading to a human as some
	// other name: "https://evil.example@streamline.example" as the attacker's,
	// "//streamline.example" and "foo://streamline.example" as neither. Origin
	// is browser-generated, so nothing legitimate is turned away by demanding
	// the exact shape — and this comparison stays as exact as it claims.
	if (u.Scheme != "http" && u.Scheme != "https") ||
		u.User != nil || u.Opaque != "" ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	want := canonicalHost(u.Host, u.Scheme)
	for _, h := range servedHosts(r) {
		if h != "" && canonicalHost(h, u.Scheme) == want {
			return true
		}
	}
	return false
}

// servedHosts lists every host this deployment legitimately answers to. Both
// sources are needed: r.Host is the authority the browser was pointed at, and
// is the only one a bare install has, while PublicURL is the bind address in
// that same bare install but the operator's real hostname whenever
// STREAMLINE_PUBLIC_URL is set — the one source that survives a proxy which
// rewrites Host to an internal name.
//
// X-Forwarded-Host is deliberately not among them, though it would cover that
// last case without STREAMLINE_PUBLIC_URL. Nothing authenticates the peer that
// set it, so honouring it lets any caller nominate its own Origin as a served
// host and walk through this guard; the Origin comparison it feeds is the only
// defence a plain-http deployment has. Reinstating it needs a trusted-proxy
// check in front, not a bare header read.
func servedHosts(r *http.Request) []string {
	pub, err := url.Parse(config.PublicURL())
	if err != nil {
		pub = &url.URL{}
	}
	return []string{r.Host, namedHost(pub.Host)}
}

// namedHost drops a host that is only a bind address. PublicURL falls back to
// server.host, which defaults to the unspecified address, and 0.0.0.0 names no
// deployment — keeping it would answer to an Origin no operator configured. A
// browser genuinely pointed at http://0.0.0.0:<port> matches on r.Host anyway.
func namedHost(host string) string {
	h := host
	if hostOnly, _, err := net.SplitHostPort(host); err == nil {
		h = hostOnly
	}
	if addr, err := netip.ParseAddr(h); err == nil && addr.IsUnspecified() {
		return ""
	}
	return host
}

// canonicalHost lowercases a host and drops the port when it is the default
// for scheme. Origin never spells out a default port, while r.Host routinely
// does, so without this "media.example" and "media.example:443" would not
// compare equal over https.
func canonicalHost(host, scheme string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	switch strings.ToLower(scheme) {
	case "https":
		return strings.TrimSuffix(host, ":443")
	case "http":
		return strings.TrimSuffix(host, ":80")
	}
	return host
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
