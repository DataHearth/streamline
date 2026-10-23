package quality_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/quality"
)

var _ = Describe("AudioTierOf", Label("unit", "quality"), func() {
	DescribeTable(
		"classifies facts",
		func(f quality.AudioFacts, want quality.AudioTier, wantOK bool) {
			got, ok := quality.AudioTierOf(f)
			Expect(ok).To(Equal(wantOK))
			Expect(got).To(Equal(want))
		},
		Entry(
			"16-bit flac",
			quality.AudioFacts{Codec: "flac", Lossless: true, BitDepth: 16},
			quality.TierLossless,
			true,
		),
		Entry(
			"flac of unknown depth is lossless, never hires",
			quality.AudioFacts{
				Codec:    "flac",
				Lossless: true,
			},
			quality.TierLossless,
			true,
		),
		Entry(
			"24-bit flac",
			quality.AudioFacts{Codec: "flac", Lossless: true, BitDepth: 24},
			quality.TierHiRes,
			true,
		),
		Entry("mp3 320", quality.AudioFacts{Codec: "mp3", BitrateKbps: 320},
			quality.TierHigh, true),
		Entry("mp3 V0", quality.AudioFacts{Codec: "mp3", VBR: "V0"},
			quality.TierHigh, true),
		Entry("aac 256", quality.AudioFacts{Codec: "aac", BitrateKbps: 256},
			quality.TierHigh, true),
		Entry(
			"mp3 256 is standard",
			quality.AudioFacts{Codec: "mp3", BitrateKbps: 256},
			quality.TierStandard,
			true,
		),
		Entry("mp3 V2", quality.AudioFacts{Codec: "mp3", VBR: "V2"},
			quality.TierStandard, true),
		Entry("mp3 192", quality.AudioFacts{Codec: "mp3", BitrateKbps: 192},
			quality.TierStandard, true),
		Entry("aac 128 is low", quality.AudioFacts{Codec: "aac", BitrateKbps: 128},
			quality.TierLow, true),
		Entry(
			"nothing stated",
			quality.AudioFacts{Codec: "mp3"},
			quality.AudioTier(0),
			false,
		),
	)

	It("round-trips tier names", func() {
		for _, name := range []string{"hires", "lossless", "high", "standard", "low"} {
			t, ok := quality.ParseAudioTier(name)
			Expect(ok).To(BeTrue(), name)
			Expect(t.String()).To(Equal(name))
		}
		_, ok := quality.ParseAudioTier("other")
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("MusicProfile", Label("unit", "quality"), func() {
	profile := quality.MusicProfile{
		Tiers: []quality.AudioTier{
			quality.TierHiRes,
			quality.TierLossless,
			quality.TierHigh,
		},
		Preferred:      quality.TierLossless,
		UpgradeAllowed: true,
	}

	It("ranks best tier first and rejects an unticked one", func() {
		Expect(profile.Score(quality.TierHiRes, 0)).
			To(BeNumerically(">", profile.Score(quality.TierLossless, 99)))
		Expect(profile.Score(quality.TierStandard, 0)).To(Equal(-1))
	})

	It("ranks mp3 320 above V0 within a tier", func() {
		cbr := quality.AudioFine(quality.AudioFacts{Codec: "mp3", BitrateKbps: 320})
		v0 := quality.AudioFine(quality.AudioFacts{Codec: "mp3", VBR: "V0"})
		Expect(profile.Score(quality.TierHigh, cbr)).
			To(BeNumerically(">", profile.Score(quality.TierHigh, v0)))
	})

	It("ranks a higher sample rate above a lower one", func() {
		hi := quality.AudioFine(
			quality.AudioFacts{Lossless: true, BitDepth: 24, SampleRateHz: 192000},
		)
		mid := quality.AudioFine(
			quality.AudioFacts{Lossless: true, BitDepth: 24, SampleRateHz: 96000},
		)
		Expect(hi).To(BeNumerically(">", mid))
	})

	DescribeTable(
		"Replaces",
		func(p quality.MusicProfile, have quality.AudioTier, known bool, in quality.AudioTier, want bool) {
			Expect(p.Replaces(have, known, in)).To(Equal(want))
		},
		Entry("better tier below the ceiling", profile,
			quality.TierHigh, true, quality.TierLossless, true),
		Entry("held tier at the ceiling", profile,
			quality.TierLossless, true, quality.TierHiRes, false),
		Entry("held tier unknown", profile,
			quality.TierHigh, false, quality.TierLossless, false),
		Entry("incoming not ticked", quality.MusicProfile{
			Tiers: []quality.AudioTier{
				quality.TierHigh,
				quality.TierLossless,
			},
			Preferred:      quality.TierHiRes,
			UpgradeAllowed: true,
		}, quality.TierHigh, true, quality.TierStandard, false),
		Entry("incoming not better", profile,
			quality.TierHigh, true, quality.TierHigh, false),
		Entry("upgrades off", quality.MusicProfile{
			Tiers: profile.Tiers, Preferred: profile.Preferred,
		}, quality.TierHigh, true, quality.TierLossless, false),
	)
})
