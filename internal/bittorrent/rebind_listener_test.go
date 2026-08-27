package bittorrent

import (
	"net"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("rebindableListener", Label("unit", "bittorrent"), func() {
	It("accepts on the new socket after a rebind", func() {
		l, err := newRebindableListener(net.IPv4(127, 0, 0, 1), 0)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(l.Close()).To(Succeed())
		})

		accepted := make(chan net.Conn, 1)
		go func() {
			defer GinkgoRecover()
			c, err := l.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}()

		Expect(l.rebind(0)).To(Succeed())

		dialed, err := net.Dial("tcp", l.Addr().String())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(dialed.Close()).To(Succeed())
		})

		var got net.Conn
		Eventually(accepted, "2s").Should(Receive(&got))
		Expect(got.Close()).To(Succeed())
	})

	It("leaves an already-accepted connection usable across a rebind", func() {
		l, err := newRebindableListener(net.IPv4(127, 0, 0, 1), 0)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(l.Close()).To(Succeed())
		})

		dialed, err := net.Dial("tcp", l.Addr().String())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(dialed.Close()).To(Succeed())
		})
		server, err := l.Accept()
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(server.Close()).To(Succeed())
		})

		Expect(l.rebind(0)).To(Succeed())

		_, err = dialed.Write([]byte("still here"))
		Expect(err).NotTo(HaveOccurred())
		buf := make([]byte, 10)
		n, err := server.Read(buf)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(buf[:n])).To(Equal("still here"))
	})

	It("reports the new port from Addr after a rebind", func() {
		l, err := newRebindableListener(net.IPv4(127, 0, 0, 1), 0)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(l.Close()).To(Succeed())
		})

		before := l.Addr().String()
		Expect(l.rebind(0)).To(Succeed())
		Expect(l.Addr().String()).NotTo(Equal(before))
	})

	It("keeps the working socket when the new bind fails", func() {
		l, err := newRebindableListener(net.IPv4(127, 0, 0, 1), 0)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(l.Close()).To(Succeed())
		})
		before := l.Addr().String()

		squatter, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(squatter.Close()).To(Succeed())
		})
		taken := uint16(squatter.Addr().(*net.TCPAddr).Port)

		Expect(l.rebind(taken)).NotTo(Succeed())
		Expect(l.Addr().String()).To(Equal(before))
	})

	It("returns ErrClosed from Accept once closed", func() {
		l, err := newRebindableListener(net.IPv4(127, 0, 0, 1), 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(l.Close()).To(Succeed())

		_, err = l.Accept()
		Expect(err).To(MatchError(net.ErrClosed))
	})

	It("rejects a rebind after Close without installing a new listener", func() {
		l, err := newRebindableListener(net.IPv4(127, 0, 0, 1), 0)
		Expect(err).NotTo(HaveOccurred())
		before := l.Addr().String()

		Expect(l.Close()).To(Succeed())

		err = l.rebind(0)
		Expect(err).To(MatchError(net.ErrClosed))
		Expect(l.Addr().String()).To(Equal(before))
	})
})
