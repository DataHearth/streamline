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

	// An uploaded .torrent skips the indexer fetch, so this is the only place
	// the domain ceiling gets applied to it. The bytes are refused on size
	// before metainfo.Load ever looks at them, so garbage of the right size
	// still reports the size, not a parse failure.
	It("rejects torrent bytes over the fetch-path ceiling", func() {
		_, _, _, err := specFromSource(download.TorrentSource{
			Bytes: make([]byte, download.MaxTorrentFileSize+1),
		})
		Expect(err).To(MatchError(ContainSubstring("over the")))
	})

	It("accepts bytes exactly at the ceiling, failing only on parse", func() {
		_, _, _, err := specFromSource(download.TorrentSource{
			Bytes: make([]byte, download.MaxTorrentFileSize),
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("parse torrent file"))
	})
})
