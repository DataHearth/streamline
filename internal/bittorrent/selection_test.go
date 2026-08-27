package bittorrent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"time"

	antorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/download"
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

	It(
		"SetWantedFiles re-arming a seed-stopped torrent persists seed_stopped=false",
		func() {
			// newSelectionTestTorrent's data is already fully verified on disk,
			// so every file always reports BytesCompleted == Length regardless
			// of priority — filesHaveMissingBytes could never see a gap. Swap in
			// newPartialTorrent's shape (a.bin corrupted post-hash, so it alone
			// verifies incomplete) via a client-returning variant, mirroring why
			// newSelectionTestTorrent itself exists over newTestTorrent.
			pClient, pt := newPartialSelectionTestTorrent()
			pHash := pt.InfoHash().HexString()
			pStore := dbmocks.NewMockStore(GinkgoT())
			pEng := &Engine{
				client: pClient,
				store:  pStore,
				state:  map[string]*torrentState{},
			}
			// a.bin (index 0) was skipped under the prior explicit selection and
			// is the file with missing bytes; widening to include it re-arms.
			pEng.setState(pHash, func(s *torrentState) {
				s.seedStopped = true
				s.selectionMode = "explicit"
				s.wantedFiles = []int{1, 2}
			})

			pStore.EXPECT().
				SetTorrentSessionSelection(
					mock.Anything, pHash, "explicit", []int{0, 1, 2},
				).
				Return(nil).Once()
			pStore.EXPECT().
				SetTorrentSessionCompleted(mock.Anything, pHash, time.Time{}).
				Return(nil).Once()
			pStore.EXPECT().
				SetTorrentSessionSeedStopped(mock.Anything, pHash, false).
				Return(nil).Once()

			Expect(pEng.SetWantedFiles(ctx, pHash, []int{0, 1, 2})).To(Succeed())

			Expect(pEng.getState(pHash).seedStopped).To(BeFalse())
		},
	)
})

