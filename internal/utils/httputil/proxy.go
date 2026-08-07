package httputil

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/datahearth/streamline/internal/config"
)

type clientIPCtxKeyType struct{}

var clientIPCtxKey clientIPCtxKeyType

const xffHeader = "X-Forwarded-For"

// TrustedPeer reports whether the request's immediate TCP peer is one of the
// reverse proxies listed in server.trusted_proxies.
//
// This is the single gate for believing any X-Forwarded-* header. On a direct
// connection — a port-forward, a LAN client, anyone who reaches the listener
// without going through the proxy — those headers are entirely client-supplied
// and must be ignored.
func TrustedPeer(r *http.Request) bool {
	prefixes := trustedProxies()
	if len(prefixes) == 0 {
		return false
	}
	peer, ok := parseAddr(peerHost(r))
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
// every trusted-proxy hop skipped, so the result is the address the outermost
// of our own proxies actually saw — the last entry no attacker could forge.
// For every other peer the header is ignored and the TCP address wins.
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
	peer, peerOK := parseAddr(peerHost(r))
	prefixes := trustedProxies()
	if peerOK && len(prefixes) > 0 && inAny(peer, prefixes) {
		if ip, ok := clientFromXFF(r.Header.Values(xffHeader), prefixes); ok {
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
// An entry that does not parse aborts the walk with no result: everything to
// its left was relayed by whoever wrote the garbage and cannot be trusted.
func clientFromXFF(headers []string, trusted []netip.Prefix) (netip.Addr, bool) {
	for _, header := range slices.Backward(headers) {
		entries := strings.Split(header, ",")
		for _, entry := range slices.Backward(entries) {
			raw := strings.TrimSpace(entry)
			if raw == "" {
				continue
			}
			ip, ok := parseAddr(raw)
			if !ok {
				return netip.Addr{}, false
			}
			if inAny(ip, trusted) {
				continue
			}
			return ip, true
		}
	}
	return netip.Addr{}, false
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

func peerHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// parseAddr normalises for comparison: a v4-mapped v6 address folds to plain
// v4 and the IPv6 zone is dropped, because netip.Prefix.Contains rejects both
// forms outright — without folding, either notation would alias a trusted
// address past the prefix check.
func parseAddr(s string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(s)
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
