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

		// Paused bucket — only the incomplete (DL) side
		Entry("pausedDL", "pausedDL", StatusPaused),
		Entry("stoppedDL", "stoppedDL", StatusPaused),
		Entry("queuedDL", "queuedDL", StatusPaused),

		// Error bucket
		Entry("error", "error", StatusError),
		Entry("missingFiles", "missingFiles", StatusError),
		Entry("unknown literal", "unknown", StatusError),

		// Completed — idle UP states are finished downloads awaiting import
		Entry("moving", "moving", StatusCompleted),
		Entry("pausedUP", "pausedUP", StatusCompleted),
		Entry("stoppedUP", "stoppedUP", StatusCompleted),
		Entry("queuedUP", "queuedUP", StatusCompleted),

		// Fallthrough / unrecognized states -> error
		Entry("unknown state string", "not-a-qb-state", StatusError),
		Entry("empty string", "", StatusError),
	)
})
