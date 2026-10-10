package subsonic

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // the Subsonic token scheme is defined as md5(password+salt)
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/appaccess"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

type fixture struct {
	client  *ent.Client
	handler http.Handler
	artist  *ent.Artist
	album   *ent.Album
	withFile,
	noFile *ent.Track
}

func signedQuery(password string, extra url.Values) url.Values {
	const salt = "abc123"
	sum := md5.Sum([]byte(password + salt))
	q := url.Values{
		"u": {"listener@example.com"},
		"t": {hex.EncodeToString(sum[:])},
		"s": {salt},
		"f": {"json"},
	}
	maps.Copy(q, extra)
	return q
}

func newFixture(ctx context.Context) *fixture {
	g.GinkgoHelper()
	client := dbtest.SetupTestDB(ctx)
	g.DeferCleanup(func() { client.Close() })

	released := time.Date(1991, 9, 24, 0, 0, 0, 0, time.UTC)
	store := db.New(client)
	ar, err := store.CreateArtist(ctx, db.CreateArtistParams{
		MBID: "a-1", Name: "Nirvana",
		Albums: []db.AlbumSeed{{
			MBID: "rg-1", Title: "Nevermind", Type: "album", ReleaseDate: &released,
		}},
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(store.SetAlbumHydration(ctx, ar.Edges.Albums[0].ID, db.HydrationParams{
		Tracks: []db.TrackSeed{
			{MBID: "t-1", Title: "Smells Like Teen Spirit", Disc: 1, Position: 1},
			{MBID: "t-2", Title: "In Bloom", Disc: 1, Position: 2},
		},
	}, time.Now())).To(Succeed())
	ar, err = store.FindArtistByID(ctx, ar.ID)
	Expect(err).NotTo(HaveOccurred())
	Expect(
		client.Artist.UpdateOneID(ar.ID).SetSortName("Nirvana").Exec(ctx),
	).To(Succeed())

	al := ar.Edges.Albums[0]
	var withFile, noFile *ent.Track
	for _, t := range al.Edges.Tracks {
		if t.Position == 1 {
			withFile = t
		} else {
			noFile = t
		}
	}
	_, err = client.MediaFile.Create().
		SetPath("/music/nirvana/01.flac").
		SetSize(1000).
		SetQuality("flac-24").
		SetFormat("flac").
		SetDurationSeconds(301).
		SetBitrate(900000).
		SetTrackID(withFile.ID).
		Save(ctx)
	Expect(err).NotTo(HaveOccurred())

	_, err = client.User.Create().
		SetEmail("listener@example.com").
		SetSubsonicPassword("sesame").
		Save(ctx)
	Expect(err).NotTo(HaveOccurred())

	return &fixture{
		client: client,
		handler: New(
			Deps{Ent: client, Tracker: appaccess.NewTracker(client)},
		).Routes(),
		artist:   ar,
		album:    al,
		withFile: withFile,
		noFile:   noFile,
	}
}

func (f *fixture) call(endpoint string, extra url.Values) map[string]any {
	g.GinkgoHelper()
	return f.callWith(endpoint, signedQuery("sesame", extra))
}

func (f *fixture) callWith(endpoint string, q url.Values) map[string]any {
	g.GinkgoHelper()
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(
		rec,
		httptest.NewRequest(http.MethodGet, "/"+endpoint+"?"+q.Encode(), nil),
	)
	Expect(rec.Code).To(Equal(http.StatusOK))
	var payload map[string]map[string]any
	Expect(json.Unmarshal(rec.Body.Bytes(), &payload)).To(Succeed())
	return payload["subsonic-response"]
}

func errCode(resp map[string]any) float64 {
	g.GinkgoHelper()
	Expect(resp["status"]).To(Equal("failed"))
	return resp["error"].(map[string]any)["code"].(float64)
}

var _ = g.Describe("browse", g.Label("integration"), func() {
	var f *fixture

	g.BeforeEach(func() {
		f = newFixture(context.Background())
	})

	g.It("logs a store failure and answers the generic error", func() {
		var logs bytes.Buffer
		g.GinkgoWriter.TeeTo(&logs)
		g.DeferCleanup(g.GinkgoWriter.ClearTeeWriters)
		Expect(f.client.Close()).To(Succeed())

		resp := f.call("getArtists", nil)

		Expect(errCode(resp)).To(BeEquivalentTo(errGeneric))
		Expect(resp["error"].(map[string]any)["message"]).To(Equal("Internal error"))
		Expect(logs.String()).To(ContainSubstring("subsonic request failed"))
		Expect(logs.String()).To(ContainSubstring("database is closed"))
	})

	g.It("groups getArtists by the first letter of sort_name", func() {
		resp := f.call("getArtists", nil)
		idx := resp["artists"].(map[string]any)["index"].([]any)
		Expect(idx).To(HaveLen(1))
		Expect(idx[0].(map[string]any)["name"]).To(Equal("N"))
		entry := idx[0].(map[string]any)["artist"].([]any)[0].(map[string]any)
		Expect(entry["id"]).To(Equal(fmt.Sprintf("ar-%d", f.artist.ID)))
		Expect(entry["albumCount"]).To(BeEquivalentTo(1))
		Expect(entry["coverArt"]).To(Equal(fmt.Sprintf("ar-%d", f.artist.ID)))
	})

	g.It("falls back to name when sort_name is empty", func() {
		Expect(
			f.client.Artist.UpdateOneID(f.artist.ID).
				ClearSortName().
				Exec(context.Background()),
		).To(Succeed())
		resp := f.call("getArtists", nil)
		idx := resp["artists"].(map[string]any)["index"].([]any)
		Expect(idx[0].(map[string]any)["name"]).To(Equal("N"))
	})

	g.It("lists an artist's albums", func() {
		resp := f.call(
			"getArtist",
			url.Values{"id": {fmt.Sprintf("ar-%d", f.artist.ID)}},
		)
		albums := resp["artist"].(map[string]any)["album"].([]any)
		Expect(albums).To(HaveLen(1))
		entry := albums[0].(map[string]any)
		Expect(entry["id"]).To(Equal(fmt.Sprintf("al-%d", f.album.ID)))
		Expect(entry["songCount"]).To(BeEquivalentTo(2))
		Expect(entry["coverArt"]).To(Equal(fmt.Sprintf("al-%d", f.album.ID)))
		Expect(entry["year"]).To(BeEquivalentTo(1991))
	})

	g.It("lists an album's songs, omitting tracks with no file", func() {
		resp := f.call(
			"getAlbum",
			url.Values{"id": {fmt.Sprintf("al-%d", f.album.ID)}},
		)
		songs := resp["album"].(map[string]any)["song"].([]any)
		Expect(songs).To(HaveLen(1))
		song := songs[0].(map[string]any)
		Expect(song["id"]).To(Equal(fmt.Sprintf("tr-%d", f.withFile.ID)))
		Expect(song["track"]).To(BeEquivalentTo(1))
		Expect(song["discNumber"]).To(BeEquivalentTo(1))
		Expect(song["suffix"]).To(Equal("flac"))
		Expect(song["contentType"]).To(Equal("audio/flac"))
		Expect(song["duration"]).To(BeEquivalentTo(301))
		Expect(song["size"]).To(BeEquivalentTo(1000))
		Expect(song["bitRate"]).To(BeEquivalentTo(900))
	})

	g.It("falls back to the track duration when the file has none", func() {
		Expect(
			f.client.Track.UpdateOneID(f.withFile.ID).
				SetDuration(222).
				Exec(context.Background()),
		).To(Succeed())
		Expect(
			f.client.MediaFile.Update().
				SetDurationSeconds(0).
				Exec(context.Background()),
		).To(Succeed())
		resp := f.call(
			"getSong",
			url.Values{"id": {fmt.Sprintf("tr-%d", f.withFile.ID)}},
		)
		Expect(resp["song"].(map[string]any)["duration"]).To(BeEquivalentTo(222))
	})

	g.It("answers getSong and 70 for a track with no file", func() {
		resp := f.call(
			"getSong",
			url.Values{"id": {fmt.Sprintf("tr-%d", f.withFile.ID)}},
		)
		Expect(resp["status"]).To(Equal("ok"))
		Expect(
			resp["song"].(map[string]any)["title"],
		).To(Equal("Smells Like Teen Spirit"))

		resp = f.call(
			"getSong",
			url.Values{"id": {fmt.Sprintf("tr-%d", f.noFile.ID)}},
		)
		Expect(errCode(resp)).To(BeEquivalentTo(70))
	})

	g.It("skips files flagged missing", func() {
		Expect(
			f.client.MediaFile.Update().
				SetMissingSince(time.Now()).
				Exec(context.Background()),
		).To(Succeed())
		resp := f.call(
			"getSong",
			url.Values{"id": {fmt.Sprintf("tr-%d", f.withFile.ID)}},
		)
		Expect(errCode(resp)).To(BeEquivalentTo(70))
	})

	g.It("serves album lists by type and falls back to alphabetical", func() {
		for _, typ := range []string{"newest", "alphabeticalByName", "random", "bogus"} {
			resp := f.call("getAlbumList2", url.Values{"type": {typ}, "size": {"1"}})
			albums := resp["albumList2"].(map[string]any)["album"].([]any)
			Expect(albums).To(HaveLen(1), typ)
			Expect(albums[0].(map[string]any)["name"]).To(Equal("Nevermind"))
		}
	})

	g.It("searches artists, albums and songs", func() {
		resp := f.call("search3", url.Values{"query": {"nirv"}})
		result := resp["searchResult3"].(map[string]any)
		Expect(result["artist"]).To(HaveLen(1))

		resp = f.call("search3", url.Values{"query": {"smells"}})
		result = resp["searchResult3"].(map[string]any)
		Expect(result["song"]).To(HaveLen(1))
		Expect(result["artist"]).To(BeEmpty())
	})

	g.It("answers 70 for bad ids and 10 for a missing id", func() {
		Expect(
			errCode(f.call("getAlbum", url.Values{"id": {"al-999"}})),
		).To(BeEquivalentTo(70))
		Expect(
			errCode(f.call("getAlbum", url.Values{"id": {"xx-1"}})),
		).To(BeEquivalentTo(70))
		Expect(
			errCode(f.call("getAlbum", url.Values{"id": {"al-99999999999"}})),
		).To(BeEquivalentTo(70))
		Expect(
			errCode(
				f.call(
					"getAlbum",
					url.Values{"id": {fmt.Sprintf("ar-%d", f.artist.ID)}},
				),
			),
		).To(BeEquivalentTo(70))
		Expect(errCode(f.call("getAlbum", nil))).To(BeEquivalentTo(10))
	})

	g.It("serves music folders, ping and license", func() {
		folders := f.call("getMusicFolders", nil)["musicFolders"].(map[string]any)["musicFolder"].([]any)
		Expect(folders).To(HaveLen(1))
		Expect(folders[0].(map[string]any)["name"]).To(Equal("Music"))

		Expect(f.call("ping.view", nil)["status"]).To(Equal("ok"))
		Expect(
			f.call("getLicense", nil)["license"].(map[string]any)["valid"],
		).To(BeTrue())
	})

	g.It("rejects missing, wrong and unknown-user credentials with 40", func() {
		Expect(
			errCode(
				f.callWith(
					"ping",
					url.Values{"u": {"listener@example.com"}, "f": {"json"}},
				),
			),
		).To(BeEquivalentTo(40))
		Expect(
			errCode(f.callWith("ping", signedQuery("wrong", nil))),
		).To(BeEquivalentTo(40))

		q := signedQuery("sesame", nil)
		q.Set("u", "nobody@example.com")
		Expect(errCode(f.callWith("ping", q))).To(BeEquivalentTo(40))
	})

	g.It("rejects Subsonic access once the password is cleared", func() {
		Expect(
			f.client.User.Update().
				ClearSubsonicPassword().
				Exec(context.Background()),
		).To(Succeed())
		Expect(errCode(f.call("ping", nil))).To(BeEquivalentTo(40))
	})

	g.It("serializes the album tree as XML attributes by default", func() {
		q := signedQuery(
			"sesame",
			url.Values{"id": {fmt.Sprintf("al-%d", f.album.ID)}},
		)
		q.Del("f")
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(
			rec,
			httptest.NewRequest(http.MethodGet, "/getAlbum.view?"+q.Encode(), nil),
		)
		Expect(rec.Body.String()).To(ContainSubstring(`<album id="al-`))
		Expect(rec.Body.String()).To(ContainSubstring(`<song id="tr-`))
		Expect(rec.Body.String()).To(ContainSubstring(`suffix="flac"`))
	})

	g.It("answers error 0 for an unimplemented endpoint", func() {
		Expect(errCode(f.call("getPlaylists", nil))).To(BeEquivalentTo(0))
	})
})
