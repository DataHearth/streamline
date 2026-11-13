package download

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"

	"github.com/anacrolix/torrent/metainfo"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("ReleaseFiles", Label("unit", "downloads"), func() {
	var (
		ctx context.Context
		mgr Downloader
	)

	// serve answers every request with body from one enabled indexer and
	// returns the release link.
	serve := func(body []byte) string {
		GinkgoHelper()
		srv := httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) },
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
		})
		return srv.URL + "/dl"
	}

	BeforeEach(func() {
		ctx = context.Background()
		// Nothing here may touch the database or a client.
		mgr = New(dbmocks.NewMockStore(GinkgoT()), &fakeSelectiveClient{})
	})

	It("lists the .torrent's files without adding it anywhere", func() {
		dl := serve(buildTorrentBytes(metainfo.Info{
			Name: "Avatar - Feathers & Flesh",
			Files: []metainfo.FileInfo{
				{Path: []string{"01 - Regret.flac"}, Length: 30},
				{Path: []string{"cover.jpg"}, Length: 1},
			},
		}))
		files, err := mgr.ReleaseFiles(ctx, indexer.SearchResult{Download: dl})
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(Equal([]ReleaseFile{
			{Path: "01 - Regret.flac", Size: 30},
			{Path: "cover.jpg", Size: 1},
		}))
	})

	It("has no file list for a magnet its client cannot probe", func() {
		_, err := mgr.ReleaseFiles(ctx, indexer.SearchResult{
			Download: "magnet:?xt=urn:btih:abc",
		})
		Expect(err).To(MatchError(ErrFileListUnavailable))
	})

	It("lists a magnet's files through a client that can probe it", func() {
		configtest.Setup(map[string]any{
			"download_clients": []map[string]any{{
				"name": "embedded", "client_type": "builtin",
				"download_dir": "/downloads", "enabled": true,
			}},
		})
		prober := &fakeProbeClient{metainfo: buildTorrentBytes(metainfo.Info{
			Name: "Avatar - Feathers & Flesh",
			Files: []metainfo.FileInfo{
				{Path: []string{"01 - Regret.flac"}, Length: 30},
			},
		})}
		probing := New(dbmocks.NewMockStore(GinkgoT()), prober)

		files, err := probing.ReleaseFiles(ctx, indexer.SearchResult{
			Download: "magnet:?xt=urn:btih:abc",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(prober.magnet).To(Equal("magnet:?xt=urn:btih:abc"))
		Expect(files).To(Equal([]ReleaseFile{{Path: "01 - Regret.flac", Size: 30}}))
	})

	It("has no file list for a magnet no peer answered for", func() {
		configtest.Setup(map[string]any{
			"download_clients": []map[string]any{{
				"name": "embedded", "client_type": "builtin",
				"download_dir": "/downloads", "enabled": true,
			}},
		})
		silent := New(dbmocks.NewMockStore(GinkgoT()), &fakeProbeClient{
			err: fmt.Errorf("%w: no peer", ErrFileListUnavailable),
		})
		_, err := silent.ReleaseFiles(ctx, indexer.SearchResult{
			Download: "magnet:?xt=urn:btih:abc",
		})
		Expect(err).To(MatchError(ErrFileListUnavailable))
	})

	It("refuses a link outside the configured indexers", func() {
		configtest.Setup(map[string]any{})
		_, err := mgr.ReleaseFiles(ctx, indexer.SearchResult{
			Download: "http://169.254.169.254/latest/meta-data/",
		})
		Expect(err).To(MatchError(ErrUntrustedSource))
	})

	It("fails on a payload that is not a .torrent", func() {
		dl := serve([]byte("<html>login</html>"))
		_, err := mgr.ReleaseFiles(ctx, indexer.SearchResult{Download: dl})
		Expect(err).To(HaveOccurred())
	})
})

// fakeProbeClient is a client that fetches a magnet's metainfo for a preview.
type fakeProbeClient struct {
	fakeSelectiveClient
	magnet   string
	metainfo []byte
	err      error
}

func (f *fakeProbeClient) ProbeMagnet(
	_ context.Context, magnet string,
) ([]byte, error) {
	f.magnet = magnet
	return f.metainfo, f.err
}
