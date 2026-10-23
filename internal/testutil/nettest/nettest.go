// Package nettest answers what the host's network stack can do, for code that
// builds plain anacrolix clients outside the engine — the e2e fake tracker and
// the bittorrent specs. The engine itself binds one dual-stack socket of its
// own and needs none of this; a default anacrolix client listens on tcp4,
// tcp6, udp4 and udp6 separately and fails outright when one family is
// missing ("listen tcp6 …: address family not supported by protocol"), which
// is the state of many containers and CI runners.
package nettest

import (
	"errors"
	"net"
	"sync"
)

// IPv6 reports whether this host can open IPv6 sockets, which is the question
// anacrolix's listeners ask: a TCP listener and a UDP socket on the
// unspecified IPv6 address, port 0. Answered once per process.
var IPv6 = sync.OnceValue(func() bool {
	ln, err := net.Listen("tcp6", "[::]:0")
	if err != nil {
		return false
	}
	_ = ln.Close()
	pc, err := net.ListenPacket("udp6", "[::]:0")
	if err != nil {
		return false
	}
	_ = pc.Close()
	return true
})

// ErrNoRoutableAddress means the host has no IPv4 address a peer could be
// pointed at: nothing but loopback and link-local.
var ErrNoRoutableAddress = errors.New(
	"no non-loopback, non-link-local IPv4 address on this host",
)

// AdvertiseIP returns an IPv4 address of this host that is neither loopback
// nor link-local — the ranges the builtin engine refuses to announce to — so
// a tracker reachable on it is one the engine will talk to. It prefers the
// address the default route leaves from (a UDP "connect" sends nothing; it
// only asks the kernel which source it would use) and falls back to the first
// qualifying interface address, so it works on Linux, macOS and Windows alike
// without `ip`, `ifconfig` or `ipconfig`.
func AdvertiseIP() (net.IP, error) {
	if c, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		addr, ok := c.LocalAddr().(*net.UDPAddr)
		_ = c.Close()
		if ok && routable(addr.IP) {
			return addr.IP.To4(), nil
		}
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && routable(n.IP) {
			return n.IP.To4(), nil
		}
	}
	return nil, ErrNoRoutableAddress
}

func routable(ip net.IP) bool {
	return ip.To4() != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() &&
		!ip.IsUnspecified() && !ip.IsMulticast()
}
