package api

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/containers"
	"github.com/datahearth/streamline/internal/download"
)

// selectivePieceLength equals each fixture file's size, so every piece maps
// to exactly one file. That alignment is what lets completing only the
// wanted file's piece finish the torrent — a piece straddling a skipped file
// would stay "needed" regardless of priority.
const selectivePieceLength = 16 * 1024

// selectiveFile is one fixture file: deterministic content plus its name
// within the torrent's root directory.
type selectiveFile struct {
	name    string
	content []byte
}

// buildSelectiveTorrent authors a three-file .torrent in memory — no binary
// fixture on disk, mirroring internal/download's own selection specs and
// e2e/fakes/torznab.go's real-hash approach (unlike the unit specs' fake
// zeroed Pieces, a real container hashes on recheck, so the piece hashes
// here must be genuine).
func buildSelectiveTorrent(rootName string) ([]byte, []selectiveFile) {
	GinkgoHelper()
	files := make([]selectiveFile, 3)
	metaFiles := make([]metainfo.FileInfo, 3)
	pieces := make([]byte, 0, sha1.Size*3)
	for i := range files {
		content := make([]byte, selectivePieceLength)
		for b := range content {
			content[b] = byte((b + i*37) % 251)
		}
		name := fmt.Sprintf("file%d.bin", i)
		files[i] = selectiveFile{name: name, content: content}
		metaFiles[i] = metainfo.FileInfo{
			Path:   []string{name},
			Length: selectivePieceLength,
		}
		sum := sha1.Sum(content)
		pieces = append(pieces, sum[:]...)
	}

	info, err := bencode.Marshal(metainfo.Info{
		Name:        rootName,
		Files:       metaFiles,
		PieceLength: selectivePieceLength,
		Pieces:      pieces,
	})
	Expect(err).NotTo(HaveOccurred())
	mi := metainfo.MetaInfo{InfoBytes: info}
	var buf bytes.Buffer
	Expect(mi.Write(&buf)).To(Succeed())
	return buf.Bytes(), files
}

var _ = Describe(
	"qBittorrent file selection",
	Label("e2e", "containers"),
	func() {
		It("downloads only the wanted file and still completes", func() {
			containers.Require()
			qb := containers.StartQBittorrent(downloadDir)
			// Constructed directly against internal/download.QBittorrent
			// rather than through streamline's REST API: this spec proves the
			// client-library mapping (WantedFiles -> a real selection) against
			// a real daemon, not the grab pipeline built on top of it — that
			// pipeline is pipeline_test.go's job.
			client := download.NewQBittorrentPassword(
				qb.URL(""), "e2e", "e2e-secret",
			)

			const rootName = "e2e-selective"
			torrentBytes, files := buildSelectiveTorrent(rootName)

			ctx := context.Background()
			hash, err := client.AddTorrent(ctx, download.TorrentSource{
				Bytes:       torrentBytes,
				WantedFiles: []int{1},
				Selective:   true,
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { qb.Remove(hash) })

			By("marking indexes 0 and 2 skipped, index 1 wanted")
			Eventually(func() ([]download.TorrentFile, error) {
				return client.ListFiles(ctx, hash)
			}).
				WithTimeout(hopBudget).
				WithPolling(hopPoll).
				Should(SatisfyAll(
					HaveLen(3),
					ContainElement(SatisfyAll(
						HaveField("Index", 0),
						HaveField("Wanted", false),
					)),
					ContainElement(SatisfyAll(
						HaveField("Index", 1),
						HaveField("Wanted", true),
					)),
					ContainElement(SatisfyAll(
						HaveField("Index", 2),
						HaveField("Wanted", false),
					)),
				))

			By("handing qBittorrent only the wanted file's bytes")
			qb.Stop(hash)
			wantedPath := filepath.Join(downloadDir, rootName, files[1].name)
			Expect(os.MkdirAll(filepath.Dir(wantedPath), 0o755)).To(Succeed())
			Expect(os.WriteFile(wantedPath, files[1].content, 0o644)).To(Succeed())
			qb.Recheck(hash)

			// files[0] and files[2] are deliberately never written — with no
			// peers and a skipped priority, qBittorrent has no other source
			// for them. Reaching completion anyway is the proof that the
			// selection reached the daemon, not just the request that
			// created it.
			By("reaching completion without the skipped files ever supplied")
			Eventually(func() []containers.Torrent {
				return qb.Torrents(managedCategory)
			}).
				WithTimeout(hopBudget).
				WithPolling(hopPoll).
				Should(ContainElement(SatisfyAll(
					HaveField("Hash", hash),
					HaveField("Progress", BeNumerically("==", 1)),
					HaveField("State", HaveSuffix("UP")),
				)))
		})
	},
)
