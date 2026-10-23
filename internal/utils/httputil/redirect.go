package httputil

import (
	"net/url"
	"strings"
)

// SafeNextPath validates a return-to path taken from untrusted input and
// returns the form safe to hand to http.Redirect, or ok=false when the value
// cannot be made safe. Callers supply their own landing page for the false
// case, since what "somewhere harmless" means differs per entry point.
//
// Every check runs on the DECODED path, and the value returned is re-encoded
// from it. Browsers fold "\" into "/", so "/\evil.com" passes a naive
// rooted-path check and lands as protocol-relative //evil.com; "/%2f%2fevil.com"
// is that trick spelled encoded. Testing the raw string instead would miss
// "/%5Cevil.com", which is inert in a Location header but not on the way to the
// SPA: it reads next through URLSearchParams, which percent-decodes, so
// window.location.assign receives the folding form after all. Measured — that
// value resolves to http://evil.example/. url.Parse also rejects the control
// characters browsers strip from a URL, which would otherwise collapse
// "/<TAB>/evil.com" the same way.
//
// /auth/* is refused so a stale next=/auth/oidc/<name>/start cannot re-launch
// the SSO flow immediately after a local login.
func SafeNextPath(n string) (string, bool) {
	u, err := url.Parse(n)
	if err != nil || u.Scheme != "" || u.Opaque != "" || u.Host != "" ||
		u.User != nil {
		return "", false
	}
	if strings.ContainsRune(u.Path, '\\') ||
		!strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") ||
		strings.HasPrefix(u.Path, "/auth/") {
		return "", false
	}
	next := u.EscapedPath()
	if u.RawQuery != "" {
		next += "?" + u.RawQuery
	}
	return next, true
}
