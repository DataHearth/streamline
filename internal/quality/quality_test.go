package quality_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/quality"
)

var _ = Describe("Evaluate", Label("unit", "quality"), func() {
	var p quality.Profile
	BeforeEach(func() {
		mkf := func(name, pat string) quality.ScoredFormat {
			f, err := quality.NewFormat(name, []quality.Condition{{
				Type: quality.ConditionReleaseTitle, Pattern: pat, Required: true,
			}})
			Expect(err).NotTo(HaveOccurred())
			return quality.ScoredFormat{Format: f}
		}
		remux, hdr, junk := mkf("remux", `(?i)\bremux\b`),
			mkf("hdr", `(?i)\bhdr\b`), mkf("junk", `(?i)\bcam\b`)
		remux.Score, hdr.Score, junk.Score = 200, 100, -1000
		p = quality.Profile{
			MinResolution: "1080p", MaxResolution: "2160p",
			MinScore: 0, UpgradeUntilScore: 300, UpgradeAllowed: true,
			Formats: []quality.ScoredFormat{remux, hdr, junk},
		}
	})

	It("sums matched format scores", func() {
		r := quality.Evaluate(p, quality.ReleaseContext{
			Title: "Movie.2024.2160p.BluRay.REMUX.HDR", Resolution: "2160p",
		})
		Expect(r.Score).To(Equal(300))
		Expect(r.Matched).To(ConsistOf("remux", "hdr"))
		Expect(r.Rejected).To(BeFalse())
	})
	It("rejects below min_score", func() {
		r := quality.Evaluate(p, quality.ReleaseContext{
			Title: "Movie.2024.2160p.CAM", Resolution: "2160p",
		})
		Expect(r.Score).To(Equal(-1000))
		Expect(r.Rejected).To(BeTrue())
	})
	It("rejects outside the resolution band and on unparseable resolution", func() {
		Expect(quality.Evaluate(p, quality.ReleaseContext{
			Title: "Movie.2024.720p.WEB", Resolution: "720p",
		}).Rejected).To(BeTrue())
		Expect(quality.Evaluate(p, quality.ReleaseContext{
			Title: "Movie.2024.WEB", Resolution: "",
		}).Rejected).To(BeTrue())
	})
	It("unlisted formats contribute nothing", func() {
		r := quality.Evaluate(p, quality.ReleaseContext{
			Title: "Movie.2024.1080p.WEB-DL.x264", Resolution: "1080p",
		})
		Expect(r.Score).To(Equal(0))
		Expect(r.Rejected).To(BeFalse())
	})
})

var _ = Describe("ShouldUpgrade", Label("unit", "quality"), func() {
	p := quality.Profile{UpgradeAllowed: true, UpgradeUntilScore: 300}
	It("upgrades strictly better below the cap", func() {
		Expect(p.ShouldUpgrade(150, 300)).To(BeTrue())
		Expect(p.ShouldUpgrade(150, 150)).To(BeFalse())
	})
	It("stops at the cap", func() {
		Expect(p.ShouldUpgrade(300, 999)).To(BeFalse())
	})
	It("cap 0 means no cap", func() {
		q := p
		q.UpgradeUntilScore = 0
		Expect(q.ShouldUpgrade(300, 301)).To(BeTrue())
	})
	It("respects upgrade_allowed", func() {
		q := p
		q.UpgradeAllowed = false
		Expect(q.ShouldUpgrade(0, 999)).To(BeFalse())
	})
})

var _ = Describe("UpgradableFrom", Label("unit", "quality"), func() {
	p := quality.Profile{MinResolution: "1080p", MaxResolution: "1080p"}

	It("allows an in-band file", func() {
		Expect(p.UpgradableFrom("1080p")).To(BeTrue())
	})
	It("allows a file below the band", func() {
		Expect(p.UpgradableFrom("720p")).To(BeTrue())
		Expect(p.UpgradableFrom("480p")).To(BeTrue())
	})
	It("refuses a file above the band", func() {
		Expect(p.UpgradableFrom("2160p")).To(BeFalse())
	})
	It("refuses an unknown resolution", func() {
		Expect(p.UpgradableFrom("")).To(BeFalse())
		Expect(p.UpgradableFrom("360p")).To(BeFalse())
	})
	It("refuses everything under a profile with no band", func() {
		Expect(quality.Profile{}.UpgradableFrom("1080p")).To(BeFalse())
	})
	It("treats 4K as its 2160p bucket", func() {
		q := quality.Profile{MinResolution: "1080p", MaxResolution: "2160p"}
		Expect(q.UpgradableFrom("4K")).To(BeTrue())
	})
})

