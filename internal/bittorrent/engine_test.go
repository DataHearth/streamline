package bittorrent

import (
	"net"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
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

var _ = Describe("newClientConfig", Label("unit", "bittorrent"), func() {
	It("disables WebTorrent for an unbound engine", func() {
		cc := newClientConfig(
			config.DownloadClientEntry{DownloadDir: "/tmp/streamline-dl"},
			nil, nil,
		)
		Expect(cc.DisableWebtorrent).To(BeTrue())
	})

	It("disables WebTorrent when bound to an interface", func() {
		cc := newClientConfig(
			config.DownloadClientEntry{DownloadDir: "/tmp/streamline-dl"},
			net.ParseIP("10.11.12.13"), nil,
		)
		Expect(cc.DisableWebtorrent).To(BeTrue())
		Expect(cc.DialForPeerConns).To(BeFalse())
		Expect(cc.DisableIPv6).To(BeTrue())
		Expect(cc.NoDefaultPortForwarding).To(BeTrue())
	})

	It("carries the entry's transport knobs through", func() {
		entry := config.DownloadClientEntry{
			DownloadDir: "/tmp/streamline-dl",
			DisableDHT:  true,
			ListenPort:  12345,
		}
		cc := newClientConfig(entry, nil, nil)
		Expect(cc.NoDHT).To(BeTrue())
		Expect(cc.ListenPort).To(Equal(12345))
		Expect(cc.DataDir).To(Equal(entry.DownloadDir))
	})
})
