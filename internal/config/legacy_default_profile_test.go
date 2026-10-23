package config

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe(
	"The legacy quality_default_profile key",
	Label("unit", "config"),
	func() {
		profiles := []map[string]any{
			{
				"name":                 "hd",
				"preferred_resolution": "1080p",
				"min_resolution":       "720p",
			},
			{
				"name":                 "uhd",
				"preferred_resolution": "2160p",
				"min_resolution":       "2160p",
			},
		}

		BeforeEach(func() {
			ResetForTest()
			DeferCleanup(ResetForTest)
		})

		It("fills both new keys when it is the only one set", func() {
			path := writeConfigFile(map[string]any{
				"quality_profiles":        profiles,
				"quality_default_profile": "uhd",
			})
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())

			Expect(Get().MovieQualityDefaultProfile).To(Equal("uhd"))
			Expect(Get().SeriesQualityDefaultProfile).To(Equal("uhd"))
		})

		It("is not written back by the next update", func() {
			path := writeConfigFile(map[string]any{
				"quality_profiles":        profiles,
				"quality_default_profile": "uhd",
			})
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())

			Expect(Update(context.Background(), func(c *Config) error {
				c.MediaServer.PlexClientID = "plex-id"
				return nil
			})).To(Succeed())

			d := onDisk(path)
			Expect(d.Exists("quality_default_profile")).To(BeFalse())
			Expect(d.String("movie_quality_default_profile")).To(Equal("uhd"))
			Expect(d.String("series_quality_default_profile")).To(Equal("uhd"))
		})

		It("loads the new keys on their own", func() {
			path := writeConfigFile(map[string]any{
				"quality_profiles":               profiles,
				"movie_quality_default_profile":  "uhd",
				"series_quality_default_profile": "hd",
			})
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())

			Expect(Get().MovieQualityDefaultProfile).To(Equal("uhd"))
			Expect(Get().SeriesQualityDefaultProfile).To(Equal("hd"))
		})

		It("yields to a new key set beside it", func() {
			path := writeConfigFile(map[string]any{
				"quality_profiles":               profiles,
				"quality_default_profile":        "uhd",
				"series_quality_default_profile": "hd",
			})
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())

			Expect(Get().MovieQualityDefaultProfile).To(Equal("uhd"))
			Expect(Get().SeriesQualityDefaultProfile).To(Equal("hd"))
		})

		It("is honoured from the environment and kept off disk", func() {
			GinkgoT().Setenv("STREAMLINE_QUALITY_DEFAULT_PROFILE", "uhd")
			path := writeConfigFile(map[string]any{"quality_profiles": profiles})
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(Get().MovieQualityDefaultProfile).To(Equal("uhd"))
			Expect(Get().SeriesQualityDefaultProfile).To(Equal("uhd"))

			Expect(Update(context.Background(), func(c *Config) error {
				c.MediaServer.PlexClientID = "plex-id"
				return nil
			})).To(Succeed())

			d := onDisk(path)
			Expect(d.Exists("quality_default_profile")).To(BeFalse())
			Expect(d.Exists("movie_quality_default_profile")).To(BeFalse())
			Expect(d.Exists("series_quality_default_profile")).To(BeFalse())
		})

		It("lets the new keys be set from the environment", func() {
			GinkgoT().Setenv("STREAMLINE_MOVIE_QUALITY_DEFAULT_PROFILE", "uhd")
			GinkgoT().Setenv("STREAMLINE_SERIES_QUALITY_DEFAULT_PROFILE", "hd")
			path := writeConfigFile(map[string]any{"quality_profiles": profiles})
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())

			Expect(Get().MovieQualityDefaultProfile).To(Equal("uhd"))
			Expect(Get().SeriesQualityDefaultProfile).To(Equal("hd"))
		})
	},
)
