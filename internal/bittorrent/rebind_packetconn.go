package bittorrent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

// rebindablePacketConn is the net.PacketConn the engine hands anacrolix via
// ClientConfig.ListenPacket, so uTP and DHT run over a socket the engine can
// move to another port without rebuilding the torrent client.
//
// ReadFrom deliberately hides a rebind from its caller: utp.Socket reads this
// conn in a loop and treats an error as a dead socket, tearing down every uTP
// connection and never recovering. A port change must not look like that, so a
// read woken by the retirement of its socket retries on the replacement.
type rebindablePacketConn struct {
	ip net.IP

	mu     sync.RWMutex
	conn   *net.UDPConn
	closed bool
}

// newPeerSockets (engine.go) passes entry.ListenPort here, and only falls
// back to 0 — let the OS pick — when that is itself unconfigured; the TCP
// listener then binds to whatever port this call resolved to.
//
//nolint:unparam // port is part of the constructor's public shape, not dead
func newRebindablePacketConn(ip net.IP, port uint16) (*rebindablePacketConn, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip, Port: int(port)})
	if err != nil {
		return nil, fmt.Errorf("listen udp: %w", err)
	}
	return &rebindablePacketConn{ip: ip, conn: conn}, nil
}

// rebind moves the socket to port. The replacement is bound before the old
// socket is retired, so a failure leaves the engine reachable on the port it
// already had, and the swap happens before the close so a reader woken by that
// close can see the replacement.
func (c *rebindablePacketConn) rebind(port uint16) error {
	next, err := net.ListenUDP("udp", &net.UDPAddr{IP: c.ip, Port: int(port)})
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		if cerr := next.Close(); cerr != nil {
			slog.WarnContext(
				context.Background(),
				"closing unused packet conn failed",
				"error",
				cerr,
			)
		}
		return net.ErrClosed
	}
	prev := c.conn
	c.conn = next
	c.mu.Unlock()

	if err := prev.Close(); err != nil {
		slog.WarnContext(
			context.Background(),
			"closing retired packet conn failed",
			"error",
			err,
		)
	}
	return nil
}

func (c *rebindablePacketConn) current() (*net.UDPConn, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn, c.closed
}

func (c *rebindablePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		conn, closed := c.current()
		if closed {
			return 0, nil, net.ErrClosed
		}
		n, addr, err := conn.ReadFrom(p)
		if err == nil || !errors.Is(err, net.ErrClosed) {
			return n, addr, err
		}
		if next, closed := c.current(); closed || next == conn {
			return n, addr, err
		}
	}
}

func (c *rebindablePacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	conn, closed := c.current()
	if closed {
		return 0, net.ErrClosed
	}
	return conn.WriteTo(p, addr)
}

func (c *rebindablePacketConn) LocalAddr() net.Addr {
	conn, _ := c.current()
	return conn.LocalAddr()
}

func (c *rebindablePacketConn) SetDeadline(t time.Time) error {
	conn, closed := c.current()
	if closed {
		return net.ErrClosed
	}
	return conn.SetDeadline(t)
}

func (c *rebindablePacketConn) SetReadDeadline(t time.Time) error {
	conn, closed := c.current()
	if closed {
		return net.ErrClosed
	}
	return conn.SetReadDeadline(t)
}

func (c *rebindablePacketConn) SetWriteDeadline(t time.Time) error {
	conn, closed := c.current()
	if closed {
		return net.ErrClosed
	}
	return conn.SetWriteDeadline(t)
}

func (c *rebindablePacketConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	conn := c.conn
	c.mu.Unlock()
	return conn.Close()
}

var _ net.PacketConn = (*rebindablePacketConn)(nil)
