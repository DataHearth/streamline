package middleware

import (
	"net/http"
	"strings"

	"github.com/datahearth/streamline/internal/utils/httputil"
)

// metadataImageCDNs are the two artwork hosts internal/metadata hardcodes into
// the poster, backdrop and profile URLs the REST API hands back —
// image.tmdb.org for TMDB, artworks.thetvdb.com for TVDB. Nothing else the SPA
// renders is loaded off-origin, so this is the whole of the img-src relaxation.
//
// Note this is not the /posters proxy, which is same-origin and needs no
// allowance: these are the raw provider URLs the lookup and detail payloads
// carry, which the SPA puts into <img src> unchanged.
const metadataImageCDNs = "https://image.tmdb.org https://artworks.thetvdb.com"

// securityCSP is one policy for the whole app — the SPA shell, the Scalar
// docs page at /api/docs, the poster proxy and the JSON API alike. Measured
// against both bundles in Chrome 147: the docs page needs no relaxation the
// SPA does not already need, so a second policy would buy nothing.
//
// 'unsafe-inline' is confined to style-src, and is unavoidable rather than
// lazy. web/app/index.html carries style="color-scheme: dark", Svelte applies
// further style attributes at runtime (Hero's grid-area, AddButton's --din /
// --dout), and Scalar injects its whole stylesheet from JS. Style *attributes*
// are exempt from hashing unless 'unsafe-hashes' is also set, and the Svelte
// values are computed per element, so no fixed hash set can cover them.
//
// script-src stays 'self'. Do not add 'unsafe-eval' to make a console message
// go away — that message is the policy working. The SPA bundle contains no
// eval and no Function constructor. The Scalar docs bundle contains two, and
// only one of them ever runs:
//
//   - zod v4's memoized JIT-capability probe (util.allowsEval, compiled to
//     `try{let e=Function;return new e(""),!0}catch{return!1}`), which decides
//     whether its object validators may be code-generated. It is the only one
//     that can run, and it is wrapped: refused, it caches false and the page
//     carries on.
//
//     When it runs is zod's business, not this policy's. Re-measured in
//     headless Chrome 147 against this bundle — loading /api/docs, opening the
//     search palette, walking the endpoint list — it never ran: zero eval
//     violations, zero console errors. So a clean console here is not evidence
//     the policy is unenforced; on the same page an induced
//     setTimeout("…") is blocked and logged, as are an off-origin <img> and
//     <script>.
//
//   - zod's Doc.compile (`let t=Function; … return new t(...)`), which is NOT
//     wrapped in a try/catch and would throw outright.
//
// The second is unreachable whichever way the first goes, because zod gates the
// code-generated parser on the probe's cached answer and under this policy that
// answer cannot be true: unrun or refused, Doc.compile is never entered and the
// interpreted parse path runs instead — identical validation results down a path
// nobody can perceive on a docs page. That gating is the whole reason a blocked
// eval would be harmless here, so do not "fix" a console line by granting
// 'unsafe-eval': it would hand the entire app the one primitive this policy
// exists to deny, and it would also switch on an unguarded Function call that
// currently never executes.
//
// img-src carries metadataImageCDNs and font-src carries nothing, and the two
// decisions only look inconsistent. What separates them is whether the browser
// was already talking to the third party before the policy existed.
//
// The SPA binds TMDB and TVDB artwork URLs straight into <img src> — the
// add-movie and add-series pickers, the lookup detail panel, similar titles,
// recommendations, cast portraits and the request decision sheet all render
// remote artwork. Those fetches happen today with or without a CSP, so
// allowlisting the two hosts changes nothing about what leaks; it only stops
// the policy from breaking browse and add outright. And it fails *silently*:
// the SPA's onerror fallback swaps in a placeholder, so a blocked policy shows
// empty film-strip tiles with no error surfaced anywhere but the console.
//
// font-src deliberately omits https://fonts.scalar.com, and that is the
// opposite trade: nothing was talking to it before, and a self-hosted install
// should not announce every visit to its API docs to a third party. There is
// no cost to pay here either, because the request is not merely blocked — it
// is not made. Scalar's default theme declares Inter and JetBrains Mono over
// fourteen woff2 subsets on that CDN, so web/static/js/docs.js turns the
// default font block off (withDefaultFonts: false) and re-declares both
// families over the copies web/static/fonts already ships for the SPA. Same
// faces, same origin, and fourteen fewer console errors to read past when a
// real violation turns up.
//
// That is true of requests, not of bytes: the string fonts.scalar.com still
// appears fourteen times in docs.min.js, in the default theme's stylesheet that
// withDefaultFonts switches off. Nothing injects it, so nothing fetches it — but
// grepping the bundle for the host will find it, and that is not a regression.
//
// So: do not "tighten" img-src back to 'self'. It does not harden anything the
// SPA was not already doing, and it silently guts the add-media flow.
const securityCSP = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' " + metadataImageCDNs + "; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"frame-ancestors 'none'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'"

