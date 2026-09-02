package ffmpeg

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("langSet", Label("unit", "ffmpeg"), func() {
	It("dedupes, canonicalises and sorts", func() {
		var l langSet
		l.add("fre")
		l.add("jpn")
		l.add("fra")
		l.add("FR")
		Expect(l.join()).To(Equal("fra,jpn"))
	})

	It("drops untagged and undefined tracks", func() {
		var l langSet
		l.add("")
		l.add("und")
		l.add("  ")
		Expect(l.join()).To(BeEmpty())
	})

	It("keeps mul, which is a real code and not an absence", func() {
		var l langSet
		l.add("mul")
		Expect(l.join()).To(Equal("mul"))
	})
})
