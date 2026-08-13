// Package httputil holds HTTP-related helpers shared across the webui.
package httputil

import (
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"
)

// RetryAfterSeconds renders a rate-limiter wait for the Retry-After header. It
// rounds up and floors at one second: anything under a second would render as
// "0", which reads as "retry now" and invites a tight loop against an endpoint
// that is refusing.
func RetryAfterSeconds(d time.Duration) string {
	secs := int64(1)
	if d > time.Second {
		secs = int64(math.Ceil(d.Seconds()))
	}
	return strconv.FormatInt(secs, 10)
}

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
