package subsonic

import (
	"context"
	"net/url"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = g.Describe("playlists", g.Label("integration"), func() {
	var f *fixture

	g.BeforeEach(func() {
		f = newFixture(context.Background())
		f.giveFile(f.noFile, "/music/nirvana/02.flac")
	})

	create := func(name string, songs ...string) map[string]any {
		g.GinkgoHelper()
		return obj(okResp(f.call("createPlaylist", url.Values{
			"name": {name}, "songId": songs,
		})), "playlist")
	}

	g.It("creates, lists, reads and deletes a playlist", func() {
		p := create("Mix", tr(f.withFile), tr(f.noFile), tr(f.withFile))
		Expect(p["songCount"]).To(BeEquivalentTo(3))
		Expect(p["owner"]).To(Equal("listener@example.com"))
		Expect(p["coverArt"]).To(Equal(al(f.album)))
		Expect(ids(list(p, "entry"))).To(Equal([]string{
			tr(f.withFile), tr(f.noFile), tr(f.withFile),
		}))

		all := list(
			obj(okResp(f.call("getPlaylists", nil)), "playlists"),
			"playlist",
		)
		Expect(all).To(HaveLen(1))
		Expect(all[0]).NotTo(HaveKey("entry"))

		got := obj(
			okResp(f.call("getPlaylist", url.Values{"id": {p["id"].(string)}})),
			"playlist",
		)
		Expect(got["duration"]).To(BeNumerically(">", 0))

		okResp(f.call("deletePlaylist", url.Values{"id": {p["id"].(string)}}))
		Expect(errCode(f.call("getPlaylist", url.Values{"id": {p["id"].(string)}}))).
			To(BeEquivalentTo(70))
	})

	g.It("refuses a playlist naming a song that does not exist", func() {
		Expect(errCode(f.call("createPlaylist", url.Values{
			"name": {"Bad"}, "songId": {"tr-999999"},
		}))).To(BeEquivalentTo(70))
		Expect(errCode(f.call("createPlaylist", nil))).To(BeEquivalentTo(10))
	})

	g.It("edits by visible index, renames, comments and publishes", func() {
		p := create("Mix", tr(f.withFile), tr(f.noFile))
		id := p["id"].(string)
		okResp(f.call("updatePlaylist", url.Values{
			"playlistId":        {id},
			"name":              {"Renamed"},
			"comment":           {"for the road"},
			"public":            {"true"},
			"songIndexToRemove": {"0"},
			"songIdToAdd":       {tr(f.withFile)},
		}))
		got := obj(okResp(f.call("getPlaylist", url.Values{"id": {id}})), "playlist")
		Expect(got["name"]).To(Equal("Renamed"))
		Expect(got["comment"]).To(Equal("for the road"))
		Expect(got["public"]).To(BeTrue())
		Expect(
			ids(list(got, "entry")),
		).To(Equal([]string{tr(f.noFile), tr(f.withFile)}))

		Expect(errCode(f.call("updatePlaylist", url.Values{
			"playlistId": {id}, "songIndexToRemove": {"9"},
		}))).To(BeEquivalentTo(0))
	})

	g.It("replaces the songs through createPlaylist with a playlistId", func() {
		p := create("Mix", tr(f.withFile))
		got := obj(okResp(f.call("createPlaylist", url.Values{
			"playlistId": {p["id"].(string)}, "songId": {tr(f.noFile)},
		})), "playlist")
		Expect(ids(list(got, "entry"))).To(Equal([]string{tr(f.noFile)}))
		Expect(got["name"]).To(Equal("Mix"))
	})

	g.It(
		"hides another user's private playlist and lets only its owner edit",
		func() {
			other := f.addUser("other@example.com", "member")
			admin := f.addUser("admin@example.com", "admin")
			p := create("Mine", tr(f.withFile))
			id := p["id"].(string)

			Expect(
				list(
					obj(
						okResp(f.callAs(other.Email, "getPlaylists", nil)),
						"playlists",
					),
					"playlist",
				),
			).
				To(BeEmpty())
			Expect(
				errCode(
					f.callAs(other.Email, "getPlaylist", url.Values{"id": {id}}),
				),
			).
				To(BeEquivalentTo(70))

			okResp(
				f.call(
					"updatePlaylist",
					url.Values{"playlistId": {id}, "public": {"true"}},
				),
			)
			Expect(
				list(
					obj(
						okResp(f.callAs(other.Email, "getPlaylists", nil)),
						"playlists",
					),
					"playlist",
				),
			).
				To(HaveLen(1))
			Expect(
				errCode(
					f.callAs(other.Email, "deletePlaylist", url.Values{"id": {id}}),
				),
			).
				To(BeEquivalentTo(50))
			Expect(errCode(f.callAs(other.Email, "getPlaylists", url.Values{
				"username": {"listener@example.com"},
			}))).To(BeEquivalentTo(50))

			okResp(
				f.callAs(
					admin.Email,
					"updatePlaylist",
					url.Values{"playlistId": {id}, "name": {"Admin"}},
				),
			)
			okResp(f.callAs(admin.Email, "deletePlaylist", url.Values{"id": {id}}))
		},
	)

	g.It("skips entries whose file went missing", func() {
		p := create("Mix", tr(f.withFile), tr(f.noFile))
		Expect(
			f.client.MediaFile.Delete().Exec(context.Background()),
		).Error().
			NotTo(HaveOccurred())
		f.giveFile(f.withFile, "/music/nirvana/01.flac")
		got := obj(
			okResp(f.call("getPlaylist", url.Values{"id": {p["id"].(string)}})),
			"playlist",
		)
		Expect(ids(list(got, "entry"))).To(Equal([]string{tr(f.withFile)}))
	})

	g.It("keeps an entry whose file is missing through an unrelated edit", func() {
		p := create("Mix", tr(f.withFile), tr(f.noFile))
		id := p["id"].(string)
		f.loseFile(f.noFile)

		okResp(f.call("updatePlaylist", url.Values{
			"playlistId": {id}, "songIdToAdd": {tr(f.withFile)},
		}))
		f.restoreFile(f.noFile)

		got := obj(okResp(f.call("getPlaylist", url.Values{"id": {id}})), "playlist")
		Expect(ids(list(got, "entry"))).To(Equal([]string{
			tr(f.withFile), tr(f.noFile), tr(f.withFile),
		}))
	})

	g.It("removes by the index the client saw, past a missing entry", func() {
		p := create("Mix", tr(f.noFile), tr(f.withFile), tr(f.noFile))
		id := p["id"].(string)
		f.loseFile(f.noFile)

		// The client sees only withFile, at index 0.
		okResp(f.call("updatePlaylist", url.Values{
			"playlistId": {id}, "songIndexToRemove": {"0"},
		}))
		f.restoreFile(f.noFile)

		got := obj(okResp(f.call("getPlaylist", url.Values{"id": {id}})), "playlist")
		Expect(
			ids(list(got, "entry")),
		).To(Equal([]string{tr(f.noFile), tr(f.noFile)}))
	})

	g.It("lets an admin list another user's playlists in any case", func() {
		admin := f.addUser("admin@example.com", "admin")
		create("Mine", tr(f.withFile))
		got := list(obj(okResp(f.callAs(admin.Email, "getPlaylists", url.Values{
			"username": {"Listener@Example.COM"},
		})), "playlists"), "playlist")
		Expect(got).To(HaveLen(1))
		Expect(got[0]["name"]).To(Equal("Mine"))
	})

	g.It("reads every playlist's songs in one query", func() {
		create("One", tr(f.withFile))
		create("Two", tr(f.withFile), tr(f.noFile))
		create("Three", tr(f.noFile), tr(f.noFile), tr(f.withFile))
		f.loseFile(f.noFile)

		n := f.countTrackQueries()
		all := list(
			obj(okResp(f.call("getPlaylists", nil)), "playlists"),
			"playlist",
		)
		Expect(n.Load()).To(BeEquivalentTo(1))

		counts := map[string]any{}
		for _, p := range all {
			counts[p["name"].(string)] = p["songCount"]
		}
		Expect(counts).To(Equal(map[string]any{
			"One": float64(1), "Two": float64(1), "Three": float64(1),
		}))
	})
})
