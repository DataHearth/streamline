package bittorrent

import (
	"context"
	"os"
	"path/filepath"

	antorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

// newSelectionTestTorrent mirrors newTestTorrent but also hands back the
// client it was added to, since SetWantedFiles/ListFiles resolve the torrent
// through Engine.client rather than a bare *antorrent.Torrent.
func newSelectionTestTorrent() (*antorrent.Client, *antorrent.Torrent) {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	for i, name := range []string{"a.bin", "b.bin", "c.bin"} {
		Expect(os.WriteFile(
			filepath.Join(dir, name), []byte{byte(i), byte(i), byte(i)}, 0o644,
		)).To(Succeed())
	}
	info := metainfo.Info{PieceLength: 1 << 20}
	Expect(info.BuildFromFilePath(dir)).To(Succeed())
	ib, err := bencode.Marshal(info)
	Expect(err).NotTo(HaveOccurred())
	mi := metainfo.MetaInfo{InfoBytes: ib}

	client := newTestClient(dir)
	t, err := client.AddTorrent(&mi)
	Expect(err).NotTo(HaveOccurred())
	<-t.GotInfo()
	Expect(t.VerifyDataContext(context.Background())).To(Succeed())
	return client, t
}

var _ = Describe("Engine file selection", Label("unit", "bittorrent"), func() {
	var (
		ctx    context.Context
		client *antorrent.Client
		t      *antorrent.Torrent
		hash   string
		store  *dbmocks.MockStore
		eng    *Engine
	)

	BeforeEach(func() {
		ctx = context.Background()
		client, t = newSelectionTestTorrent()
		hash = t.InfoHash().HexString()
		store = dbmocks.NewMockStore(GinkgoT())
		eng = &Engine{
			client: client,
			store:  store,
			state:  map[string]*torrentState{},
		}
	})

	It(
		"SetWantedFiles persists explicit mode and survives a metadata re-resolve",
		func() {
			store.EXPECT().
				SetTorrentSessionSelection(mock.Anything, hash, "explicit", []int{1}).
				Return(nil).Once()

			Expect(eng.SetWantedFiles(ctx, hash, []int{1})).To(Succeed())
			// The §3.3 regression: re-run the resolve hook, selection must hold.
			st := eng.getState(hash)
			applyFilePriorities(t, st.selectionMode, st.wantedFiles)
			files, err := eng.ListFiles(ctx, hash)
			Expect(err).ToNot(HaveOccurred())
			Expect(files[0].Wanted).To(BeFalse())
			Expect(files[1].Wanted).To(BeTrue())
		},
	)

	It("ListFiles reports empty for a metadata-less magnet", func() {
		magnetHash, err := parseHash(
			"aabbccddeeff00112233445566778899aabbccdd",
		)
		Expect(err).NotTo(HaveOccurred())
		_, _ = client.AddTorrentInfoHash(magnetHash)

		files, err := eng.ListFiles(ctx, magnetHash.HexString())
		Expect(err).ToNot(HaveOccurred())
		Expect(files).To(BeEmpty())
	})
})
