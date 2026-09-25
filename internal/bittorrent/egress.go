package bittorrent

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

// errInternalEgress refuses a tracker, webseed or metainfo-source address on
// the host itself or its link. Those URLs come from the release — a member's
// magnet, or a .torrent an indexer served — so without this the engine dials
// whatever they name: a blind request to a service on loopback, or to the
// cloud metadata endpoint on link-local. Private ranges stay reachable: a
// homelab tracker or webseed normally lives on one, as do the indexers.
// Peer connections are not filtered; a peer on the LAN is an ordinary peer.
var errInternalEgress = errors.New(
	"refusing to reach a loopback, link-local or multicast address",
)

func refuseInternalIP(ip net.IP) error {
	if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("%w: %s", errInternalEgress, ip)
	}
	return nil
}

// refuseInternalDial is a net.Dialer Control hook: it runs on the address the
// dial actually connects to, after name resolution, so a hostname resolving
// to loopback is refused as surely as a literal.
func refuseInternalDial(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	return refuseInternalIP(net.ParseIP(host))
}

// egressPacketConn applies the same rule to the UDP tracker socket, which
// sends with WriteTo rather than dialling.
type egressPacketConn struct {
	net.PacketConn
}

func (c egressPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return 0, err
	}
	if err := refuseInternalIP(net.ParseIP(host)); err != nil {
		return 0, err
	}
	return c.PacketConn.WriteTo(p, addr)
}
