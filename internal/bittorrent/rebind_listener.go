package bittorrent

import (
	"context"
	"fmt"
	"log/slog"
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

// port is always 0 in this task's own callers; the engine wiring that passes
// a real configured port lands in a later task.
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
			if cerr := conn.Close(); cerr != nil {
				slog.WarnContext(
					context.Background(),
					"closing conn after listener shutdown failed",
					"error",
					cerr,
				)
			}
			return
		}
	}
}

// rebind moves the listening socket to port, binding the replacement before
// retiring the old one so a failure leaves the engine reachable where it was.
func (l *rebindableListener) rebind(port uint16) error {
	next, err := net.ListenTCP("tcp", &net.TCPAddr{IP: l.ip, Port: int(port)})
	if err != nil {
		return fmt.Errorf("listen tcp: %w", err)
	}

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		if cerr := next.Close(); cerr != nil {
			slog.WarnContext(
				context.Background(),
				"closing unused listener failed",
				"error",
				cerr,
			)
		}
		return net.ErrClosed
	}
	prev := l.ln
	l.ln = next
	l.mu.Unlock()

	go l.pump(next)

	if err := prev.Close(); err != nil {
		slog.WarnContext(
			context.Background(),
			"closing retired listener failed",
			"error",
			err,
		)
	}
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
