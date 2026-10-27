package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"

	"github.com/anacrolix/torrent/metainfo"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// removingClient records the torrents a pass removes.
type removingClient struct {
	fakeSelectiveClient
	removed []string
}

func (c *removingClient) RemoveTorrent(
	_ context.Context, hash string, deleteFiles bool,
) error {
	Expect(deleteFiles).To(BeTrue())
	c.removed = append(c.removed, hash)
	return nil
}

func albumWithTracks(n int) *ent.Album {
	tracks := make([]*ent.Track, n)
	for i := range tracks {
		tracks[i] = &ent.Track{ID: uint32(i + 1)}
	}
	return &ent.Album{ID: 7, Edges: ent.AlbumEdges{Tracks: tracks}}
}

var imageFiles = []metainfo.FileInfo{
	{Path: []string{"Album.flac"}, Length: 300 << 20},
	{Path: []string{"Album.cue"}, Length: 2 << 10},
	{Path: []string{"Album.log"}, Length: 4 << 10},
}

var _ = Describe(
	"album grab of a single-file CD image",
	Label("unit", "downloads"),
	func() {
		var (
			ctx    context.Context
			store  *dbmocks.MockStore
			client *fakeSelectiveClient
			mgr    Downloader
			result indexer.SearchResult
		)

		BeforeEach(func() {
			ctx = context.Background()
			store = dbmocks.NewMockStore(GinkgoT())
			client = &fakeSelectiveClient{addHash: "abc123"}
			mgr = New(store, client)

			raw := buildTorrentBytes(metainfo.Info{Name: "Album", Files: imageFiles})
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					_, err := w.Write(raw)
					Expect(err).NotTo(HaveOccurred())
				},
			))
			DeferCleanup(srv.Close)
			endpoint, err := url.Parse(srv.URL)
			Expect(err).NotTo(HaveOccurred())
			port, err := strconv.Atoi(endpoint.Port())
			Expect(err).NotTo(HaveOccurred())
			configtest.Setup(map[string]any{
				"indexers": []map[string]any{{
					"name": "tracker", "host": endpoint.Hostname(), "port": port,
					"api_key": "k", "protocol": "torznab", "enabled": true,
				}},
				"download_clients": []map[string]any{{
					"name": "embedded", "client_type": "builtin",
					"download_dir": "/downloads", "enabled": true,
				}},
			})
			result = indexer.SearchResult{
				Title:    "X - Album [FLAC]",
				Download: srv.URL + "/dl",
			}
		})

		It("is refused before the torrent reaches the client", func() {
			store.EXPECT().FindAlbumByID(mock.Anything, uint32(7)).
				Return(albumWithTracks(10), nil).Once()

			_, err := mgr.GrabAlbum(ctx, result, 7)

			Expect(err).To(MatchError(ErrCueImage))
			Expect(client.addTorrentCalls).To(BeZero())
		})

		It("goes through for a one-track album, whose image is its track", func() {
			store.EXPECT().FindAlbumByID(mock.Anything, uint32(7)).
				Return(albumWithTracks(1), nil).Once()
			store.EXPECT().
				FindLiveDownloadRecordByHash(mock.Anything, mock.Anything).
				Return(nil, nil).
				Maybe()
			store.EXPECT().CreateDownloadRecord(mock.Anything, mock.MatchedBy(
				func(p db.CreateDownloadRecordParams) bool { return p.AlbumID == 7 },
			)).Return(&ent.DownloadRecord{ID: 42}, nil).Once()

			_, err := mgr.GrabAlbum(ctx, result, 7)

			Expect(err).NotTo(HaveOccurred())
			Expect(client.addTorrentCalls).To(Equal(1))
		})
	},
)

var _ = Describe("RejectCueImages", Label("unit", "downloads"), func() {
	var (
		ctx    context.Context
		store  *dbmocks.MockStore
		client *removingClient
		mgr    Checker
		rec    *ent.DownloadRecord
	)

	imageListing := []TorrentFile{
		{Index: 0, Path: "Album/Album.flac"},
		{Index: 1, Path: "Album/Album.cue"},
	}

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		client = &removingClient{}
		mgr = New(store, client)
		configtest.Setup(map[string]any{
			"download_clients": []map[string]any{{
				"name": "embedded", "client_type": "builtin",
				"download_dir": "/downloads", "enabled": true,
			}},
		})
		rec = &ent.DownloadRecord{
			ID: 5, Title: "X - Album", TorrentHash: "h5",
			DownloadClientName: "embedded",
			Edges:              ent.DownloadRecordEdges{Album: &ent.Album{ID: 7}},
		}
		store.EXPECT().ListDownloadingAlbumRecords(mock.Anything).
			Return([]*ent.DownloadRecord{rec}, nil)
	})

	It(
		"removes the torrent and rejects the record once the files are known",
		func() {
			Expect(mgr.RejectCueImages(ctx)).To(Succeed())
			Expect(client.removed).To(BeEmpty())

			client.listFilesResult = imageListing
			store.EXPECT().FindAlbumByID(mock.Anything, uint32(7)).
				Return(albumWithTracks(10), nil).Once()
			store.EXPECT().RejectDownloadingRecord(
				mock.Anything, uint32(5), "release is a "+library.CueImageReason,
			).Return(nil).Once()

			Expect(mgr.RejectCueImages(ctx)).To(Succeed())
			Expect(client.removed).To(Equal([]string{"h5"}))
		},
	)

	It("lists a record's files once, not every tick", func() {
		client.listFilesResult = []TorrentFile{
			{Index: 0, Path: "Album/01.flac"},
			{Index: 1, Path: "Album/02.flac"},
		}

		Expect(mgr.RejectCueImages(ctx)).To(Succeed())
		Expect(mgr.RejectCueImages(ctx)).To(Succeed())
		Expect(client.listFilesCalls).To(Equal(1))
		Expect(client.removed).To(BeEmpty())
	})
})
