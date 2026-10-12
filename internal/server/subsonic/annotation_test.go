package subsonic

import (
	"context"
	"net/url"
	"strconv"
	"time"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = g.Describe("annotations", g.Label("integration"), func() {
	var f *fixture

	g.BeforeEach(func() {
		f = newFixture(context.Background())
	})

	g.It("stars and unstars songs, albums and artists and shows it back", func() {
		okResp(f.call("star", url.Values{
			"id":       {tr(f.withFile)},
			"albumId":  {al(f.album)},
			"artistId": {ar(f.artist)},
		}))

		song := obj(
			okResp(f.call("getSong", url.Values{"id": {tr(f.withFile)}})),
			"song",
		)
		Expect(song["starred"]).NotTo(BeEmpty())

		album := obj(
			okResp(f.call("getAlbum", url.Values{"id": {al(f.album)}})),
			"album",
		)
		Expect(album["starred"]).NotTo(BeEmpty())
		Expect(list(album, "song")[0]["starred"]).NotTo(BeEmpty())

		starred := obj(okResp(f.call("getStarred2", nil)), "starred2")
		Expect(ids(list(starred, "song"))).To(ConsistOf(tr(f.withFile)))
		Expect(ids(list(starred, "album"))).To(ConsistOf(al(f.album)))
		Expect(ids(list(starred, "artist"))).To(ConsistOf(ar(f.artist)))

		folder := obj(okResp(f.call("getStarred", nil)), "starred")
		Expect(list(folder, "album")[0]["isDir"]).To(BeTrue())

		okResp(f.call("unstar", url.Values{"id": {tr(f.withFile), al(f.album)}}))
		starred = obj(okResp(f.call("getStarred2", nil)), "starred2")
		Expect(list(starred, "song")).To(BeEmpty())
		Expect(list(starred, "album")).To(BeEmpty())
		Expect(list(starred, "artist")).To(HaveLen(1))
	})

	g.It("keeps each listener's marks to themselves", func() {
		other := f.addUser("other@example.com", "member")
		okResp(f.callAs(other.Email, "star", url.Values{"id": {tr(f.withFile)}}))
		song := obj(
			okResp(f.call("getSong", url.Values{"id": {tr(f.withFile)}})),
			"song",
		)
		Expect(song).NotTo(HaveKey("starred"))
	})

	g.It("refuses a star on something that does not exist", func() {
		Expect(
			errCode(f.call("star", url.Values{"id": {"tr-999999"}})),
		).To(BeEquivalentTo(70))
		Expect(errCode(f.call("star", nil))).To(BeEquivalentTo(10))
		Expect(errCode(f.call("star", url.Values{"albumId": {tr(f.withFile)}}))).
			To(BeEquivalentTo(70))
	})

	g.It("rates, clears a rating and lists the highest", func() {
		okResp(f.call("setRating", url.Values{"id": {al(f.album)}, "rating": {"4"}}))
		album := obj(
			okResp(f.call("getAlbum", url.Values{"id": {al(f.album)}})),
			"album",
		)
		Expect(album["userRating"]).To(BeEquivalentTo(4))

		highest := obj(
			okResp(f.call("getAlbumList2", url.Values{"type": {"highest"}})),
			"albumList2",
		)
		Expect(ids(list(highest, "album"))).To(ConsistOf(al(f.album)))

		okResp(f.call("setRating", url.Values{"id": {al(f.album)}, "rating": {"0"}}))
		highest = obj(
			okResp(f.call("getAlbumList2", url.Values{"type": {"highest"}})),
			"albumList2",
		)
		Expect(list(highest, "album")).To(BeEmpty())

		Expect(
			errCode(
				f.call(
					"setRating",
					url.Values{"id": {al(f.album)}, "rating": {"6"}},
				),
			),
		).
			To(BeEquivalentTo(0))
		Expect(errCode(f.call("setRating", url.Values{"id": {al(f.album)}}))).
			To(BeEquivalentTo(10))
	})

	g.It("counts a scrobble on the song, its album and its artist", func() {
		at := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
		okResp(f.call("scrobble", url.Values{
			"id":   {tr(f.withFile)},
			"time": {strconv.FormatInt(at.UnixMilli(), 10)},
		}))
		okResp(f.call("scrobble", url.Values{"id": {tr(f.withFile)}}))

		song := obj(
			okResp(f.call("getSong", url.Values{"id": {tr(f.withFile)}})),
			"song",
		)
		Expect(song["playCount"]).To(BeEquivalentTo(2))
		Expect(song["played"]).NotTo(Equal("2026-03-01T12:00:00Z"))

		for _, typ := range []string{"frequent", "recent"} {
			resp := obj(
				okResp(f.call("getAlbumList2", url.Values{"type": {typ}})),
				"albumList2",
			)
			albums := list(resp, "album")
			Expect(ids(albums)).To(ConsistOf(al(f.album)), typ)
			Expect(albums[0]["playCount"]).To(BeEquivalentTo(2))
		}

		top := obj(
			okResp(f.call("getTopSongs", url.Values{"artist": {"nirvana"}})),
			"topSongs",
		)
		Expect(ids(list(top, "song"))).To(Equal([]string{tr(f.withFile)}))
	})

	g.It("reports now playing without counting a play", func() {
		okResp(f.call("scrobble", url.Values{
			"id": {tr(f.withFile)}, "submission": {"false"}, "c": {"Symfonium"},
		}))
		playing := list(
			obj(okResp(f.call("getNowPlaying", nil)), "nowPlaying"),
			"entry",
		)
		Expect(playing).To(HaveLen(1))
		Expect(playing[0]["id"]).To(Equal(tr(f.withFile)))
		Expect(playing[0]["username"]).To(Equal("listener@example.com"))
		Expect(playing[0]["playerName"]).To(Equal("Symfonium"))
		Expect(playing[0]).NotTo(HaveKey("playCount"))

		// The final scrobble closes the entry.
		okResp(
			f.call(
				"scrobble",
				url.Values{"id": {tr(f.withFile)}, "c": {"Symfonium"}},
			),
		)
		Expect(
			list(obj(okResp(f.call("getNowPlaying", nil)), "nowPlaying"), "entry"),
		).
			To(BeEmpty())
	})

	g.It("expires a now-playing entry nobody closed", func() {
		clock := time.Now()
		f2 := New(Deps{Ent: f.client, Tracker: nil})
		f2.playing.now = func() time.Time { return clock }
		u := f.client.User.Query().FirstX(context.Background())
		f2.playing.set(u, "c", f.withFile.ID, 300)
		Expect(f2.playing.list()).To(HaveLen(1))
		clock = clock.Add(5*time.Minute + nowPlayingTTL + time.Second)
		Expect(f2.playing.list()).To(BeEmpty())
	})
})
