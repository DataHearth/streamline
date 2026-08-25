package config_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Resource entries", Label("unit", "config"), func() {
	It("loads media servers, download clients, indexers, quality profiles", func() {
		c := configtest.Setup(map[string]any{
			"media_server": map[string]any{
				"servers": []map[string]any{
					{
						"name":        "home-plex",
						"server_type": "plex",
						"host":        "http://plex:32400",
						"api_key":     "tok",
					},
				},
			},
			"download_clients": []map[string]any{
				{
					"name":        "qbit",
					"client_type": "qbittorrent",
					"host":        "qbit",
					"port":        8080,
					"auth_method": "password",
				},
			},
			"indexers": []map[string]any{
				{
					"name":     "torznab1",
					"host":     "idx",
					"port":     9117,
					"api_key":  "k",
					"protocol": "torznab",
				},
			},
			"quality_profiles": []map[string]any{
				{
					"name":                 "hd",
					"preferred_resolution": "1080p",
					"min_resolution":       "720p",
				},
			},
			"quality_default_profile": "hd",
		})
		Expect(c.MediaServer.Servers).To(HaveLen(1))
		Expect(c.MediaServer.Servers[0].Name).To(Equal("home-plex"))
		Expect(c.DownloadClients).To(HaveLen(1))
		Expect(c.Indexers).To(HaveLen(1))
		Expect(c.QualityProfiles).To(HaveLen(1))
		Expect(c.QualityDefaultProfile).To(Equal("hd"))
	})
})

var _ = Describe("Library quality globals", Label("unit", "config"), func() {
	It("defaults the global quality knobs", func() {
		c := configtest.Setup()
		Expect(c.Library.NoMatchCooldown).To(Equal("6h"))
		Expect(c.Library.MaxGrabFailures).To(Equal(uint8(3)))
	})
})

var _ = Describe("FFmpeg config", Label("unit", "config"), func() {
	It("defaults ffmpeg to enabled with PATH lookup", func() {
		c := configtest.Setup()
		Expect(c.FFmpeg.Enabled).To(BeTrue())
		Expect(c.FFmpeg.Path).To(BeEmpty())
	})
})

var _ = Describe(
	"quality_default_profile validation",
	Label("unit", "config"),
	func() {
		It("rejects a default that names no existing profile", func() {
			c := configtest.Setup()
			c.QualityDefaultProfile = "missing"
			err := c.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("quality_default_profile"))
		})

		It("accepts a default that exists", func() {
			c := configtest.Setup()
			Expect(c.Validate()).To(Succeed())
		})
	},
)

var _ = Describe(
	"quality profile resolution band validation",
	Label("unit", "config"),
	func() {
		It("rejects a min_resolution above preferred_resolution", func() {
			c := configtest.Setup()
			Expect(c.QualityProfiles).NotTo(BeEmpty())
			c.QualityProfiles[0].MinResolution = "1080p"
			c.QualityProfiles[0].PreferredResolution = "720p"

			err := c.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("min_resolution"))
			Expect(err.Error()).To(ContainSubstring("preferred_resolution"))
		})

		It("accepts an equal min and preferred", func() {
			c := configtest.Setup()
			c.QualityProfiles[0].MinResolution = "1080p"
			c.QualityProfiles[0].PreferredResolution = "1080p"
			Expect(c.Validate()).To(Succeed())
		})

		It("accepts a min below preferred", func() {
			c := configtest.Setup()
			c.QualityProfiles[0].MinResolution = "720p"
			c.QualityProfiles[0].PreferredResolution = "2160p"
			Expect(c.Validate()).To(Succeed())
		})
	},
)

var _ = Describe("ResolveQualityProfile", Label("unit", "config"), func() {
	BeforeEach(func() {
		configtest.Setup(map[string]any{
			"quality_profiles": []map[string]any{
				{
					"name":                 "default",
					"preferred_resolution": "1080p",
					"min_resolution":       "720p",
					"upgrade_allowed":      true,
				},
				{
					"name":                 "uhd",
					"preferred_resolution": "2160p",
					"min_resolution":       "1080p",
				},
			},
			"quality_default_profile": "default",
		})
	})

	It("returns the named profile", func() {
		p, ok := config.ResolveQualityProfile("uhd")
		Expect(ok).To(BeTrue())
		Expect(p.PreferredResolution).To(Equal("2160p"))
	})

	It("falls back to default when empty", func() {
		p, ok := config.ResolveQualityProfile("")
		Expect(ok).To(BeTrue())
		Expect(p.Name).To(Equal("default"))
	})

	It("falls back to default when unknown", func() {
		p, ok := config.ResolveQualityProfile("nope")
		Expect(ok).To(BeTrue())
		Expect(p.Name).To(Equal("default"))
	})
})

