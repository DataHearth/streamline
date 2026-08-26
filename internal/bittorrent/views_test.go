package bittorrent

import (
	"time"

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

var _ = Describe("view ordering", Label("unit", "bittorrent"), func() {
	base := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

	It("orders oldest first and breaks a tie on the hash", func() {
		views := []TorrentView{
			{Hash: "cc", AddedAt: base},
			{Hash: "aa", AddedAt: base.Add(time.Hour)},
			{Hash: "bb", AddedAt: base},
		}
		sortViews(views)
		Expect([]string{views[0].Hash, views[1].Hash, views[2].Hash}).
			To(Equal([]string{"bb", "cc", "aa"}))
	})

	// The engine reads its torrents out of a map, so the input arrives in a
	// different order every call; the same set must still come back the same
	// way or the SPA's two-second poll makes the rows jump.
	It("is independent of the order it receives", func() {
		mk := func(hashes ...string) []TorrentView {
			out := make([]TorrentView, 0, len(hashes))
			for _, h := range hashes {
				out = append(out, TorrentView{Hash: h, AddedAt: base})
			}
			return out
		}
		want := mk("a", "b", "c", "d")
		sortViews(want)
		for _, in := range [][]TorrentView{
			mk("d", "c", "b", "a"),
			mk("c", "a", "d", "b"),
			mk("b", "d", "a", "c"),
		} {
			sortViews(in)
			Expect(in).To(Equal(want))
		}
	})
})
