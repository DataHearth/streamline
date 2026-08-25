package quality_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/quality"
)

var _ = Describe("Format.Matches", Label("unit", "quality"), func() {
	title := func(pat string, req, neg bool) quality.Condition {
		return quality.Condition{
			Type: quality.ConditionReleaseTitle, Pattern: pat,
			Required: req, Negate: neg,
		}
	}
	mk := func(conds ...quality.Condition) quality.Format {
		f, err := quality.NewFormat("t", conds)
		Expect(err).NotTo(HaveOccurred())
		return f
	}
	rel := quality.ReleaseContext{
		Title: "Movie.2024.2160p.BluRay.REMUX.HDR.x265-GRP",
		Size:  8 << 30, Seeders: 50, HasSeeders: true,
	}

	It("passes when all required conditions match", func() {
		Expect(mk(title(`(?i)remux`, true, false)).Matches(rel)).To(BeTrue())
	})
	It("fails when a required condition misses", func() {
		Expect(mk(title(`(?i)webrip`, true, false)).Matches(rel)).To(BeFalse())
	})
	It("needs at least one optional hit when optionals exist", func() {
		f := mk(title(`(?i)remux`, true, false),
			title(`(?i)webrip`, false, false), title(`(?i)hdr`, false, false))
		Expect(f.Matches(rel)).To(BeTrue())
		f2 := mk(title(`(?i)remux`, true, false),
			title(`(?i)webrip`, false, false), title(`(?i)cam`, false, false))
		Expect(f2.Matches(rel)).To(BeFalse())
	})
	It("negate inverts a condition", func() {
		Expect(mk(title(`(?i)remux`, true, true)).Matches(rel)).To(BeFalse())
	})
	It("absent data evaluates false before negate", func() {
		noSeed := rel
		noSeed.HasSeeders = false
		c := quality.Condition{
			Type: quality.ConditionSeeders, Min: 1, Required: true,
		}
		Expect(mk(c).Matches(noSeed)).To(BeFalse())
		c.Negate = true
		Expect(mk(c).Matches(noSeed)).To(BeTrue())
	})
})

var _ = Describe("condition evaluation per type", Label("unit", "quality"), func() {
	mk := func(c quality.Condition) quality.Format {
		f, err := quality.NewFormat("t", []quality.Condition{c})
		Expect(err).NotTo(HaveOccurred())
		return f
	}

	It("matches resolution exactly and case-insensitively", func() {
		r := quality.ReleaseContext{Resolution: "1080p"}
		c := quality.Condition{
			Type:     quality.ConditionResolution,
			Value:    "1080p",
			Required: true,
		}
		Expect(mk(c).Matches(r)).To(BeTrue())

		r2 := quality.ReleaseContext{Resolution: "2160p"}
		Expect(mk(c).Matches(r2)).To(BeFalse())
	})

	It("matches source exactly and case-insensitively", func() {
		r := quality.ReleaseContext{Source: "bluray"}
		c := quality.Condition{
			Type:     quality.ConditionSource,
			Value:    "BluRay",
			Required: true,
		}
		Expect(mk(c).Matches(r)).To(BeTrue())

		r2 := quality.ReleaseContext{Source: "WEB-DL"}
		Expect(mk(c).Matches(r2)).To(BeFalse())
	})

	It("matches codec exactly and case-insensitively", func() {
		r := quality.ReleaseContext{Codec: "hevc"}
		c := quality.Condition{
			Type:     quality.ConditionCodec,
			Value:    "HEVC",
			Required: true,
		}
		Expect(mk(c).Matches(r)).To(BeTrue())

		r2 := quality.ReleaseContext{Codec: "x264"}
		Expect(mk(c).Matches(r2)).To(BeFalse())
	})

	It("matches release_group by regex against Group", func() {
		r := quality.ReleaseContext{Group: "FraMeSToR"}
		c := quality.Condition{
			Type:     quality.ConditionReleaseGroup,
			Pattern:  `(?i)^framestor$`,
			Required: true,
		}
		Expect(mk(c).Matches(r)).To(BeTrue())

		r2 := quality.ReleaseContext{Group: "OTHER"}
		Expect(mk(c).Matches(r2)).To(BeFalse())
	})

	It("matches size within an inclusive [MinGB, MaxGB] band", func() {
		c := quality.Condition{
			Type:     quality.ConditionSize,
			MinGB:    4,
			MaxGB:    10,
			Required: true,
		}
		f := mk(c)
		Expect(f.Matches(quality.ReleaseContext{Size: 8 << 30})).To(BeTrue())
		Expect(f.Matches(quality.ReleaseContext{Size: 2 << 30})).To(BeFalse())
		Expect(f.Matches(quality.ReleaseContext{Size: 20 << 30})).To(BeFalse())
	})

	It("treats a zero MinGB/MaxGB as unbounded on that side", func() {
		lower := mk(
			quality.Condition{Type: quality.ConditionSize, MinGB: 4, Required: true},
		)
		Expect(lower.Matches(quality.ReleaseContext{Size: 100 << 30})).To(BeTrue())
		Expect(lower.Matches(quality.ReleaseContext{Size: 1 << 30})).To(BeFalse())

		upper := mk(
			quality.Condition{
				Type:     quality.ConditionSize,
				MaxGB:    10,
				Required: true,
			},
		)
		Expect(upper.Matches(quality.ReleaseContext{Size: 1 << 30})).To(BeTrue())
		Expect(upper.Matches(quality.ReleaseContext{Size: 20 << 30})).To(BeFalse())
	})

	It("requires seeders >= Min only when HasSeeders", func() {
		c := quality.Condition{
			Type:     quality.ConditionSeeders,
			Min:      10,
			Required: true,
		}
		f := mk(c)
		Expect(
			f.Matches(quality.ReleaseContext{Seeders: 10, HasSeeders: true}),
		).To(BeTrue())
		Expect(
			f.Matches(quality.ReleaseContext{Seeders: 9, HasSeeders: true}),
		).To(BeFalse())
		Expect(
			f.Matches(quality.ReleaseContext{Seeders: 50, HasSeeders: false}),
		).To(BeFalse())
	})
})

var _ = Describe("NewFormat validation", Label("unit", "quality"), func() {
	It("errors on an uncompilable pattern", func() {
		_, err := quality.NewFormat("t", []quality.Condition{{
			Type: quality.ConditionReleaseTitle, Pattern: "(unterminated",
		}})
		Expect(err).To(HaveOccurred())
	})

	It("errors on an unknown condition type", func() {
		_, err := quality.NewFormat("t", []quality.Condition{{
			Type: quality.ConditionType("bogus"),
		}})
		Expect(err).To(HaveOccurred())
	})

	It("errors on a regex type with an empty pattern", func() {
		_, err := quality.NewFormat("t", []quality.Condition{{
			Type: quality.ConditionReleaseTitle,
		}})
		Expect(err).To(HaveOccurred())

		_, err = quality.NewFormat("t", []quality.Condition{{
			Type: quality.ConditionReleaseGroup,
		}})
		Expect(err).To(HaveOccurred())
	})

	It("errors on a resolution condition whose Value isn't a known bucket", func() {
		_, err := quality.NewFormat("t", []quality.Condition{{
			Type: quality.ConditionResolution, Value: "4000p",
		}})
		Expect(err).To(HaveOccurred())
	})
})