var _ = Describe("Engine.AddTorrent selection", Label("unit", "bittorrent"), func() {
	var (
		ctx   context.Context
		store *dbmocks.MockStore
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
	})

	It(
		"a selective magnet add parks pending selection and requests no data once metadata resolves",
		func() {
			magnet := "magnet:?xt=urn:btih:" +
				"aabbccddeeff00112233445566778899aabbccdd&dn=test"
			client := newTestClient(GinkgoT().TempDir())
			eng := &Engine{
				client: client,
				store:  store,
				state:  map[string]*torrentState{},
			}

			var captured db.CreateTorrentSessionParams
			store.EXPECT().
				CreateTorrentSession(mock.Anything, mock.Anything).
				Run(func(_ context.Context, p db.CreateTorrentSessionParams) {
					captured = p
				}).
				Return(nil, nil).Once()

			hash, err := eng.AddTorrent(ctx, download.TorrentSource{
				Magnet: magnet, Selective: true,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(hash).To(Equal(
				"aabbccddeeff00112233445566778899aabbccdd",
			))
			Expect(captured.SelectionMode).To(Equal("pending"))
			Expect(captured.WantedFiles).To(BeNil())

			st := eng.getState(hash)
			Expect(st.selectionMode).To(Equal("pending"))
			Expect(st.wantedFiles).To(BeNil())

			// The restore-cycle rule: re-run the resolve hook a real,
			// metadata-resolved torrent goes through once its info lands, off
			// exactly the state AddTorrent persisted. Every file must stay at
			// PiecePriorityNone — no data piece is ever requested.
			_, rt := newSelectionTestTorrent()
			applyFilePriorities(rt, st.selectionMode, st.wantedFiles)
			for _, f := range rt.Files() {
				Expect(f.Priority()).To(Equal(types.PiecePriorityNone))
			}
		},
	)

	It(
		"a WantedFiles bytes add persists explicit selection with the wanted files",
		func() {
			dir := GinkgoT().TempDir()
			for i, name := range []string{"a.bin", "b.bin", "c.bin"} {
				Expect(os.WriteFile(
					filepath.Join(dir, name),
					[]byte{byte(i), byte(i), byte(i)}, 0o644,
				)).To(Succeed())
			}
			info := metainfo.Info{PieceLength: 1 << 20}
			Expect(info.BuildFromFilePath(dir)).To(Succeed())
			ib, err := bencode.Marshal(info)
			Expect(err).NotTo(HaveOccurred())
			mi := metainfo.MetaInfo{InfoBytes: ib}
			var buf bytes.Buffer
			Expect(mi.Write(&buf)).To(Succeed())
			raw := buf.Bytes()

			client := newTestClient(dir)
			eng := &Engine{
				client: client,
				store:  store,
				state:  map[string]*torrentState{},
			}

			var captured db.CreateTorrentSessionParams
			store.EXPECT().
				CreateTorrentSession(mock.Anything, mock.Anything).
				Run(func(_ context.Context, p db.CreateTorrentSessionParams) {
					captured = p
				}).
				Return(nil, nil).Once()
			// startWhenReady fires synchronously for a bytes source (metadata is
			// already known) and persists the resolved name; incidental to what
			// this test asserts.
			store.EXPECT().
				SetTorrentSessionName(mock.Anything, mock.Anything, mock.Anything).
				Return(nil).Maybe()

			hash, err := eng.AddTorrent(ctx, download.TorrentSource{
				Bytes: raw, WantedFiles: []int{1},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(captured.SelectionMode).To(Equal("explicit"))
			Expect(captured.WantedFiles).To(Equal([]int{1}))

			st := eng.getState(hash)
			Expect(st.selectionMode).To(Equal("explicit"))
			Expect(st.wantedFiles).To(Equal([]int{1}))
		},
	)

	It(
		"a duplicate-hash add leaves the first add's stored selection untouched",
		func() {
			magnet := "magnet:?xt=urn:btih:" +
				"aabbccddeeff00112233445566778899aabbccdd&dn=test"
			client := newTestClient(GinkgoT().TempDir())
			eng := &Engine{
				client: client,
				store:  store,
				state:  map[string]*torrentState{},
			}

			// First add: a genuinely fresh selective grab.
			store.EXPECT().
				CreateTorrentSession(mock.Anything, mock.Anything).
				Return(nil, nil).Once()
			hash, err := eng.AddTorrent(ctx, download.TorrentSource{
				Magnet: magnet, Selective: true,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(eng.getState(hash).selectionMode).To(Equal("pending"))

			// Second add on the same hash (e.g. a completed, non-widen-eligible
			// record falling through to a normal grab): CreateTorrentSession
			// swallows a constraint error for the already-existing row, and
			// this call carries no selection at all — it must not clobber
			// what the first add persisted, in memory or (by construction,
			// since CreateTorrentSession is create-only) on disk.
			store.EXPECT().
				CreateTorrentSession(mock.Anything, mock.Anything).
				Return(nil, &ent.ConstraintError{}).Once()
			hash2, err := eng.AddTorrent(ctx, download.TorrentSource{
				Magnet: magnet,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(hash2).To(Equal(hash))

			st := eng.getState(hash)
			Expect(st.selectionMode).To(Equal("pending"))
			Expect(st.wantedFiles).To(BeNil())
		},
	)
})

// newPartialSelectionTestTorrent mirrors newPartialTorrent (engine_test.go)
// but also hands back the client, for the same reason
// newSelectionTestTorrent does: SetWantedFiles/ListFiles resolve the
// torrent through Engine.client, not a bare *antorrent.Torrent. File 0
// (a.bin) is corrupted after the metainfo hash is computed from its
// original content, so it alone verifies incomplete.
func newPartialSelectionTestTorrent() (*antorrent.Client, *antorrent.Torrent) {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	for i, name := range []string{"a.bin", "b.bin", "c.bin"} {
		Expect(os.WriteFile(
			filepath.Join(dir, name), []byte{byte(i), byte(i), byte(i)}, 0o644,
		)).To(Succeed())
	}
	info := metainfo.Info{PieceLength: 3}
	Expect(info.BuildFromFilePath(dir)).To(Succeed())
	ib, err := bencode.Marshal(info)
	Expect(err).NotTo(HaveOccurred())
	mi := metainfo.MetaInfo{InfoBytes: ib}

	Expect(os.WriteFile(
		filepath.Join(dir, "a.bin"), []byte{9, 9, 9}, 0o644,
	)).To(Succeed())

	client := newTestClient(dir)
	t, err := client.AddTorrent(&mi)
	Expect(err).NotTo(HaveOccurred())
	<-t.GotInfo()
	Expect(t.VerifyDataContext(context.Background())).To(Succeed())
	return client, t
}
