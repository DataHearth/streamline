package subsonic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/appaccess"
	postersmocks "github.com/datahearth/streamline/internal/posters/mocks"
)

var _ = g.Describe("shares", g.Label("integration"), func() {
	var (
		f       *fixture
		h       *Handler
		posters *postersmocks.MockManager
		page    http.Handler
		audio   = []byte("shared-audio-bytes")
	)

	g.BeforeEach(func() {
		ctx := context.Background()
		f = newFixture(ctx)
		path := filepath.Join(g.GinkgoT().TempDir(), "01.flac")
		Expect(os.WriteFile(path, audio, 0o600)).To(Succeed())
		Expect(f.client.MediaFile.Update().SetPath(path).Exec(ctx)).To(Succeed())
		posters = postersmocks.NewMockManager(g.GinkgoT())
		h = New(
			Deps{
				Ent:     f.client,
				Posters: posters,
				Tracker: appaccess.NewTracker(f.client),
			},
		)
		f.handler = h.Routes()
		page = h.SharePage()
	})

	visit := func(path string) *httptest.ResponseRecorder {
		g.GinkgoHelper()
		rec := httptest.NewRecorder()
		page.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	tokenOf := func(s map[string]any) string {
		g.GinkgoHelper()
		u, err := url.Parse(s["url"].(string))
		Expect(err).NotTo(HaveOccurred())
		Expect(u.Path).To(HavePrefix("/share/"))
		return strings.TrimPrefix(u.Path, "/share/")
	}

	g.It("creates a share of an album and serves it to a visitor", func() {
		created := list(obj(okResp(f.call("createShare", url.Values{
			"id": {al(f.album)}, "description": {"Listen to this"},
		})), "shares"), "share")
		Expect(created).To(HaveLen(1))
		s := created[0]
		Expect(s["url"]).To(HavePrefix("http://example.com/share/"))
		Expect(s["username"]).To(Equal("listener@example.com"))
		Expect(ids(list(s, "entry"))).To(Equal([]string{tr(f.withFile)}))
		token := tokenOf(s)
		Expect(len(token)).To(BeNumerically(">=", 22))

		rec := visit("/" + token)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Header().Get("Content-Type")).To(HavePrefix("text/html"))
		html := rec.Body.String()
		Expect(html).To(ContainSubstring("Listen to this"))
		Expect(html).To(ContainSubstring("Smells Like Teen Spirit"))
		Expect(html).NotTo(ContainSubstring("listener@example.com"))
		Expect(html).NotTo(ContainSubstring("<script"))

		rec = visit("/" + token + "/stream/" + strconv.Itoa(int(f.withFile.ID)))
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.Bytes()).To(Equal(audio))

		posters.EXPECT().
			Serve(mock.Anything, mock.Anything, "albums", f.album.ID).
			Once()
		visit("/" + token + "/cover/" + strconv.Itoa(int(f.album.ID)))

		got := list(obj(okResp(f.call("getShares", nil)), "shares"), "share")
		Expect(got).To(HaveLen(1))
		Expect(got[0]["visitCount"]).To(BeEquivalentTo(1))
		Expect(got[0]["lastVisited"]).NotTo(BeEmpty())
	})

	g.It("serves only what the share holds", func() {
		other := f.addArtist("a-2", "Mudhoney", "")
		s := list(obj(okResp(f.call("createShare", url.Values{"id": {tr(f.withFile)}})), "shares"), "share")[0]
		token := tokenOf(s)
		Expect(
			visit("/" + token + "/stream/" + strconv.Itoa(int(f.noFile.ID))).Code,
		).
			To(Equal(http.StatusNotFound))
		Expect(visit("/" + token + "/cover/" + strconv.Itoa(int(other.ID))).Code).
			To(Equal(http.StatusNotFound))
		Expect(visit("/not-a-token").Code).To(Equal(http.StatusNotFound))
	})

	g.It("stops serving an expired or deleted share", func() {
		past := strconv.FormatInt(time.Now().Add(-time.Hour).UnixMilli(), 10)
		s := list(obj(okResp(f.call("createShare", url.Values{
			"id": {tr(f.withFile)}, "expires": {past},
		})), "shares"), "share")[0]
		token := tokenOf(s)
		Expect(visit("/" + token).Code).To(Equal(http.StatusNotFound))

		okResp(
			f.call(
				"updateShare",
				url.Values{"id": {s["id"].(string)}, "expires": {"0"}},
			),
		)
		Expect(visit("/" + token).Code).To(Equal(http.StatusOK))

		okResp(f.call("deleteShare", url.Values{"id": {s["id"].(string)}}))
		Expect(visit("/" + token).Code).To(Equal(http.StatusNotFound))
	})

	g.It("keeps shares to their owner", func() {
		other := f.addUser("other@example.com", "member")
		s := list(obj(okResp(f.call("createShare", url.Values{"id": {tr(f.withFile)}})), "shares"), "share")[0]
		Expect(
			list(
				obj(okResp(f.callAs(other.Email, "getShares", nil)), "shares"),
				"share",
			),
		).
			To(BeEmpty())
		Expect(
			errCode(
				f.callAs(
					other.Email,
					"deleteShare",
					url.Values{"id": {s["id"].(string)}},
				),
			),
		).
			To(BeEquivalentTo(70))
		Expect(
			errCode(
				f.callAs(
					other.Email,
					"updateShare",
					url.Values{"id": {s["id"].(string)}},
				),
			),
		).
			To(BeEquivalentTo(70))
	})

	g.It(
		"refuses to share what does not exist or is someone's private playlist",
		func() {
			Expect(errCode(f.call("createShare", nil))).To(BeEquivalentTo(10))
			Expect(
				errCode(f.call("createShare", url.Values{"id": {"al-999999"}})),
			).To(BeEquivalentTo(70))

			other := f.addUser("other@example.com", "member")
			p := obj(okResp(f.callAs(other.Email, "createPlaylist", url.Values{
				"name": {"Private"}, "songId": {tr(f.withFile)},
			})), "playlist")
			Expect(
				errCode(f.call("createShare", url.Values{"id": {p["id"].(string)}})),
			).
				To(BeEquivalentTo(70))
		},
	)

	g.It("drops a shared playlist its owner made private", func() {
		other := f.addUser("other@example.com", "member")
		admin := f.addUser("admin@example.com", "admin")
		p := obj(okResp(f.callAs(other.Email, "createPlaylist", url.Values{
			"name": {"Public"}, "songId": {tr(f.withFile)},
		})), "playlist")
		pid := p["id"].(string)
		okResp(f.callAs(other.Email, "updatePlaylist", url.Values{
			"playlistId": {pid}, "public": {"true"},
		}))
		s := list(obj(okResp(f.call("createShare", url.Values{"id": {pid}})), "shares"), "share")[0]
		Expect(list(s, "entry")).To(HaveLen(1))

		okResp(f.callAs(other.Email, "updatePlaylist", url.Values{
			"playlistId": {pid}, "public": {"false"},
		}))
		got := list(obj(okResp(f.call("getShares", nil)), "shares"), "share")
		Expect(list(got[0], "entry")).To(BeEmpty())

		// An admin reads every playlist but publishes only public ones.
		Expect(
			errCode(f.callAs(admin.Email, "createShare", url.Values{"id": {pid}})),
		).
			To(BeEquivalentTo(50))
	})
})
