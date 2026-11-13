package bittorrent

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

var _ = Describe("dataRemovals", Label("unit", "bittorrent"), func() {
	const hash = "0100000000000000000000000000000000000000"

	var (
		ctx   context.Context
		store *dbmocks.MockStore
		paths *contentPaths
		r     *dataRemovals
		base  string
		file  string
	)
	info := &metainfo.Info{
		Name:        "Some.Release",
		PieceLength: 16384,
		Files:       []metainfo.FileInfo{{Path: []string{"a.mkv"}, Length: 4}},
	}
	infoBytes, err := bencode.Marshal(info)
	Expect(err).NotTo(HaveOccurred())

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		paths = newContentPaths()
		r = newDataRemovals(store, paths)
		base = GinkgoT().TempDir()
		file = filepath.Join(base, "Some.Release", "a.mkv")
		Expect(os.MkdirAll(filepath.Dir(file), 0o755)).To(Succeed())
		Expect(os.WriteFile(file, []byte("data"), 0o644)).To(Succeed())
	})

	owe := func() {
		GinkgoHelper()
		store.EXPECT().
			KeepTorrentDataRemoval(mock.Anything, db.TorrentDataRemovalParams{
				InfoHash: hash, Name: "Some.Release", Dir: base, Info: infoBytes,
			}).
			Return(nil).
			Once()
		Expect(r.owe(ctx, hash, base, info)).To(Succeed())
	}

	// lock makes the release folder refuse deletes for the spec's duration.
	lock := func() {
		GinkgoHelper()
		if os.Geteuid() == 0 {
			Skip("root deletes from a read-only directory")
		}
		dir := filepath.Dir(file)
		Expect(os.Chmod(dir, 0o555)).To(Succeed())
		DeferCleanup(func() {
			// TempDir's own cleanup cannot empty a read-only folder; a spec
			// that finished the removal has none left to unlock.
			if _, err := os.Stat(dir); err == nil {
				Expect(os.Chmod(dir, 0o755)).To(Succeed())
			}
		})
	}

	It("does not drop the torrent when the removal cannot be recorded", func() {
		store.EXPECT().
			KeepTorrentDataRemoval(mock.Anything, mock.Anything).
			Return(errors.New("db boom")).
			Once()

		Expect(r.owe(ctx, hash, base, info)).NotTo(Succeed())
		Expect(r.views()).To(BeEmpty())
	})

	It("deletes the files and forgets the removal", func() {
		owe()
		store.EXPECT().
			DeleteTorrentDataRemoval(mock.Anything, hash).
			Return(nil).
			Once()

		r.attempt(ctx, hash)

		Expect(file).NotTo(BeAnExistingFile())
		Expect(filepath.Dir(file)).NotTo(BeADirectory())
		Expect(r.views()).To(BeEmpty())
	})

	It("keeps a removal whose files could not be deleted, with the reason", func() {
		owe()
		lock()

		r.attempt(ctx, hash)

		Expect(file).To(BeAnExistingFile())
		views := r.views()
		Expect(views).To(HaveLen(1))
		Expect(views[0].Hash).To(Equal(hash))
		Expect(views[0].Name).To(Equal("Some.Release"))
		Expect(views[0].Path).To(Equal(filepath.Join(base, "Some.Release")))
		Expect(views[0].Error).To(ContainSubstring("permission denied"))
	})

	It("finishes a kept removal on a later pass", func() {
		owe()
		lock()
		r.attempt(ctx, hash)
		Expect(os.Chmod(filepath.Dir(file), 0o755)).To(Succeed())
		store.EXPECT().
			DeleteTorrentDataRemoval(mock.Anything, hash).
			Return(nil).
			Once()

		r.retryAll(ctx)

		Expect(file).NotTo(BeAnExistingFile())
		Expect(r.views()).To(BeEmpty())
	})

	It("gives the files up to a torrent that took the name since", func() {
		owe()
		Expect(paths.torrentDir(base, info, metainfo.Hash{9})).To(Equal(base))
		store.EXPECT().
			DeleteTorrentDataRemoval(mock.Anything, hash).
			Return(nil).
			Once()

		r.attempt(ctx, hash)

		Expect(file).To(BeAnExistingFile())
		Expect(r.views()).To(BeEmpty())
	})

	It("lets nothing be placed under the name while it deletes", func() {
		Expect(paths.hold("Some.Release")).To(BeTrue())
		Expect(paths.admits(base, info, metainfo.Hash{1})).To(BeFalse())

		paths.unhold("Some.Release")
		Expect(paths.admits(base, info, metainfo.Hash{1})).To(BeTrue())
	})

	It("loads what an earlier process still owed", func() {
		store.EXPECT().
			ListTorrentDataRemovals(mock.Anything).
			Return([]*ent.TorrentDataRemoval{
				{InfoHash: hash, Name: "Some.Release", Dir: base, Info: infoBytes},
				{InfoHash: "bad", Name: "Broken", Dir: base, Info: []byte("x")},
			}, nil).
			Once()
		store.EXPECT().
			DeleteTorrentDataRemoval(mock.Anything, "bad").
			Return(nil).
			Once()

		Expect(r.load(ctx)).To(Succeed())

		views := r.views()
		Expect(views).To(HaveLen(1))
		Expect(views[0].Hash).To(Equal(hash))
	})
})
