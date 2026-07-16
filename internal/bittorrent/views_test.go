package bittorrent

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/anacrolix/torrent/types"
)

var _ = Describe("file priorities", Label("unit", "bittorrent"), func() {
	It("maps API names to piece priorities and back", func() {
		for name, prio := range map[string]types.PiecePriority{
			"skip":   types.PiecePriorityNone,
			"normal": types.PiecePriorityNormal,
			"high":   types.PiecePriorityHigh,
		} {
			got, err := parsePriority(name)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(prio))
			Expect(priorityName(prio)).To(Equal(name))
		}
	})

	It("rejects unknown priority names", func() {
		_, err := parsePriority("urgent")
		Expect(err).To(HaveOccurred())
	})

	It("labels exotic piece priorities as normal", func() {
		Expect(priorityName(types.PiecePriorityNow)).To(Equal("normal"))
	})
})
