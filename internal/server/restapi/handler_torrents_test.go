package restapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/bittorrent"
	"github.com/datahearth/streamline/internal/download"
)

const testHash = "aabbccddeeff00112233445566778899aabbccdd"

var _ = Describe("Handler: Torrents", Label("unit", "server", "torrents"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		app = newAPIKeyApp()
	})

	It("lists torrents with live stats", func() {
		app.torrents.EXPECT().ListViews(mock.Anything).Return(
			[]bittorrent.TorrentView{{
				Hash: testHash,
				Name: "Test", Status: download.StatusSeeding,
				Progress: 1, Size: 100, Uploaded: 250, Ratio: 2.5, PeerCount: 3,
			}},
		).Once()

		resp, err := http.Get(app.srv.URL + "/api/v1/torrents")
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var list TorrentList
		Expect(json.NewDecoder(resp.Body).Decode(&list)).To(Succeed())
		Expect(list.Items).To(HaveLen(1))
		Expect(list.Items[0].Ratio).To(Equal(2.5))
		Expect(list.RefreshedAt).NotTo(BeZero())
	})

	It("adds a torrent from a magnet link", func() {
		app.torrents.EXPECT().AddTorrent(mock.Anything, download.TorrentSource{
			Magnet: "magnet:?xt=urn:btih:" + testHash,
		}).Return(testHash, nil).Once()

		resp, err := http.Post(app.srv.URL+"/api/v1/torrents",
			"application/json", strings.NewReader(
				`{"magnet": "magnet:?xt=urn:btih:`+testHash+`"}`,
			))
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		var res TorrentAddResult
		Expect(json.NewDecoder(resp.Body).Decode(&res)).To(Succeed())
		Expect(res.Hash).To(Equal(testHash))
	})

	It("rejects an add with both magnet and torrent", func() {
		resp, err := http.Post(app.srv.URL+"/api/v1/torrents",
			"application/json", strings.NewReader(
				`{"magnet": "magnet:?x", "torrent": "ZGF0YQ=="}`,
			))
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
	})

	It("forbids non-admin mutations", func() {
		app.addMember("member")
		req := app.req(http.MethodDelete,
			"/api/v1/torrents/"+testHash, "test-member-token", nil)
		resp := app.do(req)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
	})

	It("maps unknown hashes to 404", func() {
		app.torrents.EXPECT().Details(mock.Anything, testHash).
			Return(bittorrent.TorrentDetails{}, download.ErrTorrentNotFound).Once()
		resp, err := http.Get(app.srv.URL + "/api/v1/torrents/" + testHash)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("pauses a torrent via its pause subresource", func() {
		app.torrents.EXPECT().PauseTorrent(mock.Anything, testHash).
			Return(nil).Once()

		req := app.req(http.MethodPost,
			"/api/v1/torrents/"+testHash+"/pause", app.adminKey, nil)
		resp := app.do(req)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
	})

	It("sets a single file's priority", func() {
		app.torrents.EXPECT().SetFilePriorities(mock.Anything, testHash,
			[]bittorrent.FilePriority{{Index: 0, Priority: "skip"}},
		).Return(nil).Once()

		req := app.req(http.MethodPatch,
			"/api/v1/torrents/"+testHash+"/files/0",
			app.adminKey, strings.NewReader(`{"priority": "skip"}`))
		req.Header.Set("Content-Type", "application/json")
		resp := app.do(req)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
	})

	It("returns 404 when no builtin client is configured", func() {
		// direct StrictServer call — no engine wired
		bare := New(Deps{})
		resp, err := bare.ListTorrents(context.Background(),
			ListTorrentsRequestObject{})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).To(BeAssignableToTypeOf(ListTorrents404JSONResponse{}))
	})
})
