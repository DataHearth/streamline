package config_test

import (
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
