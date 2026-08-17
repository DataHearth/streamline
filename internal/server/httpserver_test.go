package server

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// armReadDeadline bounds every socket read so a spec that the server never
// answers fails instead of hanging the suite.
func armReadDeadline(conn net.Conn) {
	GinkgoHelper()

	Expect(conn.SetReadDeadline(time.Now().Add(5 * time.Second))).To(Succeed())
}

// serveOn starts srv on a fresh loopback listener and returns its address.
func serveOn(srv *http.Server) string {
	GinkgoHelper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		_ = srv.Serve(ln)
	}()
	DeferCleanup(srv.Close)

	return ln.Addr().String()
}

var _ = Describe("NewHTTPServer", Label("unit", "server"), func() {
	It("pins the production timeouts", func() {
		handler := http.NewServeMux()
		srv := NewHTTPServer("127.0.0.1:9999", handler)

		Expect(srv.Addr).To(Equal("127.0.0.1:9999"))
		Expect(srv.Handler).To(BeIdenticalTo(handler))
		Expect(srv.ReadHeaderTimeout).To(Equal(10 * time.Second))
		Expect(srv.ReadTimeout).To(Equal(time.Minute))
		Expect(srv.WriteTimeout).To(Equal(2 * time.Minute))
		Expect(srv.IdleTimeout).To(Equal(2 * time.Minute))
	})

	// The deadlines are shortened here so the suite stays fast, after
	// asserting the constructor set them at all: shortening a field the
	// constructor left at zero would turn these into stdlib tests. The spec
	// above owns the production values.
	Context("on a live listener", func() {
		var srv *http.Server

		BeforeEach(func() {
			srv = NewHTTPServer("", http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				},
			))
		})

		It("closes a connection stuck in partial headers", func() {
			Expect(srv.ReadHeaderTimeout).NotTo(BeZero())
			srv.ReadHeaderTimeout = 250 * time.Millisecond
			addr := serveOn(srv)

			conn, err := net.Dial("tcp", addr)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(conn.Close)

			By("sending headers that never terminate")
			_, err = conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\nX-Slow: 1"))
			Expect(err).NotTo(HaveOccurred())

			armReadDeadline(conn)
			start := time.Now()
			_, err = conn.Read(make([]byte, 1))

			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, os.ErrDeadlineExceeded)).To(
				BeFalse(), "server should close the socket, not our deadline",
			)
			Expect(time.Since(start)).To(BeNumerically("<", 2*time.Second))
		})

		It("closes an idle keep-alive connection", func() {
			Expect(srv.IdleTimeout).NotTo(BeZero())
			srv.IdleTimeout = 250 * time.Millisecond
			addr := serveOn(srv)

			conn, err := net.Dial("tcp", addr)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(conn.Close)

			_, err = conn.Write([]byte(
				"GET / HTTP/1.1\r\nHost: x\r\nConnection: keep-alive\r\n\r\n",
			))
			Expect(err).NotTo(HaveOccurred())

			By("draining the response so the connection goes idle")
			armReadDeadline(conn)
			br := bufio.NewReader(conn)
			resp, err := http.ReadResponse(br, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Body.Close()).To(Succeed())

			armReadDeadline(conn)
			start := time.Now()
			_, err = br.Read(make([]byte, 1))

			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, os.ErrDeadlineExceeded)).To(
				BeFalse(), "server should close the socket, not our deadline",
			)
			Expect(time.Since(start)).To(BeNumerically("<", 2*time.Second))
		})
	})
})
