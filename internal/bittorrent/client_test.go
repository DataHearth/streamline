package bittorrent

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/download"
)

var _ = Describe("specFromSource", Label("unit", "bittorrent"), func() {
	It("parses a magnet URI", func() {
		spec, magnet, raw, err := specFromSource(download.TorrentSource{
			Magnet: "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd&dn=test",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.InfoHash.HexString()).To(
			Equal("aabbccddeeff00112233445566778899aabbccdd"))
		Expect(magnet).NotTo(BeEmpty())
		Expect(raw).To(BeNil())
	})

	It("rejects an empty source", func() {
		_, _, _, err := specFromSource(download.TorrentSource{})
		Expect(err).To(HaveOccurred())
	})

	It("rejects garbage torrent bytes", func() {
		_, _, _, err := specFromSource(download.TorrentSource{
			Bytes: []byte("not bencode"),
		})
		Expect(err).To(HaveOccurred())
	})
})
