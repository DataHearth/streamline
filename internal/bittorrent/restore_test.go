package bittorrent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/download"
)

// readyNow is the readiness of an engine whose boot restore is already done.
func readyNow() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// localTorrentFile returns a .torrent for data already in the returned dir.
func localTorrentFile() ([]byte, string) {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	Expect(os.WriteFile(
		filepath.Join(dir, "a.bin"), []byte("restore me"), 0o644,
	)).To(Succeed())
	info := metainfo.Info{PieceLength: 1 << 20}
	Expect(info.BuildFromFilePath(dir)).To(Succeed())
	ib, err := bencode.Marshal(info)
	Expect(err).NotTo(HaveOccurred())
	raw, err := bencode.Marshal(metainfo.MetaInfo{InfoBytes: ib})
	Expect(err).NotTo(HaveOccurred())
	return raw, dir
}

var _ = Describe("Engine restore", Label("unit", "bittorrent"), func() {
	const unknown = "00112233445566778899aabbccddeeff00112233"

	var (
		ctx     context.Context
		store   *mocks.MockStore
		e       *Engine
		raw     []byte
		hash    string
		name    string
		stopped bool
	)

	BeforeEach(func() {
		ctx = context.Background()
		var dir string
		raw, dir = localTorrentFile()
		mi, err := metainfo.Load(bytes.NewReader(raw))
		Expect(err).NotTo(HaveOccurred())
		hash = mi.HashInfoBytes().HexString()
		name = filepath.Base(dir)

		store = mocks.NewMockStore(GinkgoT())
		// startWhenReady records the resolved name once the info is in.
		store.EXPECT().
			SetTorrentSessionName(mock.Anything, hash, mock.Anything).
			Return(nil).Maybe()
		e = &Engine{
			ready:     readyNow(),
			fatal:     make(chan error, 1),
			restoring: map[string]struct{}{},
			client:    newTestClient(dir),
			store:     store,
			paths:     newContentPaths(),
			state:     map[string]*torrentState{},
			sample:    map[string]speedSample{},
			stop:      make(chan struct{}),
		}
		stopped = false
		DeferCleanup(func() {
			if !stopped {
				close(e.stop)
			}
			e.wg.Wait()
		})
	})

	It("holds a session not re-added yet apart from a gone one", func() {
		store.EXPECT().ListTorrentSessions(mock.Anything).Return(
			[]*ent.TorrentSession{
				{InfoHash: hash, SourceTorrent: raw, Paused: true},
			},
			nil,
		).
			Once()

		pending, err := e.loadSessions(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(pending).To(HaveLen(1))

		_, err = e.GetTorrent(ctx, hash)
		Expect(err).To(MatchError(download.ErrClientNotReady))
		Expect(err).NotTo(MatchError(download.ErrTorrentNotFound))
		_, err = e.GetTorrent(ctx, unknown)
		Expect(err).To(MatchError(download.ErrTorrentNotFound))
		_, err = e.ListTorrents(ctx)
		Expect(err).To(MatchError(download.ErrClientNotReady))
		Expect(e.Runtime().RestorePending).To(Equal(1))
		Expect(e.Runtime().RestoreTotal).To(Equal(1))
		Expect(e.getState(hash).paused).To(BeTrue(),
			"the row's flags are loaded before the re-add")

		e.restoreSessions(ctx, pending)

		listed, err := e.ListTorrents(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(listed).To(HaveLen(1))
		Expect(listed[0].Hash).To(Equal(hash))
		got, err := e.GetTorrent(ctx, hash)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Status).To(Equal(download.StatusPaused))
		Expect(e.Runtime().RestorePending).To(BeZero())
	})

	It("claims each session's name before any re-add", func() {
		store.EXPECT().ListTorrentSessions(mock.Anything).Return(
			[]*ent.TorrentSession{{InfoHash: hash, SourceTorrent: raw}}, nil,
		).Once()

		_, err := e.loadSessions(ctx)
		Expect(err).NotTo(HaveOccurred())

		var ih, other metainfo.Hash
		Expect(ih.FromHexString(hash)).To(Succeed())
		Expect(other.FromHexString(unknown)).To(Succeed())
		info := &metainfo.Info{Name: name, Length: 1}
		Expect(e.paths.admits(GinkgoT().TempDir(), info, other)).To(BeFalse())
		Expect(e.paths.admits(GinkgoT().TempDir(), info, ih)).To(BeTrue())
	})

	It(
		"drops a row whose source no longer parses, and waits on nothing for it",
		func() {
			store.EXPECT().ListTorrentSessions(mock.Anything).Return(
				[]*ent.TorrentSession{
					{InfoHash: unknown, SourceTorrent: []byte("junk")},
				},
				nil,
			).
				Once()
			store.EXPECT().DeleteTorrentSessionByHash(mock.Anything, unknown).
				Return(nil).Once()

			pending, err := e.loadSessions(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(pending).To(BeEmpty())
			_, err = e.ListTorrents(ctx)
			Expect(err).NotTo(HaveOccurred())
		},
	)

	Describe("loadAndRestore", func() {
		var release chan struct{}

		BeforeEach(func() {
			release = make(chan struct{})
			e.loading = true
			e.ready = make(chan struct{})
		})

		It("holds engine calls until the rows are read and re-added", func() {
			store.EXPECT().ListTorrentSessions(mock.Anything).RunAndReturn(
				func(context.Context) ([]*ent.TorrentSession, error) {
					<-release
					return []*ent.TorrentSession{
						{InfoHash: hash, SourceTorrent: raw},
					}, nil
				},
			).Once()
			e.wg.Go(func() { e.loadAndRestore(ctx) })

			short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			_, err := e.GetTorrent(short, hash)
			Expect(err).To(MatchError(download.ErrClientNotReady))
			Expect(e.Runtime().RestoreLoading).To(BeTrue())

			got := make(chan error, 1)
			go func() {
				_, err := e.GetTorrent(ctx, hash)
				got <- err
			}()
			Consistently(got).ShouldNot(Receive())
			close(release)

			Eventually(got).Should(Receive(BeNil()))
			Expect(e.Ready()).To(BeClosed())
			_, err = e.GetTorrent(ctx, unknown)
			Expect(err).To(MatchError(download.ErrTorrentNotFound))
			Expect(e.Runtime().RestoreTotal).To(Equal(1))
		})

		It("reports a failed read as fatal and never reports ready", func() {
			store.EXPECT().ListTorrentSessions(mock.Anything).
				Return(nil, errors.New("disk")).Once()

			e.loadAndRestore(ctx)

			Expect(e.Fatal()).To(Receive(MatchError(ContainSubstring("disk"))))
			Expect(e.Ready()).NotTo(BeClosed())
		})

		It("releases waiting calls when the process shuts down", func() {
			quit := make(chan struct{})
			e.quit = quit
			got := make(chan error, 1)
			go func() {
				_, err := e.GetTorrent(ctx, hash)
				got <- err
			}()
			Consistently(got).ShouldNot(Receive())
			close(quit)

			Eventually(got).Should(Receive(MatchError(download.ErrClientNotReady)))
		})

		It("does not report ready when a shutdown interrupts the re-add", func() {
			store.EXPECT().ListTorrentSessions(mock.Anything).Return(
				[]*ent.TorrentSession{{InfoHash: hash, SourceTorrent: raw}}, nil,
			).Once()
			e.quit = readyNow()

			e.loadAndRestore(ctx)

			Expect(e.Ready()).NotTo(BeClosed())
		})

		It("reports nothing when Close stops the read", func() {
			store.EXPECT().ListTorrentSessions(mock.Anything).
				Return(nil, context.Canceled).Once()
			close(e.stop)
			stopped = true

			e.loadAndRestore(ctx)

			Expect(e.Fatal()).NotTo(Receive())
		})
	})

	It("keeps a session's trust when it gives its names up", func() {
		var ih metainfo.Hash
		Expect(ih.FromHexString(hash)).To(Succeed())
		e.paths.trust(ih)
		e.paths.claim(name, ih)

		e.paths.unclaim(ih)

		e.paths.mu.Lock()
		defer e.paths.mu.Unlock()
		Expect(e.paths.owners).NotTo(HaveKey(name))
		Expect(e.paths.trusted[ih]).To(BeTrue())
	})

	It("stops at Close, leaving what it did not reach pending", func() {
		store.EXPECT().ListTorrentSessions(mock.Anything).Return(
			[]*ent.TorrentSession{{InfoHash: hash, SourceTorrent: raw}}, nil,
		).Once()
		pending, err := e.loadSessions(ctx)
		Expect(err).NotTo(HaveOccurred())

		close(e.stop)
		stopped = true
		e.restoreSessions(ctx, pending)

		_, err = e.GetTorrent(ctx, hash)
		Expect(err).To(MatchError(download.ErrClientNotReady))
	})
})
