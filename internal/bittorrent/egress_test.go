package bittorrent

import (
	"context"
	"net"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
)

var _ = Describe("tracker and webseed egress", Label("unit", "bittorrent"), func() {
	DescribeTable("refuses an address on the host or its link",
		func(ip string) {
			Expect(
				refuseInternalIP(net.ParseIP(ip)),
			).To(MatchError(errInternalEgress))
		},
		Entry("IPv4 loopback", "127.0.0.1"),
		Entry("IPv6 loopback", "::1"),
		Entry("cloud metadata", "169.254.169.254"),
		Entry("IPv6 link-local", "fe80::1"),
		Entry("unspecified", "0.0.0.0"),
		Entry("multicast", "239.255.255.250"),
	)

	DescribeTable("reaches public and private addresses",
		func(ip string) {
			Expect(refuseInternalIP(net.ParseIP(ip))).To(Succeed())
		},
		Entry("public", "93.184.216.34"),
		Entry("homelab tracker", "192.168.1.5"),
		Entry("private IPv6", "fd00::5"),
	)

	It("refuses a tracker dial that resolves to loopback", func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ln.Close)

		cc := newClientConfig(
			config.DownloadClientEntry{DownloadDir: GinkgoT().TempDir()},
			nil,
			nil,
			nil,
		)
		_, err = cc.TrackerDialContext(
			context.Background(),
			"tcp",
			ln.Addr().String(),
		)

		Expect(err).To(MatchError(errInternalEgress))
	})

	It("refuses a UDP tracker packet to loopback", func() {
		cc := newClientConfig(
			config.DownloadClientEntry{DownloadDir: GinkgoT().TempDir()},
			nil,
			nil,
			nil,
		)
		pc, err := cc.TrackerListenPacket("udp4", "")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pc.Close)

		_, err = pc.WriteTo(
			[]byte("x"),
			&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6969},
		)

		Expect(err).To(MatchError(errInternalEgress))
	})
})
