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
