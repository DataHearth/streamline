// Package httputil holds HTTP-related helpers shared across the webui.
package httputil

import (
	"net"
	"net/http"
	"net/netip"
)

// ClientIP returns the client IP recorded by ClientIPResolver, falling back to
// the connecting RemoteAddr when the resolver did not run.
func ClientIP(r *http.Request) net.IP {
	if ip, ok := r.Context().Value(clientIPCtxKey).(netip.Addr); ok {
		return net.IP(ip.AsSlice())
	}
	if ip, ok := parseAddr(r.RemoteAddr); ok {
		return net.IP(ip.AsSlice())
	}
	return nil
}

// ClientIPString returns the client IP as a string, or the raw RemoteAddr when
// it cannot be parsed.
func ClientIPString(r *http.Request) string {
	ip := ClientIP(r)
	if ip == nil {
		return r.RemoteAddr
	}
	return ip.String()
}
