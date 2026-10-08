package indexer

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("infoHash", Label("unit", "indexers"), func() {
	const hex = "0123456789abcdef0123456789abcdef01234567"

	It("lowercases a hex hash", func() {
		Expect(infoHash("0123456789ABCDEF0123456789ABCDEF01234567")).To(Equal(hex))
	})

	It("decodes a base32 btih to hex", func() {
		Expect(
			infoHash(
				"",
				"magnet:?xt=urn:btih:AERUKZ4JVPG66AJDIVTYTK6N54ASGRLH&dn=x",
			),
		).
			To(Equal(hex))
	})

	It("prefers the indexer's hash over a magnet", func() {
		other := "magnet:?xt=urn:btih:ffffffffffffffffffffffffffffffffffffffff"
		Expect(infoHash(hex, other)).To(Equal(hex))
	})

	It("takes the first magnet carrying a hash", func() {
		Expect(infoHash("", "https://idx/dl/1", "magnet:?xt=urn:btih:"+hex)).
			To(Equal(hex))
	})

	It("is empty for a torrent link or a malformed hash", func() {
		Expect(infoHash("", "https://idx/dl/1")).To(BeEmpty())
		Expect(infoHash("not-a-hash", "magnet:?xt=urn:btih:abc123")).To(BeEmpty())
	})
})
