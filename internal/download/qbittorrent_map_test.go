package download

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("mapQBState", Label("unit", "downloads"), func() {
	DescribeTable("maps qBittorrent states to TorrentStatus",
		func(state string, want TorrentStatus) {
			Expect(mapQBState(state)).To(Equal(want))
		},
		// Downloading bucket
		Entry("downloading", "downloading", StatusDownloading),
		Entry("metaDL", "metaDL", StatusDownloading),
		Entry("forcedDL", "forcedDL", StatusDownloading),
		Entry("allocating", "allocating", StatusDownloading),
		Entry("stalledDL", "stalledDL", StatusDownloading),
		Entry("checkingDL", "checkingDL", StatusDownloading),
		Entry("checkingResumeData", "checkingResumeData", StatusDownloading),

		// Seeding bucket
		Entry("uploading", "uploading", StatusSeeding),
		Entry("forcedUP", "forcedUP", StatusSeeding),
		Entry("stalledUP", "stalledUP", StatusSeeding),
		Entry("checkingUP", "checkingUP", StatusSeeding),

		// Paused bucket
		Entry("pausedDL", "pausedDL", StatusPaused),
		Entry("pausedUP", "pausedUP", StatusPaused),
		Entry("queuedDL", "queuedDL", StatusPaused),
		Entry("queuedUP", "queuedUP", StatusPaused),

		// Error bucket
		Entry("error", "error", StatusError),
		Entry("missingFiles", "missingFiles", StatusError),
		Entry("unknown literal", "unknown", StatusError),

		// Completed
		Entry("moving", "moving", StatusCompleted),

		// Fallthrough / unrecognized states -> error
		Entry("unknown state string", "not-a-qb-state", StatusError),
		Entry("empty string", "", StatusError),
	)
})
