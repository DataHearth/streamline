package httputil

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/datahearth/streamline/internal/config"
)

type clientIPCtxKeyType struct{}

var clientIPCtxKey clientIPCtxKeyType

const xffHeader = "X-Forwarded-For"

// TrustedPeer reports whether the request's immediate TCP peer is one of the
// reverse proxies listed in server.trusted_proxies.
//
// This is the gate to consult before believing any X-Forwarded-* header. On a
// direct connection — a port-forward, a LAN client, anyone who reaches the
// listener without going through the proxy — those headers are entirely
// client-supplied and must be ignored. It does not yet cover every reader:
// the session cookie's Secure flag (internal/auth/cookie.go) and the OIDC
// redirect URI (internal/server/web/auth.go) still read X-Forwarded-Proto and
// X-Forwarded-Host without passing through here.
func TrustedPeer(r *http.Request) bool {
	prefixes := trustedProxies()
	if len(prefixes) == 0 {
		return false
	}
	peer, ok := parseAddr(r.RemoteAddr)
	if !ok {
		return false
	}
	return inAny(peer, prefixes)
}

// ClientIPResolver returns middleware that resolves the client IP once per
// request and stores it for the rest of the chain; read it back with ClientIP
// or ClientIPString.
//
// For a trusted peer the X-Forwarded-For chain is walked right to left with
// trusted-proxy hops skipped, so the result is the address the outermost of our
// own proxies actually saw — the last entry no attacker could forge. For every
// other peer the header is ignored and the TCP address wins.
//
// Must be registered before anything that logs, rate-limits or authorises by
// IP: a forged value would otherwise inherit that IP's trusted-network
// privileges and its rate-limit budget.
func ClientIPResolver() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ip, ok := resolveClientIP(r); ok {
				r = r.WithContext(
					context.WithValue(r.Context(), clientIPCtxKey, ip),
				)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func resolveClientIP(r *http.Request) (netip.Addr, bool) {
	peer, peerOK := parseAddr(r.RemoteAddr)
	prefixes := trustedProxies()
	if peerOK && len(prefixes) > 0 && inAny(peer, prefixes) {
		ip, ok := clientFromXFF(
			r.Context(), r.Header.Values(xffHeader), prefixes,
		)
		if ok {
			return ip, true
		}
	}
	return peer, peerOK
}

// clientFromXFF walks the merged X-Forwarded-For chain right to left, skipping
// hops that are themselves trusted proxies, and returns the first remaining
// entry. Multiple headers are merged in the order received (RFC 2616) so a
// duplicate header cannot be used to pick which value is read.
//
// It gives up with no result — leaving the peer as the client — on an entry
// that does not parse, since everything to its left was relayed by whoever
// wrote the garbage.
//
// The number of hops it will skip is deliberately not bounded. A bound protects
// nothing: skipping stops at the first entry outside server.trusted_proxies, so
// a caller who can forge entries at all forges the one that matters with no
// padding, and a caller who cannot gains nothing from a longer chain. What a
// bound does do is truncate legitimately deep chains — CDN, WAF, load balancer,
// ingress, sidecar — onto the peer, collapsing every client behind them into
// one identity for logging, rate limiting and trusted-network auth.
func clientFromXFF(
	ctx context.Context, headers []string, trusted []netip.Prefix,
) (netip.Addr, bool) {
	for _, header := range slices.Backward(headers) {
		entries := strings.Split(header, ",")
		for _, entry := range slices.Backward(entries) {
			raw := strings.TrimSpace(entry)
			if raw == "" {
				continue
			}
			ip, ok := parseAddr(raw)
			if !ok {
				warnUnreadableXFF(ctx, raw)
				return netip.Addr{}, false
			}
			if !inAny(ip, trusted) {
				return ip, true
			}
		}
	}
	return netip.Addr{}, false
}

const (
	// unreadableXFFInterval throttles the unreadable-chain warning. A proxy
	// emitting a form we cannot read emits it on every request, so this is a
	// configuration signal rather than per-request news — but whoever wrote the
	// unreadable entry is the client whenever the peer is trusted, so a
	// once-per-process warning hands an attacker a way to spend the operator's
	// only signal on one crafted request and silence a genuinely broken proxy
	// for the life of the process. Repeating on an interval keeps the log
	// bounded and the signal alive.
	unreadableXFFInterval = time.Minute

	// maxLoggedXFFEntry truncates the offending entry. It is attacker-supplied
	// and bounded only by the server's header limit.
	maxLoggedXFFEntry = 64
)

// lastUnreadableXFF is the UnixNano of the last warning, zero before the first.
var lastUnreadableXFF atomic.Int64

func warnUnreadableXFF(ctx context.Context, entry string) {
	now := time.Now().UnixNano()
	last := lastUnreadableXFF.Load()
	if now-last < int64(unreadableXFFInterval) {
		return
	}
	if !lastUnreadableXFF.CompareAndSwap(last, now) {
		return
	}
	if len(entry) > maxLoggedXFFEntry {
		entry = strings.ToValidUTF8(entry[:maxLoggedXFFEntry], "") + "…"
	}
	slog.WarnContext(ctx,
		"Ignoring an X-Forwarded-For chain with an unreadable entry; "+
			"the connecting proxy is being logged and rate-limited as the client",
		"entry", entry,
	)
}

func trustedProxies() []netip.Prefix {
	cfg := config.Get()
	if cfg == nil {
		return nil
	}
	prefixes := make([]netip.Prefix, 0, len(cfg.Server.TrustedProxies))
	for _, cidr := range cfg.Server.TrustedProxies {
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			continue
		}
		prefixes = append(prefixes, p)
	}
	return prefixes
}

// parseAddr reads one address in any of the shapes it arrives in: RemoteAddr
// always carries a port, and an X-Forwarded-For entry may be bare, bracketed
// IPv6, or carry the source port, which some proxies append. Rejecting the
// latter two would abort the whole walk and silently promote the proxy to
// client.
//
// It also normalises for comparison: a v4-mapped v6 address folds to plain v4
// and the IPv6 zone is dropped, because netip.Prefix.Contains rejects both
// forms outright — without folding, either notation would alias a trusted
// address past the prefix check.
func parseAddr(s string) (netip.Addr, bool) {
	raw := s
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	} else {
		raw = strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]")
	}
	ip, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap().WithZone(""), true
}

func inAny(ip netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