// One year, without includeSubDomains or preload. This is a self-hosted app
// that commonly shares a parent domain with hosts it knows nothing about;
// pinning those to https on its behalf is not its call to make.
const securityHSTS = "max-age=31536000"

// securityCOOP is "same-origin-allow-popups" rather than "same-origin". The
// Plex PIN flow (web/app/lib/plex_pin.ts) opens https://app.plex.tv/auth with
// window.open and keeps the returned handle: it checks `if (!popup)` for
// popup-blocker detection and later calls closePopup(popup) to close it.
// Plain "same-origin" severs that handle the instant the popup navigates
// cross-origin, breaking both. same-origin-allow-popups still isolates this
// origin's window from a popup's window.opener while leaving the opener's own
// handle to the popup intact.
const securityCOOP = "same-origin-allow-popups"

// SecurityHeaders stamps the browser-facing hardening headers onto every
// response.
//
// Referrer-Policy is "same-origin" rather than the stricter "no-referrer".
// no-referrer does more than drop the Referer: per Fetch's "append a request
// Origin header", it also degrades Origin to null on same-origin requests whose
// mode is not cors. Measured against this app — a same-origin form POST to
// /auth/login from a plain-http LAN origin — that arrives as origin=[null] and
// csrfGuard refuses it 403, because no Sec-Fetch-Site is emitted off a
// potentially-trustworthy origin and Origin is the entire defence there. Under
// same-origin the identical POST passes the guard. Today's SPA only ever fetches
// with the default cors mode, so no-referrer would not break it yet; it would
// leave a trap that springs on the first form post or sendBeacon, and only on
// plain-http installs. same-origin still fixes what the leak was about: a
// cross-origin request gets no Referer at all, so /register?token=<invite>
// cannot escape, and the instance hostname stays off the TMDB/TVDB/IMDb links
// the UI renders.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", securityCSP)
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", securityCOOP)
		// Default every response to no-store rather than opting each handler
		// out individually: /api/v1 and the webui auth routes hand back
		// bearer material (API keys, invite tokens, the Plex PIN's
		// auth_token, the rotated JWT secret) and a missed spot would cache
		// it silently. /static/* is the one exclusion — the embedded
		// JS/CSS/fonts carry no secrets and need to stay cacheable.
		if !strings.HasPrefix(r.URL.Path, "/static/") {
			h.Set("Cache-Control", "no-store, private")
		}
		if servedOverTLS(r) {
			h.Set("Strict-Transport-Security", securityHSTS)
		}
		next.ServeHTTP(w, r)
	})
}

// servedOverTLS reports whether the browser actually reached us over https.
//
// Deliberately stricter than auth.isSecure, which also accepts a configured
// https public_url. That fallback is right for the session cookie, where the
// failure it prevents is emitting a JWT without Secure — so guessing "secure"
// is the safe error. HSTS biases the other way: emitted once over https it
// pins the host for a year, and a LAN client reaching the same install over
// plain http would be stranded. Only signals that describe *this* request's
// transport count.
//
// It is also more liberal in one direction: auth.isSecure still exact-matches
// X-Forwarded-Proto == "https", so behind a proxy that reports the scheme any
// other way this arms HSTS while the session cookie goes out without Secure.
// That gap is auth's to close, and closing it there is strictly a hardening.
func servedOverTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return httputil.TrustedPeer(r) && forwardedProto(r) == "https"
}

// forwardedProto reads the browser-facing scheme a reverse proxy reported,
// lowercased, or "" when it reported none. The caller must have established
// that the peer is trusted: every header read here is free-form client input
// off one.
//
// Several spellings all mean https and all reach us in the wild, so an exact
// match on X-Forwarded-Proto: https quietly disarms HSTS on an https-only
// install. Traefik and HAProxy can emit RFC 7239 Forwarded instead; Apache
// mod_ssl deployments and several appliances send X-Forwarded-Ssl: on; the
// scheme token is case-insensitive, so "HTTPS" is legal; and a proxy that
// appends rather than replaces leaves a chain like "https,http".
//
// Order is first-signal-wins, not any-signal-wins: RFC 7239 is the standard and
// settles it alone, even when it says http and a stale X-Forwarded-* left by an
// earlier hop disagrees. "Any header that says https" would instead let the
// *most* stale signal arm a year-long pin.
func forwardedProto(r *http.Request) string {
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
// X-Forwarded-For walk in httputil reads the chain by — and the browser's own
// scheme is the one HSTS is about. That walk cannot be reused: it identifies
// the client by testing each entry against server.trusted_proxies, and a bare
// scheme token carries no address to test.
//
// Behind a proxy that appends rather than replaces, the leftmost entry is
// whatever the client sent. That does not widen the attack surface HSTS has:
// the header only ever pins the requesting browser, so forging it pins nobody
// but the forger. The header a client cannot forge is the one on some *other*
// user's response, and nothing here reads across requests.
func firstHop(chain string) string {
	head, _, _ := strings.Cut(chain, ",")
	return strings.ToLower(strings.TrimSpace(head))
}
