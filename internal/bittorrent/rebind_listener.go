package bittorrent

import (
	"context"
	"fmt"
	"net"
	"sync"
)

// rebindableListener is the antorrent.Listener the engine registers with
// AddListener, so incoming TCP peer connections can be moved to another port
// without rebuilding the torrent client. Connections already accepted are
// their own kernel sockets and are untouched by a rebind, which is what lets
// TCP peers survive a forwarded-port rotation.
//
// Each underlying socket gets its own accept pump feeding one shared channel,
// so Accept never learns that the socket beneath it changed.
type rebindableListener struct {
	ip    net.IP
	conns chan net.Conn
	done  chan struct{}

	closeOnce sync.Once
	mu        sync.RWMutex
	ln        net.Listener
	// closed is read and written only under mu, so a rebind that reaches its
	// swap always agrees with a concurrent Close about which one happened
	// first — checking <-l.done outside the lock left a window where Close
	// could finish entirely between that check and the swap, installing a
	// replacement listener nothing would ever close.
	closed bool
}

// newPeerSockets (engine.go) never passes 0 here: a configured port is
// forwarded directly, and an unconfigured one is resolved to whatever the
// packet conn already bound, so the pair always answers on the same port.
// Only this package's own tests pass 0, to grab a free port.
//
//nolint:unparam // port is part of the constructor's public shape, not dead
func newRebindableListener(ip net.IP, port uint16) (*rebindableListener, error) {
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: ip, Port: int(port)})
	if err != nil {
		return nil, fmt.Errorf("listen tcp: %w", err)
	}
	l := &rebindableListener{
		ip:    ip,
		conns: make(chan net.Conn),
		done:  make(chan struct{}),
		ln:    ln,
	}
	go l.pump(ln)
	return l, nil
}

// pump feeds one socket's accepted connections into the shared channel. It
// exits when its socket is retired or the listener closes; it must never close
// the shared channel, which outlives any individual socket.
func (l *rebindableListener) pump(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		select {
		case l.conns <- conn:
		case <-l.done:
			closeAndWarn(
				context.Background(), "conn after listener shutdown", conn)
			return
		}
	}
}

// rebind moves the listening socket to port, binding the replacement before
// retiring the old one so a failure leaves the engine reachable where it was.
//
// Concurrent rebinds are not guarded here beyond the swap: two of them could
// each bind a socket and then race to install it, retiring the other's. What
// makes that unreachable is Engine.mu, which the only caller
// (Engine.movePeerSockets) holds for the whole pair of rebinds.
func (l *rebindableListener) rebind(port uint16) error {
	next, err := net.ListenTCP("tcp", &net.TCPAddr{IP: l.ip, Port: int(port)})
	if err != nil {
		return fmt.Errorf("listen tcp: %w", err)
	}

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		closeAndWarn(context.Background(), "unused listener", next)
		return net.ErrClosed
	}
	prev := l.ln
	l.ln = next
	l.mu.Unlock()

	go l.pump(next)

	closeAndWarn(context.Background(), "retired listener", prev)
	return nil
}

func (l *rebindableListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *rebindableListener) Addr() net.Addr {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.ln.Addr()
}

func (l *rebindableListener) Close() error {
	var err error
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		ln := l.ln
		l.mu.Unlock()
		close(l.done)
		err = ln.Close()
	})
	return err
}
