package bittorrent

import (
	"github.com/anacrolix/torrent/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("wantedBytes", Label("unit", "bittorrent"), func() {
	It("equals full length when everything is wanted", func() {
		t := newTestTorrent()
		applyFilePriorities(t, "all", nil)
		Expect(wantedBytes(t)).To(Equal(t.Length()))
	})

	It("drops skipped files from the denominator", func() {
		t := newTestTorrent()
		applyFilePriorities(t, "all", nil)
		t.Files()[0].SetPriority(types.PiecePriorityNone)
		Expect(wantedBytes(t)).To(Equal(t.Length() - t.Files()[0].Length()))
	})
})

var _ = Describe("wantedCompleted", Label("unit", "bittorrent"), func() {
	It("equals full completed bytes when everything is wanted", func() {
		t := newTestTorrent()
		applyFilePriorities(t, "all", nil)
		Expect(wantedCompleted(t)).To(Equal(t.BytesCompleted()))
	})

	It("drops a skipped file's bytes from completion", func() {
		t := newTestTorrent()
		applyFilePriorities(t, "all", nil)
		skipped := t.Files()[0]
		completedBefore := skipped.BytesCompleted()
		skipped.SetPriority(types.PiecePriorityNone)
		Expect(wantedCompleted(t)).To(Equal(t.BytesCompleted() - completedBefore))
	})
})
