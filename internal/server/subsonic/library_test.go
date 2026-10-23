package subsonic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

var _ = g.Describe("library", g.Label("integration"), func() {
	var f *fixture

	g.BeforeEach(func() {
		ctx := context.Background()
		f = newFixture(ctx)
		Expect(f.client.Artist.UpdateOneID(f.artist.ID).
			SetGenre("Grunge").
			SetOverview("Seattle band.").
			Exec(ctx)).To(Succeed())
	})

	g.It("announces OpenSubsonic and its extensions without credentials", func() {
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, httptest.NewRequest(
			http.MethodGet, "/getOpenSubsonicExtensions?f=json", nil,
		))
		var payload map[string]map[string]any
		Expect(json.Unmarshal(rec.Body.Bytes(), &payload)).To(Succeed())
		resp := payload["subsonic-response"]
		Expect(resp["openSubsonic"]).To(BeTrue())
		Expect(resp["type"]).To(Equal("streamline"))
		exts := list(resp, "openSubsonicExtensions")
		names := make([]string, 0, len(exts))
		for _, e := range exts {
			names = append(names, e["name"].(string))
		}
		Expect(
			names,
		).To(ContainElements("formPost", "songLyrics", "transcodeOffset", "indexBasedQueue"))

		Expect(okResp(f.call("ping", nil))["openSubsonic"]).To(BeTrue())
	})

	g.It("walks the folder view from artist to album to songs", func() {
		dir := obj(
			okResp(f.call("getMusicDirectory", url.Values{"id": {ar(f.artist)}})),
			"directory",
		)
		Expect(dir["name"]).To(Equal("Nirvana"))
		albums := list(dir, "child")
		Expect(albums).To(HaveLen(1))
		Expect(albums[0]["isDir"]).To(BeTrue())
		Expect(albums[0]["id"]).To(Equal(al(f.album)))

		dir = obj(
			okResp(f.call("getMusicDirectory", url.Values{"id": {al(f.album)}})),
			"directory",
		)
		Expect(dir["parent"]).To(Equal(ar(f.artist)))
		Expect(ids(list(dir, "child"))).To(Equal([]string{tr(f.withFile)}))

		Expect(
			errCode(f.call("getMusicDirectory", url.Values{"id": {tr(f.withFile)}})),
		).
			To(BeEquivalentTo(70))
	})

	g.It("lists genres and filters by them", func() {
		gs := list(obj(okResp(f.call("getGenres", nil)), "genres"), "genre")
		Expect(gs).To(HaveLen(1))
		Expect(gs[0]["value"]).To(Equal("Grunge"))
		Expect(gs[0]["albumCount"]).To(BeEquivalentTo(1))
		Expect(gs[0]["songCount"]).To(BeEquivalentTo(1))

		byGenre := obj(
			okResp(f.call("getSongsByGenre", url.Values{"genre": {"grunge"}})),
			"songsByGenre",
		)
		Expect(ids(list(byGenre, "song"))).To(Equal([]string{tr(f.withFile)}))
		Expect(list(byGenre, "song")[0]["genre"]).To(Equal("Grunge"))

		albums := obj(
			okResp(
				f.call(
					"getAlbumList2",
					url.Values{"type": {"byGenre"}, "genre": {"Grunge"}},
				),
			),
			"albumList2",
		)
		Expect(list(albums, "album")).To(HaveLen(1))
		albums = obj(
			okResp(
				f.call(
					"getAlbumList2",
					url.Values{"type": {"byGenre"}, "genre": {"Jazz"}},
				),
			),
			"albumList2",
		)
		Expect(list(albums, "album")).To(BeEmpty())
		Expect(
			errCode(f.call("getAlbumList2", url.Values{"type": {"byGenre"}})),
		).To(BeEquivalentTo(10))
	})

	g.It("filters album lists by year range in either direction", func() {
		for _, r := range [][2]string{{"1990", "1992"}, {"1992", "1990"}} {
			resp := obj(okResp(f.call("getAlbumList2", url.Values{
				"type": {"byYear"}, "fromYear": {r[0]}, "toYear": {r[1]},
			})), "albumList2")
			Expect(list(resp, "album")).To(HaveLen(1), r[0]+"-"+r[1])
		}
		resp := obj(okResp(f.call("getAlbumList2", url.Values{
			"type": {"byYear"}, "fromYear": {"2000"}, "toYear": {"2010"},
		})), "albumList2")
		Expect(list(resp, "album")).To(BeEmpty())
	})

	g.It("serves the folder-style album list", func() {
		resp := obj(
			okResp(
				f.call("getAlbumList", url.Values{"type": {"alphabeticalByArtist"}}),
			),
			"albumList",
		)
		albums := list(resp, "album")
		Expect(albums).To(HaveLen(1))
		Expect(albums[0]["isDir"]).To(BeTrue())
		Expect(albums[0]["parent"]).To(Equal(ar(f.artist)))
	})

	g.It("answers artist and album info", func() {
		other := f.addArtist("a-2", "Mudhoney", "Grunge")
		f.addAlbum(other, "rg-2", "Superfuzz Bigmuff", "/music/mudhoney/01.flac")
		info := obj(
			okResp(f.call("getArtistInfo2", url.Values{"id": {tr(f.withFile)}})),
			"artistInfo2",
		)
		Expect(info["biography"]).To(Equal("Seattle band."))
		Expect(info["musicBrainzId"]).To(Equal("a-1"))
		Expect(ids(list(info, "similarArtist"))).To(Equal([]string{ar(other)}))

		info = obj(
			okResp(
				f.call(
					"getArtistInfo",
					url.Values{"id": {ar(f.artist)}, "count": {"0"}},
				),
			),
			"artistInfo",
		)
		Expect(list(info, "similarArtist")).To(BeEmpty())

		album := obj(
			okResp(f.call("getAlbumInfo2", url.Values{"id": {al(f.album)}})),
			"albumInfo",
		)
		Expect(album["musicBrainzId"]).To(Equal("rg-1"))
	})

	g.It("mixes similar and random songs from playable tracks only", func() {
		for _, ep := range []string{"getSimilarSongs", "getSimilarSongs2"} {
			resp := okResp(f.call(ep, url.Values{"id": {ar(f.artist)}}))
			key := map[string]string{
				"getSimilarSongs": "similarSongs", "getSimilarSongs2": "similarSongs2",
			}[ep]
			Expect(
				ids(list(obj(resp, key), "song")),
			).To(Equal([]string{tr(f.withFile)}))
		}
		random := obj(
			okResp(f.call("getRandomSongs", url.Values{"size": {"5"}})),
			"randomSongs",
		)
		Expect(ids(list(random, "song"))).To(Equal([]string{tr(f.withFile)}))
		random = obj(
			okResp(f.call("getRandomSongs", url.Values{"fromYear": {"2000"}})),
			"randomSongs",
		)
		Expect(list(random, "song")).To(BeEmpty())
	})

	g.It("has no top songs before anyone plays", func() {
		top := obj(
			okResp(f.call("getTopSongs", url.Values{"artist": {"Nirvana"}})),
			"topSongs",
		)
		Expect(list(top, "song")).To(BeEmpty())
		Expect(errCode(f.call("getTopSongs", nil))).To(BeEquivalentTo(10))
	})

	g.It("searches with search2, search and a quoted empty query", func() {
		res := obj(
			okResp(f.call("search2", url.Values{"query": {"never"}})),
			"searchResult2",
		)
		Expect(list(res, "album")[0]["isDir"]).To(BeTrue())

		res = obj(
			okResp(
				f.call(
					"search",
					url.Values{"artist": {"nirv"}, "title": {"smells"}},
				),
			),
			"searchResult",
		)
		Expect(res["totalHits"]).To(BeEquivalentTo(1))
		Expect(ids(list(res, "match"))).To(Equal([]string{tr(f.withFile)}))
		res = obj(
			okResp(f.call("search", url.Values{"any": {"nevermind"}})),
			"searchResult",
		)
		Expect(res["totalHits"]).To(BeEquivalentTo(1))

		res = obj(
			okResp(f.call("search3", url.Values{"query": {`""`}})),
			"searchResult3",
		)
		Expect(list(res, "artist")).To(HaveLen(1))
		Expect(list(res, "song")).To(HaveLen(1))
	})

	g.It("pages a search past 100 hits", func() {
		ctx := context.Background()
		bulk := make([]*ent.ArtistCreate, 0, 150)
		for i := range 150 {
			bulk = append(bulk, f.client.Artist.Create().
				SetMbid(fmt.Sprintf("bulk-%03d", i)).
				SetName(fmt.Sprintf("Bulk Band %03d", i)))
		}
		bands, err := f.client.Artist.CreateBulk(bulk...).Save(ctx)
		Expect(err).NotTo(HaveOccurred())
		for i, b := range bands {
			f.addAlbum(
				b,
				fmt.Sprintf("bulk-rg-%03d", i),
				"Demo",
				fmt.Sprintf("/music/bulk/%03d.flac", i),
			)
		}

		res := obj(okResp(f.call("search3", url.Values{
			"query": {"Bulk Band"}, "artistCount": {"150"},
			"albumCount": {"0"}, "songCount": {"0"},
		})), "searchResult3")
		Expect(list(res, "artist")).To(HaveLen(150))
	})

	g.It("answers 70 for a song whose file went between two queries", func() {
		f.beforeNextFileQuery(func() { f.loseFile(f.withFile) })
		Expect(
			errCode(f.call("getSong", url.Values{"id": {tr(f.withFile)}})),
		).To(BeEquivalentTo(70))
	})

	g.It("counts genres in two grouped queries", func() {
		ctx := context.Background()
		other := f.addArtist("a-3", "Miles Davis", "Jazz")
		_, err := f.client.Album.Create().
			SetMbid("rg-jazz").
			SetTitle("Kind of Blue").
			SetType("album").
			SetArtist(other).
			Save(ctx)
		Expect(err).NotTo(HaveOccurred())

		n := f.countTrackQueries()
		gs := list(obj(okResp(f.call("getGenres", nil)), "genres"), "genre")
		Expect(n.Load()).To(BeEquivalentTo(1))
		byName := map[string][2]any{}
		for _, g := range gs {
			byName[g["value"].(string)] = [2]any{g["albumCount"], g["songCount"]}
		}
		// Kind of Blue holds no file, so Jazz has nothing to count.
		Expect(byName).To(Equal(map[string][2]any{
			"Grunge": {float64(1), float64(1)},
		}))
	})
})