var _ = Describe("CompareResolutions", Label("unit", "quality"), func() {
	It("orders the buckets", func() {
		Expect(quality.CompareResolutions("1080p", "720p")).To(BeNumerically(">", 0))
		Expect(quality.CompareResolutions("720p", "1080p")).To(BeNumerically("<", 0))
		Expect(
			quality.CompareResolutions("2160p", "1080p"),
		).To(BeNumerically(">", 0))
	})
	It("reports equal buckets as equal, 4K included", func() {
		Expect(quality.CompareResolutions("1080p", "1080p")).To(Equal(0))
		Expect(quality.CompareResolutions("4K", "2160p")).To(Equal(0))
	})
	It("ranks an unknown value below every known bucket", func() {
		Expect(quality.CompareResolutions("", "480p")).To(BeNumerically("<", 0))
		Expect(quality.CompareResolutions("360p", "720p")).To(BeNumerically("<", 0))
		Expect(quality.CompareResolutions("360p", "")).To(Equal(0))
	})
})

var _ = Describe("ReplacesFile", Label("unit", "quality"), func() {
	var p quality.Profile
	BeforeEach(func() {
		remux, err := quality.NewFormat("remux", []quality.Condition{
			{
				Type:     quality.ConditionReleaseTitle,
				Pattern:  `(?i)\bremux\b`,
				Required: true,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		p = quality.Profile{
			MinResolution: "720p", MaxResolution: "1080p", UpgradeAllowed: true,
			Formats: []quality.ScoredFormat{{Format: remux, Score: 200}},
		}
	})

	ctxFor := func(title string) quality.ReleaseContext {
		return quality.ReleaseContext{Title: title, Resolution: "1080p"}
	}

	It("replaces a plain file with a higher-scoring release", func() {
		Expect(quality.ReplacesFile(p,
			ctxFor("Show.S01E01.1080p.WEB-DL.x264-GRP"),
			ctxFor("Show.S01E01.1080p.BluRay.REMUX.x264-GRP"),
		)).To(BeTrue())
	})

	It("leaves a file the release only ties", func() {
		Expect(quality.ReplacesFile(p,
			ctxFor("Show.S01E02.1080p.BluRay.REMUX.x264-GRP"),
			ctxFor("Show.S01E02.1080p.BluRay.REMUX.x264-OTHER"),
		)).To(BeFalse())
	})

	It("never replaces when the profile forbids upgrades", func() {
		p.UpgradeAllowed = false
		Expect(quality.ReplacesFile(p,
			ctxFor("Show.S01E01.1080p.WEB-DL.x264-GRP"),
			ctxFor("Show.S01E01.1080p.BluRay.REMUX.x264-GRP"),
		)).To(BeFalse())
	})

	It("never replaces a file above the profile ceiling", func() {
		existing := quality.ReleaseContext{
			Title: "Show.S01E01.2160p.BluRay.REMUX.x265-GRP", Resolution: "2160p",
		}
		Expect(quality.ReplacesFile(p, existing,
			ctxFor("Show.S01E01.1080p.BluRay.REMUX.x264-GRP"),
		)).To(BeFalse())
	})

	It("replaces a file whose resolution is below the band", func() {
		existing := quality.ReleaseContext{
			Title: "Show.S01E01.480p.WEB-DL.x264-GRP", Resolution: "480p",
		}
		Expect(quality.ReplacesFile(p, existing,
			ctxFor("Show.S01E01.1080p.BluRay.REMUX.x264-GRP"),
		)).To(BeTrue())
	})

	It("never replaces a file whose resolution cannot be determined", func() {
		Expect(quality.ReplacesFile(p,
			quality.ReleaseContext{Title: "episode1.mkv"},
			ctxFor("Show.S01E01.1080p.BluRay.REMUX.x264-GRP"),
		)).To(BeFalse())
	})
})
