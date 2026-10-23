package mailer

import (
	"crypto/tls"
	"io"
	"net"
	"net/textproto"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeSMTP struct {
	ln         net.Listener
	offerTLS   bool
	rejectRcpt bool

	mu        sync.Mutex
	data      []byte
	authSeen  bool
	authOnTLS bool
}

func newFakeSMTP(offerTLS, rejectRcpt bool) *fakeSMTP {
	GinkgoHelper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	f := &fakeSMTP{ln: ln, offerTLS: offerTLS, rejectRcpt: rejectRcpt}
	DeferCleanup(ln.Close)
	go f.accept()
	return f
}

func (f *fakeSMTP) port() int {
	return f.ln.Addr().(*net.TCPAddr).Port
}

func (f *fakeSMTP) accept() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.serve(conn)
	}
}

func (f *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	isTLS := false
	tp := textproto.NewConn(conn)
	reply := func(s string) bool { return tp.PrintfLine("%s", s) == nil }
	if !reply("220 fake ESMTP") {
		return
	}
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			lines := []string{"250-fake"}
			if f.offerTLS && !isTLS {
				lines = append(lines, "250-STARTTLS")
			}
			lines = append(lines, "250 AUTH PLAIN")
			for _, l := range lines {
				if !reply(l) {
					return
				}
			}
		case cmd == "STARTTLS":
			if !reply("220 go ahead") {
				return
			}
			tc := tls.Server(
				conn,
				&tls.Config{Certificates: []tls.Certificate{serverCert}},
			)
			if err := tc.Handshake(); err != nil {
				return
			}
			isTLS = true
			tp = textproto.NewConn(tc)
		case strings.HasPrefix(cmd, "AUTH"):
			f.mu.Lock()
			f.authSeen = true
			f.authOnTLS = isTLS
			f.mu.Unlock()
			reply("235 2.7.0 ok")
		case strings.HasPrefix(cmd, "MAIL"):
			reply("250 ok")
		case strings.HasPrefix(cmd, "RCPT"):
			if f.rejectRcpt {
				reply("550 5.1.1 no such user")
			} else {
				reply("250 ok")
			}
		case cmd == "DATA":
			reply("354 go")
			body, err := io.ReadAll(tp.DotReader())
			if err != nil {
				return
			}
			f.mu.Lock()
			f.data = body
			f.mu.Unlock()
			reply("250 queued")
		case cmd == "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 unknown")
		}
	}
}

func (f *fakeSMTP) captured() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data
}

func (f *fakeSMTP) auth() (seen, onTLS bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authSeen, f.authOnTLS
}
