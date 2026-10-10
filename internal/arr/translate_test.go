package arr

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MapRoot", Label("unit", "arr"), func() {
	roots := []RootMapping{
		{From: "/media/movies", To: "/srv/films"},
		{From: "/media/movies-4k", To: "/srv/films-4k"},
	}

	It("takes the longest matching prefix, not the first", func() {
		got, ok := MapRoot("/media/movies-4k/Dune/Dune.mkv", roots)
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal("/srv/films-4k/Dune/Dune.mkv"))
	})

	It("does not let a shorter root swallow a longer sibling", func() {
		got, ok := MapRoot("/media/movies/Dune/Dune.mkv", roots)
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal("/srv/films/Dune/Dune.mkv"))
	})

	It("prefers the deeper of two nested roots", func() {
		got, ok := MapRoot("/media/movies/kids/Up.mkv", []RootMapping{
			{From: "/media/movies", To: "/a"},
			{From: "/media/movies/kids", To: "/b"},
		})
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal("/b/Up.mkv"))
	})

	It("maps the root itself", func() {
		got, ok := MapRoot("/media/movies/", roots)
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal("/srv/films"))
	})

	It("leaves a path under no mapping verbatim", func() {
		got, ok := MapRoot("/elsewhere/Dune.mkv", roots)
		Expect(ok).To(BeFalse())
		Expect(got).To(Equal("/elsewhere/Dune.mkv"))
	})

	It("is the identity when from and to are equal", func() {
		got, ok := MapRoot("/media/movies/a.mkv",
			[]RootMapping{{From: "/media/movies", To: "/media/movies"}})
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal("/media/movies/a.mkv"))
	})

	It("maps a Windows source onto a path of this host", func() {
		got, ok := MapRoot(`D:\Movies\Heat (1995)\Heat.mkv`,
			[]RootMapping{{From: `D:\Movies\`, To: "/data/movies"}})
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal("/data/movies/Heat (1995)/Heat.mkv"))
	})

	It("maps a UNC source onto a path of this host", func() {
		got, ok := MapRoot(`\\nas\media\Movies\Heat.mkv`,
			[]RootMapping{{From: `\\nas\media\Movies`, To: "/data/movies"}})
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal("/data/movies/Heat.mkv"))
	})

	It("keeps a backslash in a Unix file name", func() {
		got, ok := MapRoot(`/media/movies/AC\DC Live.mkv`, roots)
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal(`/srv/films/AC\DC Live.mkv`))
	})
})

var _ = Describe("IsAbsSourcePath", Label("unit", "arr"), func() {
	It("accepts absolute paths in either style and nothing relative", func() {
		Expect(IsAbsSourcePath("/movies")).To(BeTrue())
		Expect(IsAbsSourcePath(`D:\Movies`)).To(BeTrue())
		Expect(IsAbsSourcePath(`\\nas\share`)).To(BeTrue())
		Expect(IsAbsSourcePath("movies")).To(BeFalse())
		Expect(IsAbsSourcePath(`Movies\x`)).To(BeFalse())
	})
})

var _ = Describe("FieldValue", Label("unit", "arr"), func() {
	fields := []Field{
		{Name: "baseUrl", Value: "https://idx.example.com", Privacy: "normal"},
		{Name: "port", Value: float64(8080), Privacy: "normal"},
		{Name: "apiKey", Value: "", Privacy: "apiKey"},
		{Name: "useSsl", Value: true, Privacy: "normal"},
	}

	It("reads a string field", func() {
		v, ok := FieldValue(fields, "baseUrl")
		Expect(ok).To(BeTrue())
		Expect(v).To(Equal("https://idx.example.com"))
	})

	It("renders a JSON number without a decimal tail", func() {
		v, ok := FieldValue(fields, "port")
		Expect(ok).To(BeTrue())
		Expect(v).To(Equal("8080"))
	})

	It("renders a bool", func() {
		v, ok := FieldValue(fields, "useSsl")
		Expect(ok).To(BeTrue())
		Expect(v).To(Equal("true"))
	})

	It("reports an empty secret as present but blank", func() {
		v, ok := FieldValue(fields, "apiKey")
		Expect(ok).To(BeTrue())
		Expect(v).To(BeEmpty())
	})

	It("reports an absent field", func() {
		_, ok := FieldValue(fields, "nope")
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("TranslateProfile", Label("unit", "arr"), func() {
	// known stands in for the caller's builtin/custom-format resolution.
	known := func(n string) (string, bool) {
		if strings.EqualFold(n, "repack") {
			return "Repack", true
		}
		return "", false
	}

	leaf := func(id uint32, name string, res int, allowed bool) QualityItem {
		return QualityItem{
			Quality: &Quality{ID: id, Name: name, Resolution: res},
			Allowed: allowed,
		}
	}

	It("takes the band of the lowest allowed quality and the cutoff", func() {
		got, notes := TranslateProfile(QualityProfile{
			Name:              "HD-1080p",
			UpgradeAllowed:    true,
			Cutoff:            9,
			MinFormatScore:    10,
			CutoffFormatScore: 100,
			Items: []QualityItem{
				leaf(4, "HDTV-720p", 720, true),
				leaf(9, "Bluray-1080p", 1080, true),
				leaf(19, "Bluray-2160p", 2160, false),
			},
		}, known)

		Expect(got.Name).To(Equal("HD-1080p"))
		Expect(got.MinResolution).To(Equal("720p"))
		Expect(got.PreferredResolution).To(Equal("1080p"))
		Expect(got.UpgradeAllowed).To(BeTrue())
		Expect(got.MinScore).To(Equal(10))
		Expect(got.UpgradeUntilScore).To(Equal(100))
		Expect(notes).To(ContainElement(ContainSubstring("source")))
	})

	It("floors anything below 720p and says so", func() {
		got, notes := TranslateProfile(QualityProfile{
			Name:   "SD",
			Cutoff: 2,
			Items: []QualityItem{
				leaf(1, "SDTV", 480, true),
				leaf(2, "DVD", 480, true),
			},
		}, known)

		Expect(got.MinResolution).To(Equal("720p"))
		Expect(got.PreferredResolution).To(Equal("720p"))
		Expect(notes).To(ContainElement(ContainSubstring("480")))
	})

	It("says so when the ceiling refuses qualities the source allows", func() {
		// The stock "HD - 720p/1080p": 1080p allowed, cutoff at 720p.
		got, notes := TranslateProfile(QualityProfile{
			Name:   "HD - 720p/1080p",
			Cutoff: 4,
			Items: []QualityItem{
				leaf(4, "HDTV-720p", 720, true),
				leaf(9, "Bluray-1080p", 1080, true),
			},
		}, known)

		Expect(got.PreferredResolution).To(Equal("720p"))
		Expect(notes).To(ContainElement(SatisfyAll(
			ContainSubstring("1080p"),
			ContainSubstring("hard ceiling"),
		)))
	})

	It("adds no ceiling note when the cutoff is the top allowed band", func() {
		_, notes := TranslateProfile(QualityProfile{
			Name:   "HD-1080p",
			Cutoff: 9,
			Items: []QualityItem{
				leaf(4, "HDTV-720p", 720, true),
				leaf(9, "Bluray-1080p", 1080, true),
			},
		}, known)

		Expect(notes).NotTo(ContainElement(ContainSubstring("hard ceiling")))
	})

	It("resolves a cutoff that names a group to the group's top resolution", func() {
		got, _ := TranslateProfile(QualityProfile{
			Name:   "Any HD",
			Cutoff: 1000,
			Items: []QualityItem{
				{
					ID:      1000,
					Name:    "WEB 1080p",
					Allowed: true,
					Items: []QualityItem{
						leaf(15, "WEBRip-1080p", 1080, false),
						leaf(3, "WEBDL-720p", 720, false),
					},
				},
			},
		}, known)

		Expect(got.MinResolution).To(Equal("720p"))
		Expect(got.PreferredResolution).To(Equal("1080p"))
	})

	It("never yields a preferred band below the minimum", func() {
		got, _ := TranslateProfile(QualityProfile{
			Name:   "Odd",
			Cutoff: 4,
			Items: []QualityItem{
				leaf(4, "HDTV-720p", 720, false),
				leaf(9, "Bluray-1080p", 1080, true),
			},
		}, known)

		Expect(got.MinResolution).To(Equal("1080p"))
		Expect(got.PreferredResolution).To(Equal("1080p"))
	})

	It("keeps only format scores whose name resolves, and lists the rest", func() {
		got, notes := TranslateProfile(QualityProfile{
			Name:   "P",
			Cutoff: 9,
			Items:  []QualityItem{leaf(9, "Bluray-1080p", 1080, true)},
			FormatItems: []FormatItem{
				{Name: "Repack", Score: 5},
				{Name: "x265 (no HDR)", Score: -10000},
				{Name: "Unscored", Score: 0},
			},
		}, known)

		Expect(got.Formats).To(HaveLen(1))
		Expect(got.Formats[0].Name).To(Equal("Repack"))
		Expect(got.Formats[0].Score).To(Equal(5))
		Expect(notes).To(ContainElement(ContainSubstring("x265 (no HDR)")))
		Expect(notes).NotTo(ContainElement(ContainSubstring("Unscored")))
	})

	It("stores a case-folded match under streamline's spelling", func() {
		got, notes := TranslateProfile(QualityProfile{
			Name:        "P",
			Cutoff:      9,
			Items:       []QualityItem{leaf(9, "Bluray-1080p", 1080, true)},
			FormatItems: []FormatItem{{Name: "REPACK", Score: 5}},
		}, known)

		Expect(got.Formats).To(HaveLen(1))
		Expect(got.Formats[0].Name).To(Equal("Repack"))
		Expect(notes).NotTo(ContainElement(ContainSubstring("REPACK")))
	})

	It("keeps the first of two source formats resolving to one name", func() {
		got, _ := TranslateProfile(QualityProfile{
			Name:   "P",
			Cutoff: 9,
			Items:  []QualityItem{leaf(9, "Bluray-1080p", 1080, true)},
			FormatItems: []FormatItem{
				{Name: "Repack", Score: 5},
				{Name: "REPACK", Score: 50},
			},
		}, known)

		Expect(got.Formats).To(HaveLen(1))
		Expect(got.Formats[0].Score).To(Equal(5))
	})

	It("notes a minimum upgrade format score it cannot carry", func() {
		_, notes := TranslateProfile(QualityProfile{
			Name:   "P",
			Cutoff: 9,
			Items: []QualityItem{
				leaf(9, "Bluray-1080p", 1080, true),
			},
			MinUpgradeFormatScore: 50,
		}, known)

		Expect(notes).To(ContainElement(ContainSubstring("50")))
	})
})

var _ = Describe("TranslateIndexers", Label("unit", "arr"), func() {
	It("renders a plain Torznab indexer, joining its api path", func() {
		got := TranslateIndexers([]Provider{{
			Name:           "Nyaa",
			Implementation: "Torznab",
			Protocol:       "torrent",
			Priority:       25,
			EnableRSS:      true,
			Fields: []Field{
				{
					Name:  "baseUrl",
					Value: "https://jackett.lan:9117/api/v2.0/indexers/nyaa/results/torznab/",
				},
				{Name: "apiPath", Value: "/api"},
				{Name: "apiKey", Value: "abc123", Privacy: "apiKey"},
			},
		}})

		Expect(got).To(HaveLen(1))
		Expect(got[0].Kind).To(Equal(IndexerTorznab))
		Expect(got[0].Entry.Host).To(Equal("jackett.lan"))
		Expect(got[0].Entry.Port).To(Equal(uint16(9117)))
		Expect(got[0].Entry.UseSSL).To(BeTrue())
		Expect(got[0].Entry.Path).
			To(Equal("/api/v2.0/indexers/nyaa/results/torznab/api"))
		Expect(got[0].Entry.APIKey).To(Equal("abc123"))
		Expect(got[0].Entry.Protocol).To(Equal("torznab"))
		Expect(got[0].Entry.Priority).To(Equal(uint8(26)))
		Expect(got[0].Entry.Enabled).To(BeTrue())
		Expect(got[0].NeedsSecret).To(BeFalse())
	})

	It(
		"collapses prowlarr-synced indexers on one instance into a single entry",
		func() {
			prowlarr := func(name string, id int, rss bool) Provider {
				return Provider{
					Name:           name,
					Implementation: "Torznab",
					Protocol:       "torrent",
					EnableRSS:      rss,
					Fields: []Field{
						{
							Name: "baseUrl",
							Value: fmt.Sprintf(
								"http://prowlarr.lan:9696/prowlarr/%d/",
								id,
							),
						},
						{Name: "apiPath", Value: "/api"},
						{Name: "apiKey", Value: "shared", Privacy: "apiKey"},
					},
				}
			}
			got := TranslateIndexers([]Provider{
				prowlarr("TorrentLeech (Prowlarr)", 3, false),
				prowlarr("IPTorrents (Prowlarr)", 4, true),
			})

			Expect(got).To(HaveLen(1))
			Expect(got[0].Kind).To(Equal(IndexerProwlarr))
			Expect(got[0].Collapses).To(Equal(uint32(2)))
			Expect(got[0].Entry.Host).To(Equal("prowlarr.lan"))
			Expect(got[0].Entry.Port).To(Equal(uint16(9696)))
			Expect(got[0].Entry.Path).To(Equal("/prowlarr"))
			Expect(got[0].Entry.Protocol).To(Equal("prowlarr"))
			Expect(got[0].Entry.APIKey).To(Equal("shared"))
			Expect(got[0].Entry.Enabled).To(BeTrue())
		},
	)

	It("keeps two prowlarr instances apart", func() {
		mk := func(host string) Provider {
			return Provider{
				Name: "X (Prowlarr)", Implementation: "Torznab", Protocol: "torrent",
				Fields: []Field{
					{Name: "baseUrl", Value: "http://" + host + ":9696/1/"},
					{Name: "apiKey", Value: "k"},
				},
			}
		}
		got := TranslateIndexers([]Provider{mk("a"), mk("b")})
		Expect(got).To(HaveLen(2))
		// A name keys the preview row, the selection and the config entry.
		Expect(got[0].Name).To(Equal("Prowlarr"))
		Expect(got[1].Name).To(Equal("Prowlarr (2)"))
		Expect(got[1].Entry.Name).To(Equal("Prowlarr (2)"))
	})

	It("keeps a manual indexer and its prowlarr-synced twin apart", func() {
		got := TranslateIndexers([]Provider{
			{Name: "NZBgeek", Implementation: "Newznab", Protocol: "usenet"},
			{
				Name:           "NZBgeek (Prowlarr)",
				Implementation: "Newznab",
				Protocol:       "usenet",
			},
		})
		Expect(got).To(HaveLen(2))
		Expect(got[0].Name).NotTo(Equal(got[1].Name))
	})

	It("marks usenet and unknown implementations unsupported with a reason", func() {
		got := TranslateIndexers([]Provider{
			{Name: "NZBgeek", Implementation: "Newznab", Protocol: "usenet"},
			{Name: "Weird", Implementation: "SomethingElse", Protocol: "torrent"},
		})

		Expect(got).To(HaveLen(2))
		Expect(got[0].Kind).To(Equal(IndexerUnsupported))
		Expect(got[0].Reason).To(ContainSubstring("usenet"))
		Expect(got[1].Kind).To(Equal(IndexerUnsupported))
		Expect(got[1].Reason).To(ContainSubstring("SomethingElse"))
	})

	It("flags a blank or masked secret as needing one", func() {
		mk := func(key string) Provider {
			return Provider{
				Name: "Nyaa", Implementation: "Torznab", Protocol: "torrent",
				Fields: []Field{
					{Name: "baseUrl", Value: "http://h:1/api"},
					{Name: "apiKey", Value: key, Privacy: "apiKey"},
				},
			}
		}
		got := TranslateIndexers([]Provider{mk(""), mk("********")})
		Expect(got[0].NeedsSecret).To(BeTrue())
		Expect(got[1].NeedsSecret).To(BeTrue())
		Expect(got[1].Entry.APIKey).To(BeEmpty())
	})
})

var _ = Describe("TranslateDownloadClients", Label("unit", "arr"), func() {
	It("renders qbittorrent with its credentials", func() {
		got := TranslateDownloadClients([]Provider{{
			Name:           "qbit",
			Implementation: "QBittorrent",
			Protocol:       "torrent",
			Enable:         true,
			Priority:       1,
			Fields: []Field{
				{Name: "host", Value: "qbit.lan"},
				{Name: "port", Value: float64(8080)},
				{Name: "useSsl", Value: false},
				{Name: "username", Value: "admin"},
				{Name: "password", Value: "pw", Privacy: "password"},
			},
		}})

		Expect(got).To(HaveLen(1))
		Expect(got[0].Reason).To(BeEmpty())
		Expect(got[0].Entry.ClientType).To(Equal("qbittorrent"))
		Expect(got[0].Entry.Host).To(Equal("qbit.lan"))
		Expect(got[0].Entry.Port).To(Equal(uint16(8080)))
		Expect(got[0].Entry.AuthMethod).To(Equal("password"))
		Expect(got[0].Entry.Username).To(Equal("admin"))
		Expect(got[0].Entry.Password).To(Equal("pw"))
		Expect(got[0].Entry.Priority).To(Equal(uint8(50)))
		Expect(got[0].Entry.Enabled).To(BeTrue())
		Expect(got[0].NeedsSecret).To(BeFalse())
	})

	It("drops the username for deluge", func() {
		got := TranslateDownloadClients([]Provider{{
			Name: "d", Implementation: "Deluge", Protocol: "torrent",
			Fields: []Field{
				{Name: "host", Value: "deluge"},
				{Name: "port", Value: float64(8112)},
				{Name: "username", Value: "ignored"},
				{Name: "password", Value: "********", Privacy: "password"},
			},
		}})
		Expect(got[0].Entry.ClientType).To(Equal("deluge"))
		Expect(got[0].Entry.Username).To(BeEmpty())
		Expect(got[0].NeedsSecret).To(BeTrue())
	})

	It("rejects a usenet or unknown client with a reason", func() {
		got := TranslateDownloadClients([]Provider{
			{Name: "sab", Implementation: "Sabnzbd", Protocol: "usenet"},
			{Name: "rt", Implementation: "RTorrent", Protocol: "torrent"},
		})
		Expect(got[0].Entry.ClientType).To(BeEmpty())
		Expect(got[0].Reason).To(ContainSubstring("usenet"))
		Expect(got[1].Reason).To(ContainSubstring("RTorrent"))
	})

	It("carries a client without authentication as it is", func() {
		// The *arr apps mask a set password and return an unset one empty.
		got := TranslateDownloadClients([]Provider{{
			Name: "tr", Implementation: "Transmission", Protocol: "torrent",
			Fields: []Field{
				{Name: "host", Value: "transmission"},
				{Name: "port", Value: float64(9091)},
				{Name: "urlBase", Value: "/transmission/"},
				{Name: "username", Value: ""},
				{Name: "password", Value: "", Privacy: "password"},
			},
		}})
		Expect(got[0].Reason).To(BeEmpty())
		Expect(got[0].NeedsSecret).To(BeFalse())
		Expect(got[0].Entry.Password).To(BeEmpty())
	})

	It("refuses a client reached under a URL base", func() {
		got := TranslateDownloadClients([]Provider{{
			Name: "qbit", Implementation: "QBittorrent", Protocol: "torrent",
			Fields: []Field{
				{Name: "host", Value: "proxy.lan"},
				{Name: "port", Value: float64(443)},
				{Name: "useSsl", Value: true},
				{Name: "urlBase", Value: "/qbittorrent"},
			},
		}})
		Expect(got[0].Reason).To(ContainSubstring("/qbittorrent"))
		Expect(got[0].Entry.ClientType).To(BeEmpty())
	})
})
