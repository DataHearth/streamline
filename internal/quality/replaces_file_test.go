package quality_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/quality"
)

// animeProfile is the shape that made this necessary, taken from a real
// install: almost all of the weight sits in vostfr, and upgrade_until_score is
// set just above what a release can score.
func animeProfile() quality.Profile {
	vostfr := mustFmt("vostfr",
		quality.Condition{
			Type:    quality.ConditionReleaseTitle,
			Pattern: `(?i)\bvostfr\b`,
		},
		quality.Condition{
			Type:  quality.ConditionSubtitleLanguage,
			Value: "fra",
		})
	x265 := mustFmt("x265", quality.Condition{
		Type: quality.ConditionCodec, Value: "hevc",
	})
	return quality.Profile{
		MinResolution: "720p", MaxResolution: "1080p",
		UpgradeAllowed: true, UpgradeUntilScore: 1200,
		Formats: []quality.ScoredFormat{
			{Format: vostfr, Score: 1000},
			{Format: x265, Score: 80},
		},
	}
}

func mustFmt(name string, conds ...quality.Condition) quality.Format {
	GinkgoHelper()
	f, err := quality.NewFormat(name, conds)
	Expect(err).NotTo(HaveOccurred())
	return f
}

// probedRow is a file the library holds: a renamed basename that says nothing,
// plus the columns the probe filled in.
func probedRow(subLangs []string) quality.ReleaseContext {
	tracks := 1
	return quality.ReleaseContext{
		Title:          "Show - S01E01 - Title [].mkv",
		Resolution:     "1080p",
		Codec:          "hevc",
		EmptyIsUnknown: true,
		AudioTracks:    &tracks,
		AudioLangs:     []string{"jpn"},
		SubLangs:       subLangs,
	}
}

var _ = Describe("ReplacesFile", Label("unit", "quality"), func() {
	p := animeProfile()

	incoming := quality.ReleaseContext{
		Title:      "Show.S01E01.VOSTFR.1080p.WEB.x265-GRP",
		Resolution: "1080p",
		Codec:      "hevc",
		SubLangs:   []string{"fra"},
	}

	It(
		"does not upgrade a file that already carries what the release claims",
		func() {
			// The bug this exists to prevent: the file's own name was destroyed by
			// the renamer, so before the probe could answer vostfr it scored 80
			// against the release's 1080 and every episode in the library read as
			// upgradable, forever, by any VOSTFR release.
			Expect(quality.ReplacesFile(p, probedRow([]string{"fra"}), incoming)).
				To(BeFalse())
		},
	)

	It("still upgrades a file that genuinely lacks the French subtitles", func() {
		Expect(quality.ReplacesFile(p, probedRow([]string{}), incoming)).
			To(BeTrue())
	})

	It("drops a format the file cannot answer from BOTH sides", func() {
		// repack is written only in release_title, which a row can never
		// answer. Scoring it on the release alone is the same rigged
		// comparison in miniature.
		withRepack := p
		withRepack.Formats = append(append(
			[]quality.ScoredFormat{}, p.Formats...),
			quality.ScoredFormat{
				Format: mustFmt("repack", quality.Condition{
					Type:    quality.ConditionReleaseTitle,
					Pattern: `(?i)\brepack\b`,
				}),
				Score: 500,
			})
		repacked := incoming
		repacked.Title = "Show.S01E01.REPACK.VOSTFR.1080p.WEB.x265-GRP"

		Expect(
			quality.ReplacesFile(withRepack, probedRow([]string{"fra"}), repacked),
		).
			To(BeFalse())
	})

	It("leaves an unprobed file comparable on what its columns do say", func() {
		// ffmpeg off: no stream fields at all, so vostfr drops from both sides
		// and the release cannot win on it. The codec column still counts.
		unprobed := quality.ReleaseContext{
			Title:          "Show - S01E01 - Title [].mkv",
			Resolution:     "1080p",
			Codec:          "h264",
			EmptyIsUnknown: true,
		}
		Expect(quality.ReplacesFile(p, unprobed, incoming)).To(BeTrue())
	})
})
