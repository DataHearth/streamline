package metadata

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("mbURLIDs", Label("unit", "metadata"), func() {
	rel := func(u string) mbRel {
		return mbRel{URL: &struct {
			Resource string `json:"resource"`
		}{Resource: u}}
	}

	It("reads an artist's ids out of its url relations", func() {
		out := map[string]string{}
		mbURLIDs(out, []mbRel{
			rel("https://www.discogs.com/artist/3840"),
			rel("https://www.allmusic.com/artist/radiohead-mn0000326249"),
			rel("https://open.spotify.com/artist/4Z8W4fKeB5YxbusRsdQVPb"),
			rel("https://radiohead.bandcamp.com/"),
			rel("https://www.last.fm/music/Radiohead"),
			rel("https://rateyourmusic.com/artist/radiohead"),
			rel("https://www.wikidata.org/wiki/Q44190"),
		}, mbArtistURLs)
		Expect(out).To(Equal(map[string]string{
			SourceDiscogs:       "3840",
			SourceAllMusic:      "mn0000326249",
			SourceSpotify:       "4Z8W4fKeB5YxbusRsdQVPb",
			SourceBandcamp:      "radiohead.bandcamp.com",
			SourceLastFM:        "Radiohead",
			SourceRateYourMusic: "radiohead",
		}))
	})

	It("reads an album's ids from its release group, then its release", func() {
		out := map[string]string{}
		mbURLIDs(out, []mbRel{
			rel("https://www.wikidata.org/wiki/Q213434"),
			rel("https://www.discogs.com/master/21491"),
			rel("https://www.allmusic.com/album/ok-computer-mw0000024289"),
			rel("https://rateyourmusic.com/release/album/radiohead/ok_computer/"),
		}, mbReleaseGroupURLs)
		mbURLIDs(out, []mbRel{
			rel("https://open.spotify.com/album/6dVIqQ8qmQ5GBnJ9shOYGE"),
			rel("https://www.deezer.com/album/1422436"),
			rel("https://radiohead.bandcamp.com/album/ok-computer"),
			// A release's own Discogs page is not the album's master.
			rel("https://www.discogs.com/release/1234"),
		}, mbReleaseURLs)
		Expect(out).To(Equal(map[string]string{
			SourceWikidata:      "Q213434",
			SourceDiscogs:       "21491",
			SourceAllMusic:      "mw0000024289",
			SourceRateYourMusic: "album/radiohead/ok_computer",
			SourceSpotify:       "6dVIqQ8qmQ5GBnJ9shOYGE",
			SourceDeezer:        "1422436",
			SourceBandcamp:      "radiohead.bandcamp.com/album/ok-computer",
		}))
	})

	It("keeps the first link per site and skips an ended one", func() {
		out := map[string]string{}
		ended := rel("https://www.discogs.com/artist/1")
		ended.Ended = true
		mbURLIDs(out, []mbRel{
			ended,
			rel("https://www.discogs.com/artist/2"),
			rel("https://www.discogs.com/artist/3"),
			{Type: "member of band"},
		}, mbArtistURLs)
		Expect(out).To(Equal(map[string]string{SourceDiscogs: "2"}))
	})
})
