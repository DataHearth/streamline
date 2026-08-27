package bittorrent

import (
	"net"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("rebindablePacketConn", Label("unit", "bittorrent"), func() {
	It("keeps a blocked ReadFrom alive across a rebind", func() {
		c, err := newRebindablePacketConn(net.IPv4(127, 0, 0, 1), 0)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(c.Close()).To(Succeed())
		})

		type read struct {
			payload string
			err     error
		}
		reads := make(chan read, 1)
		go func() {
			defer GinkgoRecover()
			buf := make([]byte, 64)
			n, _, err := c.ReadFrom(buf)
			reads <- read{payload: string(buf[:n]), err: err}
		}()

		// Let the reader block on the original socket before retiring it, so
		// the spec exercises the wake-on-close path rather than a fresh read.
		time.Sleep(50 * time.Millisecond)
		Expect(c.rebind(0)).To(Succeed())

		target, err := net.ResolveUDPAddr("udp", c.LocalAddr().String())
		Expect(err).NotTo(HaveOccurred())
		sender, err := net.DialUDP("udp", nil, target)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(sender.Close()).To(Succeed())
		})
		_, err = sender.Write([]byte("after-rebind"))
		Expect(err).NotTo(HaveOccurred())

		var got read
		Eventually(reads, "2s").Should(Receive(&got))
		Expect(got.err).NotTo(HaveOccurred())
		Expect(got.payload).To(Equal("after-rebind"))
	})

	It("reports the new port from LocalAddr after a rebind", func() {
		c, err := newRebindablePacketConn(net.IPv4(127, 0, 0, 1), 0)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(c.Close()).To(Succeed())
		})

		before := c.LocalAddr().String()
		Expect(c.rebind(0)).To(Succeed())
		Expect(c.LocalAddr().String()).NotTo(Equal(before))
	})

	It("keeps the working socket when the new bind fails", func() {
		c, err := newRebindablePacketConn(net.IPv4(127, 0, 0, 1), 0)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(c.Close()).To(Succeed())
		})
		before := c.LocalAddr().String()

		// Squat the target port so the rebind's bind cannot succeed.
		squatter, err := net.ListenUDP(
			"udp",
			&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)},
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(squatter.Close()).To(Succeed())
		})
		taken := uint16(squatter.LocalAddr().(*net.UDPAddr).Port)

		Expect(c.rebind(taken)).NotTo(Succeed())
		Expect(c.LocalAddr().String()).To(Equal(before))
	})

	It("returns ErrClosed once closed", func() {
		c, err := newRebindablePacketConn(net.IPv4(127, 0, 0, 1), 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Close()).To(Succeed())

		_, _, err = c.ReadFrom(make([]byte, 8))
		Expect(err).To(MatchError(net.ErrClosed))
	})
})