var _ = Describe("Resource pick helpers", Label("unit", "config"), func() {
	BeforeEach(func() {
		configtest.Setup(map[string]any{
			"download_clients": []map[string]any{
				{
					"name":        "low",
					"client_type": "qbittorrent",
					"host":        "a",
					"port":        1,
					"auth_method": "password",
					"priority":    1,
					"enabled":     true,
				},
				{
					"name":        "high",
					"client_type": "qbittorrent",
					"host":        "b",
					"port":        2,
					"auth_method": "password",
					"priority":    9,
					"enabled":     true,
				},
				{
					"name":        "off",
					"client_type": "qbittorrent",
					"host":        "c",
					"port":        3,
					"auth_method": "password",
					"priority":    99,
					"enabled":     false,
				},
			},
			"indexers": []map[string]any{
				{
					"name":     "on",
					"host":     "i",
					"port":     1,
					"api_key":  "k",
					"protocol": "torznab",
					"enabled":  true,
				},
				{
					"name":     "off",
					"host":     "j",
					"port":     2,
					"api_key":  "k",
					"protocol": "torznab",
					"enabled":  false,
				},
			},
		})
	})

	It("picks the highest-priority enabled download client", func() {
		dc, ok := config.PickDownloadClient()
		Expect(ok).To(BeTrue())
		Expect(dc.Name).To(Equal("high"))
	})

	It("lists only enabled indexers", func() {
		Expect(config.EnabledIndexers()).To(HaveLen(1))
		Expect(config.EnabledIndexers()[0].Name).To(Equal("on"))
	})

	It("finds a download client by name", func() {
		dc, ok := config.FindDownloadClient("low")
		Expect(ok).To(BeTrue())
		Expect(dc.Host).To(Equal("a"))
		_, ok = config.FindDownloadClient("ghost")
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("builtin download client", Label("unit", "config"), func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		configtest.SetupFile()
	})

	It("accepts a builtin entry without host/port/auth", func() {
		err := config.AddDownloadClient(ctx, config.DownloadClientEntry{
			Name: "embedded", ClientType: "builtin",
			DownloadDir: "/downloads", Enabled: true,
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("rejects a builtin entry without download_dir", func() {
		err := config.AddDownloadClient(ctx, config.DownloadClientEntry{
			Name: "embedded", ClientType: "builtin", Enabled: true,
		})
		Expect(err).To(HaveOccurred())
	})

	It("still rejects an external entry without host", func() {
		err := config.AddDownloadClient(ctx, config.DownloadClientEntry{
			Name: "qbt", ClientType: "qbittorrent", Port: 8080,
			AuthMethod: "password", Enabled: true,
		})
		Expect(err).To(HaveOccurred())
	})

	It("rejects a second builtin entry", func() {
		Expect(config.AddDownloadClient(ctx, config.DownloadClientEntry{
			Name: "one", ClientType: "builtin", DownloadDir: "/d1",
		})).To(Succeed())
		err := config.AddDownloadClient(ctx, config.DownloadClientEntry{
			Name: "two", ClientType: "builtin", DownloadDir: "/d2",
		})
		Expect(err).To(MatchError(ContainSubstring("builtin")))
	})

	It("finds the enabled builtin entry", func() {
		Expect(config.AddDownloadClient(ctx, config.DownloadClientEntry{
			Name: "embedded", ClientType: "builtin",
			DownloadDir: "/downloads", Enabled: true,
		})).To(Succeed())
		e, ok := config.BuiltinDownloadClient()
		Expect(ok).To(BeTrue())
		Expect(e.Name).To(Equal("embedded"))
	})

	It("reports no builtin entry when the only one is disabled", func() {
		Expect(config.AddDownloadClient(ctx, config.DownloadClientEntry{
			Name: "embedded", ClientType: "builtin", DownloadDir: "/downloads",
		})).To(Succeed())
		_, ok := config.BuiltinDownloadClient()
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("Custom formats", Label("unit", "config"), func() {
	It("loads a valid custom_formats entry and finds it by name", func() {
		configtest.Setup(map[string]any{
			"custom_formats": []map[string]any{
				{
					"name": "french-vf",
					"conditions": []map[string]any{
						{
							"type":     "release_title",
							"pattern":  `(?i)\bVFF\b`,
							"required": true,
						},
					},
				},
			},
		})
		e, ok := config.FindCustomFormat("french-vf")
		Expect(ok).To(BeTrue())
		Expect(e.Conditions).To(HaveLen(1))
		Expect(e.Conditions[0].Type).To(Equal("release_title"))

		_, ok = config.FindCustomFormat("ghost")
		Expect(ok).To(BeFalse())
	})

	It("rejects duplicate custom format names", func() {
		c := configtest.Setup()
		c.CustomFormats = []config.CustomFormatEntry{
			{
				Name: "dup",
				Conditions: []config.CustomFormatConditionEntry{
					{Type: "release_title", Pattern: "a", Required: true},
				},
			},
			{
				Name: "dup",
				Conditions: []config.CustomFormatConditionEntry{
					{Type: "release_title", Pattern: "b", Required: true},
				},
			},
		}
		Expect(c.Validate()).To(HaveOccurred())
	})

	It("rejects a user format named like a built-in", func() {
		c := configtest.Setup()
		c.CustomFormats = []config.CustomFormatEntry{
			{
				Name: "x265",
				Conditions: []config.CustomFormatConditionEntry{
					{Type: "release_title", Pattern: "a", Required: true},
				},
			},
		}
		err := c.Validate()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("x265"))
	})

	It("rejects an uncompilable regex pattern", func() {
		c := configtest.Setup()
		c.CustomFormats = []config.CustomFormatEntry{
			{
				Name: "bad-regex",
				Conditions: []config.CustomFormatConditionEntry{
					{Type: "release_title", Pattern: "(", Required: true},
				},
			},
		}
		Expect(c.Validate()).To(HaveOccurred())
	})

	It("rejects a condition with an unknown type", func() {
		c := configtest.Setup()
		c.CustomFormats = []config.CustomFormatEntry{
			{
				Name: "unknown-type",
				Conditions: []config.CustomFormatConditionEntry{
					{Type: "bogus", Required: true},
				},
			},
		}
		Expect(c.Validate()).To(HaveOccurred())
	})

	It("rejects a resolution condition with a bad value", func() {
		c := configtest.Setup()
		c.CustomFormats = []config.CustomFormatEntry{
			{
				Name: "bad-res",
				Conditions: []config.CustomFormatConditionEntry{
					{Type: "resolution", Value: "8K", Required: true},
				},
			},
		}
		Expect(c.Validate()).To(HaveOccurred())
	})

	It("rejects a format with zero conditions", func() {
		c := configtest.Setup()
		c.CustomFormats = []config.CustomFormatEntry{
			{Name: "empty", Conditions: nil},
		}
		Expect(c.Validate()).To(HaveOccurred())
	})

	It(
		"rejects a profile format reference naming neither a built-in nor a user format",
		func() {
			c := configtest.Setup()
			c.QualityProfiles = []config.QualityProfileEntry{
				{
					Name:                "default",
					PreferredResolution: "1080p",
					MinResolution:       "720p",
					Formats: []config.QualityProfileFormatScore{
						{Name: "nonexistent", Score: 10},
					},
				},
			}
			c.QualityDefaultProfile = "default"
			err := c.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("nonexistent"))
		},
	)

	It(
		"still validates a config with no custom_formats or scored-profile fields",
		func() {
			c := configtest.Setup()
			Expect(c.Validate()).To(Succeed())
			Expect(c.CustomFormats).To(BeEmpty())
		},
	)
})

var _ = Describe("ResolveScoredProfile", Label("unit", "config"), func() {
	BeforeEach(func() {
		configtest.Setup(map[string]any{
			"custom_formats": []map[string]any{
				{
					"name": "french-vf",
					"conditions": []map[string]any{
						{
							"type":     "release_title",
							"pattern":  `(?i)\bVFF\b`,
							"required": true,
						},
					},
				},
			},
			"quality_profiles": []map[string]any{
				{
					"name":                 "default",
					"preferred_resolution": "1080p",
					"min_resolution":       "720p",
					"upgrade_allowed":      true,
					"min_score":            5,
					"upgrade_until_score":  100,
					"formats": []map[string]any{
						{"name": "x265", "score": 10},
						{"name": "french-vf", "score": 20},
					},
				},
			},
			"quality_default_profile": "default",
		})
	})

	It(
		"resolves min/max resolution, thresholds, and scored formats",
		func() {
			p, ok := config.ResolveScoredProfile("default")
			Expect(ok).To(BeTrue())
			Expect(p.MinResolution).To(Equal("720p"))
			Expect(p.MaxResolution).To(Equal("1080p"))
			Expect(p.UpgradeAllowed).To(BeTrue())
			Expect(p.MinScore).To(Equal(5))
			Expect(p.UpgradeUntilScore).To(Equal(100))
			Expect(p.Formats).To(HaveLen(2))

			scores := map[string]int{}
			for _, sf := range p.Formats {
				scores[sf.Format.Name] = sf.Score
			}
			Expect(scores).To(HaveKeyWithValue("x265", 10))
			Expect(scores).To(HaveKeyWithValue("french-vf", 20))
		},
	)

	It("falls back to the default profile for an unknown name", func() {
		p, ok := config.ResolveScoredProfile("nope")
		Expect(ok).To(BeTrue())
		Expect(p.MaxResolution).To(Equal("1080p"))
	})

	It("reports ok=false when no profiles are configured", func() {
		c := config.Get()
		c.QualityProfiles = nil
		c.QualityDefaultProfile = ""
		_, ok := config.ResolveScoredProfile("anything")
		Expect(ok).To(BeFalse())
	})
})
