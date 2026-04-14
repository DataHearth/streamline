package rss

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("QualityConfig.Accepts", Label("unit", "rss"), func() {
	base := func() QualityConfig {
		return QualityConfig{
			PreferredResolution: "1080p",
			MinResolution:       "720p",
			UpgradeAllowed:      true,
			NoMatchCooldown:     6 * time.Hour,
			MaxGrabFailures:     3,
		}
	}

	DescribeTable(
		"upgrade_allowed=true (default config)",
		func(title string, want bool) {
			Expect(base().Accepts(title)).To(Equal(want))
		},
		Entry(
			"1080p matches preferred",
			"Fight.Club.1999.1080p.BluRay.x264-GROUP",
			true,
		),
		Entry(
			"720p at min accepted",
			"Fight.Club.1999.720p.BluRay.x264-GROUP",
			true,
		),
		Entry(
			"2160p above preferred accepted",
			"Fight.Club.1999.2160p.BluRay.x265-GROUP",
			true,
		),
		Entry("unparseable title rejected", "some.random.garbage.title", false),
	)

	DescribeTable("MinResolution=1080p rejects anything lower",
		func(title string, want bool) {
			q := base()
			q.MinResolution = "1080p"
			Expect(q.Accepts(title)).To(Equal(want))
		},
		Entry("720p below min", "Fight.Club.1999.720p.BluRay.x264-GROUP", false),
		Entry("1080p at min", "Fight.Club.1999.1080p.BluRay.x264-GROUP", true),
		Entry("2160p above min", "Fight.Club.1999.2160p.BluRay.x265-GROUP", true),
	)

	DescribeTable(
		"unknown configured resolutions rank as 0",
		func(title, minRes, prefRes string, upgradeAllowed, want bool) {
			q := base()
			q.MinResolution = minRes
			q.PreferredResolution = prefRes
			q.UpgradeAllowed = upgradeAllowed
			Expect(q.Accepts(title)).To(Equal(want))
		},
		Entry(
			"unknown MinResolution: any known res ranks 1080p=2 >= 0 so accepted with upgrade",
			"Fight.Club.1999.1080p.BluRay.x264-GROUP",
			"1440p",
			"1080p",
			true,
			true,
		),
		Entry(
			"unknown PreferredResolution + upgrade off rejects any real res",
			"Fight.Club.1999.1080p.BluRay.x264-GROUP",
			"720p", "1440p", false, false),
	)

	DescribeTable(
		"UpgradeAllowed=false, only preferred passes",
		func(title string, want bool) {
			q := base()
			q.UpgradeAllowed = false
			Expect(q.Accepts(title)).To(Equal(want))
		},
		Entry(
			"720p below preferred",
			"Fight.Club.1999.720p.BluRay.x264-GROUP",
			false,
		),
		Entry(
			"1080p equals preferred",
			"Fight.Club.1999.1080p.BluRay.x264-GROUP",
			true,
		),
		Entry(
			"2160p above preferred",
			"Fight.Club.1999.2160p.BluRay.x265-GROUP",
			false,
		),
	)
})
