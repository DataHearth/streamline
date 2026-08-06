package fakes

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Torznab serves the tracker surface the indexer client consumes: the caps
// document, a one-item feed and the torrent that feed advertises. One canned
// release is enough — the e2e specs assert plumbing, not catalog breadth.
type Torznab struct {
	URL string
}

// The canned release, exported so specs can assert what came back is the
// fake's and only the fake's.
const (
	// APIKey is the credential specs register the fake with. Requests carrying
	// any other value are rejected with 401, which the client maps to
	// ErrUnauthorized — so a broken credential plumb fails a spec instead of
	// passing unnoticed.
	APIKey = "fake-key"

	// ReleaseTitle names the movie the TMDB fake serves, so a library movie's
	// search reads as a real title match.
	ReleaseTitle   = "Fight.Club.1999.1080p.BluRay.x264-FAKE"
	ReleaseGUID    = "fake-release-1"
	ReleasePubDate = "Fri, 15 Oct 1999 00:00:00 +0000"
	ReleaseSeeders = 10
	ReleasePeers   = 12

	// ReleaseSize clears library.MinMediaSize (50 MiB), below which the
	// importer discards a completed download as "no media found" — the
	// pipeline spec imports these bytes for real, so a token payload would
	// never get past that filter.
	ReleaseSize = 64 * 1024 * 1024

	// TorrentName is the file the torrent describes; the .mkv suffix belongs
	// to the file, not to the release it ships under.
	TorrentName = ReleaseTitle + ".mkv"

	DownloadPath = "/dl/release.torrent"

	// torrent() hashes Content() piece by piece, so ReleaseSize must stay a
	// whole multiple of pieceLength.
	pieceLength = 4 * 1024 * 1024
)

const (
	capsXML = `<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server title="fake-torznab"/>
  <limits max="100" default="50"/>
  <searching>
    <search available="yes" supportedParams="q"/>
    <movie-search available="yes" supportedParams="q,imdbid,tmdbid"/>
    <tv-search available="yes" supportedParams="q,season,ep"/>
  </searching>
  <categories><category id="2000" name="Movies"/></categories>
</caps>`

	// Interpolated values land in the document unescaped, which the canned
	// release survives; a title carrying & or < would need escaping first.
	rssTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
<channel>
  <item>
    <title>%[1]s</title>
    <guid>%[2]s</guid>
    <link>%[3]s</link>
    <pubDate>%[4]s</pubDate>
    <size>%[5]d</size>
    <enclosure url="%[3]s" length="%[5]d" type="application/x-bittorrent"/>
    <torznab:attr name="category" value="2000"/>
    <torznab:attr name="seeders" value="%[6]d"/>
    <torznab:attr name="peers" value="%[7]d"/>
  </item>
</channel>
</rss>`
)

func NewTorznab() *Torznab {
	GinkgoHelper()

	mux := http.NewServeMux()
	srv := httptest.NewUnstartedServer(mux)
	// Both the feed's enclosure and the torrent's announce URL point back at
	// the fake, so its address must be known before the bodies are built: an
	// unstarted server has its listener bound already.
	t := &Torznab{URL: "http://" + srv.Listener.Addr().String()}
	feed := fmt.Sprintf(
		rssTemplate,
		ReleaseTitle,
		ReleaseGUID,
		t.URL+DownloadPath,
		ReleasePubDate,
		ReleaseSize,
		ReleaseSeeders,
		ReleasePeers,
	)
	torrent := t.torrent()

	mux.HandleFunc(
		"GET "+DownloadPath,
		func(w http.ResponseWriter, _ *http.Request) {
			defer GinkgoRecover()
			w.Header().Set("Content-Type", "application/x-bittorrent")
			_, err := w.Write(torrent)
			Expect(err).NotTo(HaveOccurred())
		},
	)
	// Torznab pins no path — operators mount it wherever they like and the
	// client only varies the query string — so the API answers on any route.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		defer GinkgoRecover()
		if r.URL.Query().Get("apikey") != APIKey {
			http.Error(w, "bad apikey", http.StatusUnauthorized)
			return
		}
		switch mode := r.URL.Query().Get("t"); mode {
		case "caps":
			writeXML(w, capsXML)
		case "search", "movie", "tvsearch":
			writeXML(w, feed)
		default:
			http.Error(w, "unsupported t="+mode, http.StatusBadRequest)
		}
	})

	srv.Start()
	DeferCleanup(srv.Close)
	return t
}

// Content returns the payload the canned torrent describes — one fixed
// sequence of bytes, the same for every fake. The pipeline spec writes exactly
// these to disk so qBittorrent's recheck reaches 100%. Rebuilt per call rather
// than cached: one transient 64 MiB allocation beats pinning that much for a
// whole suite.
func (t *Torznab) Content() []byte { return content() }

func content() []byte {
	buf := make([]byte, ReleaseSize)
	for i := range buf {
		buf[i] = byte(i % 251)
	}
	return buf
}

// infoBytes is the bencoded info dictionary, identical for every fake — only
// the announce URL varies between them — so the pass over 64 MiB of content
// happens once per process instead of once per fake.
var infoBytes = sync.OnceValue(func() []byte {
	GinkgoHelper()
	payload := content()
	pieces := make([]byte, 0, sha1.Size*(ReleaseSize/pieceLength))
	for off := 0; off < len(payload); off += pieceLength {
		sum := sha1.Sum(payload[off : off+pieceLength])
		pieces = append(pieces, sum[:]...)
	}
	info, err := bencode.Marshal(metainfo.Info{
		Name:        TorrentName,
		Length:      int64(len(payload)),
		PieceLength: pieceLength,
		Pieces:      pieces,
	})
	Expect(err).NotTo(HaveOccurred())
	return info
})

func (t *Torznab) torrent() []byte {
	GinkgoHelper()
	mi := metainfo.MetaInfo{
		Announce:  t.URL + "/announce",
		InfoBytes: infoBytes(),
	}
	var buf bytes.Buffer
	Expect(mi.Write(&buf)).To(Succeed())
	return buf.Bytes()
}

func writeXML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/xml")
	_, err := io.WriteString(w, body)
	Expect(err).NotTo(HaveOccurred())
}
