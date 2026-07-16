package bittorrent

import (
	"net"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/download"
)

var _ = Describe("parseHash", Label("unit", "bittorrent"), func() {
	It("parses a 40-char hex hash", func() {
		h, err := parseHash("aabbccddeeff00112233445566778899aabbccdd")
		Expect(err).NotTo(HaveOccurred())
		Expect(h.HexString()).To(
			Equal("aabbccddeeff00112233445566778899aabbccdd"))
	})

	It("rejects bad input as torrent-not-found", func() {
		for _, s := range []string{"", "zz", "aabb", "not-hex-at-all!"} {
			_, err := parseHash(s)
			Expect(err).To(MatchError(download.ErrTorrentNotFound), s)
		}
	})
})

var _ = Describe("resolveBindIP", Label("unit", "bittorrent"), func() {
	It("returns no IP for an empty interface (all interfaces)", func() {
		ip, err := resolveBindIP("")
		Expect(err).NotTo(HaveOccurred())
		Expect(ip).To(BeNil())
	})

	It("uses a literal IPv4 address verbatim", func() {
		ip, err := resolveBindIP("10.11.12.13")
		Expect(err).NotTo(HaveOccurred())
		Expect(ip.Equal(net.ParseIP("10.11.12.13"))).To(BeTrue())
	})

	It("uses a literal IPv6 address verbatim", func() {
		ip, err := resolveBindIP("2001:db8::1")
		Expect(err).NotTo(HaveOccurred())
		Expect(ip.Equal(net.ParseIP("2001:db8::1"))).To(BeTrue())
	})

	It("fails start when the named interface does not exist", func() {
		_, err := resolveBindIP("streamline-no-such-iface0")
		Expect(err).To(HaveOccurred())
	})
})
