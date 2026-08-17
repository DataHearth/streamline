package restapi

import (
	"context"
	"log/slog"
	"net"
	"net/url"
)

const draftTargetRefusedMessage = "connection tests may not target a link-local address"

// draftTargetRefused reports whether a draft connection test must not be
// attempted against target, a host or base URL taken straight from the request
// body.
//
// The draft-test endpoints turn a caller-supplied address into an outbound
// request, so they are an SSRF primitive by construction. Only link-local is
// refused (169.254.0.0/16, fe80::/10), which on a cloud host is the instance
// metadata service and the credentials it hands out. Loopback and the RFC1918
// ranges stay reachable deliberately: a self-hosted deployment's qBittorrent,
// Prowlarr and Plex live exactly there, so refusing them would break the only
// legitimate use these endpoints have.
//
// The decision is made on the resolved addresses, not on the spelling of
// target, so a hostname pointing at the metadata service is caught as well.
// It does not close DNS rebinding: the request itself is issued afterwards by
// the service layer's own HTTP client, which resolves the host again.
//
// A resolution failure is not a refusal — the service call that follows renders
// it as the endpoint's canonical unreachable answer.
func draftTargetRefused(ctx context.Context, target string) bool {
	host := target
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		host = u.Hostname()
	}

	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		addr = addr.Unmap()
		if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
			slog.WarnContext(
				ctx,
				"refused a connection test to a link-local address",
				"host",
				host,
				"address",
				addr.String(),
			)
			return true
		}
	}
	return false
}
