package subsonic

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// xmlNode is any element, kept as a tree so a spec can look for children
// without a struct per response.
type xmlNode struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Text     string     `xml:",chardata"`
	Children []xmlNode  `xml:",any"`
}

func (n xmlNode) attr(name string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func (n xmlNode) find(name string) *xmlNode {
	for i := range n.Children {
		if n.Children[i].XMLName.Local == name {
			return &n.Children[i]
		}
		if found := n.Children[i].find(name); found != nil {
			return found
		}
	}
	return nil
}

// XML is the API's default format and what DSub and Ultrasonic ask for, so
// every read endpoint is fetched without f=json against a populated library.
var _ = g.Describe("XML responses", g.Label("integration"), func() {
	var f *fixture

	g.BeforeEach(func() {
		ctx := context.Background()
		f = newFixture(ctx)
		dir := g.GinkgoT().TempDir()
		path := filepath.Join(dir, "01.flac")
		Expect(os.WriteFile(path, []byte("audio"), 0o600)).To(Succeed())
		Expect(
			os.WriteFile(
				filepath.Join(dir, "01.lrc"),
				[]byte("[00:01.00]Hello & <you>\n"),
				0o600,
			),
		).
			To(Succeed())
		Expect(f.client.MediaFile.Update().SetPath(path).Exec(ctx)).To(Succeed())
		Expect(
			f.client.Artist.UpdateOneID(f.artist.ID).
				SetGenre("Grunge").
				SetOverview("Bio & more").
				Exec(ctx),
		).
			To(Succeed())
		admin := f.addUser("admin@example.com", "admin")

		okResp(
			f.call(
				"star",
				url.Values{"id": {tr(f.withFile)}, "albumId": {al(f.album)}},
			),
		)
		okResp(f.call("scrobble", url.Values{"id": {tr(f.withFile)}}))
		okResp(
			f.call(
				"scrobble",
				url.Values{"id": {tr(f.withFile)}, "submission": {"false"}},
			),
		)
		okResp(
			f.call(
				"createPlaylist",
				url.Values{"name": {"Mix"}, "songId": {tr(f.withFile)}},
			),
		)
		okResp(f.call("createShare", url.Values{"id": {al(f.album)}}))
		okResp(
			f.call(
				"createBookmark",
				url.Values{"id": {tr(f.withFile)}, "position": {"1000"}},
			),
		)
		okResp(f.call("savePlayQueue", url.Values{"id": {tr(f.withFile)}}))
		okResp(f.callAs(admin.Email, "createInternetRadioStation", url.Values{
			"name": {"FIP"}, "streamUrl": {"https://example.org/fip"},
		}))
	})

	get := func(endpoint string, extra url.Values) xmlNode {
		g.GinkgoHelper()
		q := signedQuery("sesame", extra)
		q.Del("f")
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(
			rec,
			httptest.NewRequest(http.MethodGet, "/"+endpoint+"?"+q.Encode(), nil),
		)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Header().Get("Content-Type")).To(HavePrefix("text/xml"), endpoint)
		var root xmlNode
		Expect(
			xml.Unmarshal(rec.Body.Bytes(), &root),
		).To(Succeed(), endpoint+": "+rec.Body.String())
		Expect(root.XMLName.Local).To(Equal("subsonic-response"))
		Expect(root.XMLName.Space).To(Equal("http://subsonic.org/restapi"))
		Expect(root.attr("status")).To(Equal("ok"), endpoint+": "+rec.Body.String())
		Expect(root.attr("openSubsonic")).To(Equal("true"))
		return root
	}

	g.DescribeTable(
		"renders every read endpoint",
		func(endpoint string, params url.Values, element, child string) {
			root := get(endpoint, params)
			el := root.find(element)
			Expect(el).NotTo(BeNil(), "no <%s> in %s", element, endpoint)
			if child != "" {
				Expect(
					el.find(child),
				).NotTo(BeNil(), "no <%s> under <%s> in %s", child, element, endpoint)
			}
		},
		g.Entry(nil, "getMusicFolders", nil, "musicFolders", "musicFolder"),
		g.Entry(nil, "getIndexes", nil, "indexes", "artist"),
		g.Entry(nil, "getArtists", nil, "artists", "artist"),
		g.Entry(
			nil,
			"getMusicDirectory",
			url.Values{"id": {"ar-1"}},
			"directory",
			"child",
		),
		g.Entry(nil, "getGenres", nil, "genres", "genre"),
		g.Entry(nil, "getArtist", url.Values{"id": {"ar-1"}}, "artist", "album"),
		g.Entry(nil, "getAlbum", url.Values{"id": {"al-1"}}, "album", "song"),
		g.Entry(nil, "getSong", url.Values{"id": {"tr-1"}}, "song", ""),
		g.Entry(
			nil,
			"getArtistInfo",
			url.Values{"id": {"ar-1"}},
			"artistInfo",
			"biography",
		),
		g.Entry(
			nil,
			"getArtistInfo2",
			url.Values{"id": {"ar-1"}},
			"artistInfo2",
			"musicBrainzId",
		),
		g.Entry(
			nil,
			"getAlbumInfo",
			url.Values{"id": {"al-1"}},
			"albumInfo",
			"musicBrainzId",
		),
		g.Entry(nil, "getAlbumInfo2", url.Values{"id": {"al-1"}}, "albumInfo", ""),
		g.Entry(
			nil,
			"getSimilarSongs",
			url.Values{"id": {"ar-1"}},
			"similarSongs",
			"song",
		),
		g.Entry(
			nil,
			"getSimilarSongs2",
			url.Values{"id": {"ar-1"}},
			"similarSongs2",
			"song",
		),
		g.Entry(
			nil,
			"getTopSongs",
			url.Values{"artist": {"Nirvana"}},
			"topSongs",
			"song",
		),
		g.Entry(
			nil,
			"getAlbumList",
			url.Values{"type": {"frequent"}},
			"albumList",
			"album",
		),
		g.Entry(
			nil,
			"getAlbumList2",
			url.Values{"type": {"starred"}},
			"albumList2",
			"album",
		),
		g.Entry(nil, "getRandomSongs", nil, "randomSongs", "song"),
		g.Entry(
			nil,
			"getSongsByGenre",
			url.Values{"genre": {"Grunge"}},
			"songsByGenre",
			"song",
		),
		g.Entry(nil, "getNowPlaying", nil, "nowPlaying", "entry"),
		g.Entry(nil, "getStarred", nil, "starred", "song"),
		g.Entry(nil, "getStarred2", nil, "starred2", "album"),
		g.Entry(
			nil,
			"search",
			url.Values{"any": {"smells"}},
			"searchResult",
			"match",
		),
		g.Entry(
			nil,
			"search2",
			url.Values{"query": {"nirv"}},
			"searchResult2",
			"artist",
		),
		g.Entry(
			nil,
			"search3",
			url.Values{"query": {"smells"}},
			"searchResult3",
			"song",
		),
		g.Entry(nil, "getPlaylists", nil, "playlists", "playlist"),
		g.Entry(nil, "getPlaylist", url.Values{"id": {"pl-1"}}, "playlist", "entry"),
		g.Entry(nil, "getShares", nil, "shares", "entry"),
		g.Entry(nil, "getBookmarks", nil, "bookmarks", "entry"),
		g.Entry(nil, "getPlayQueue", nil, "playQueue", "entry"),
		g.Entry(nil, "getPlayQueueByIndex", nil, "playQueueByIndex", "entry"),
		g.Entry(
			nil,
			"getInternetRadioStations",
			nil,
			"internetRadioStations",
			"internetRadioStation",
		),
		g.Entry(nil, "getScanStatus", nil, "scanStatus", ""),
		g.Entry(
			nil,
			"getUser",
			url.Values{"username": {"listener@example.com"}},
			"user",
			"folder",
		),
		g.Entry(
			nil,
			"getLyrics",
			url.Values{"title": {"Smells Like Teen Spirit"}},
			"lyrics",
			"",
		),
		g.Entry(
			nil,
			"getLyricsBySongId",
			url.Values{"id": {"tr-1"}},
			"lyricsList",
			"line",
		),
		g.Entry(nil, "getLicense", nil, "license", ""),
	)

	g.It("puts text-content values in the element body, escaped", func() {
		gen := get("getGenres", nil).find("genre")
		Expect(strings.TrimSpace(gen.Text)).To(Equal("Grunge"))
		Expect(gen.attr("songCount")).To(Equal("1"))

		bio := get("getArtistInfo2", url.Values{"id": {"ar-1"}}).find("biography")
		Expect(bio.Text).To(Equal("Bio & more"))

		line := get("getLyricsBySongId", url.Values{"id": {"tr-1"}}).find("line")
		Expect(line.Text).To(Equal("Hello & <you>"))
		Expect(line.attr("start")).To(Equal("1000"))

		song := get("getSong", url.Values{"id": {"tr-1"}}).find("song")
		Expect(song.attr("starred")).NotTo(BeEmpty())
		Expect(song.attr("playCount")).To(Equal("1"))
		Expect(song.attr("genre")).To(Equal("Grunge"))
	})

	g.It("lists the OpenSubsonic extensions as repeated elements", func() {
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(
			rec,
			httptest.NewRequest(http.MethodGet, "/getOpenSubsonicExtensions", nil),
		)
		var root xmlNode
		Expect(xml.Unmarshal(rec.Body.Bytes(), &root)).To(Succeed())
		names := make([]string, 0, len(root.Children))
		for _, c := range root.Children {
			Expect(c.XMLName.Local).To(Equal("openSubsonicExtensions"))
			Expect(c.find("versions")).NotTo(BeNil())
			names = append(names, c.attr("name"))
		}
		Expect(names).To(ContainElement("songLyrics"))
	})

	g.It("answers an error envelope in XML", func() {
		q := signedQuery("sesame", url.Values{"id": {"al-999"}})
		q.Del("f")
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(
			rec,
			httptest.NewRequest(http.MethodGet, "/getAlbum?"+q.Encode(), nil),
		)
		var root xmlNode
		Expect(xml.Unmarshal(rec.Body.Bytes(), &root)).To(Succeed())
		Expect(root.attr("status")).To(Equal("failed"))
		Expect(root.find("error").attr("code")).To(Equal("70"))
	})
})
