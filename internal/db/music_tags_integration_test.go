package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
)

var _ = Describe("Music tag bookkeeping", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
		alb    *ent.Album
		tr     *ent.Track
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
		ar := client.Artist.Create().SetMbid("art").SetName("Nirvana").SaveX(ctx)
		alb = client.Album.Create().SetMbid("rg").SetTitle("Nevermind").
			SetArtist(ar).SaveX(ctx)
		tr = client.Track.Create().SetTitle("Lithium").SetPosition(1).
			SetAlbum(alb).SaveX(ctx)
	})

	file := func(state *entmediafile.TagsState, hash string) *ent.MediaFile {
		GinkgoHelper()
		return client.MediaFile.Create().SetPath("/m/" + hash).SetSize(1).
			SetTrack(tr).SetNillableTagsState(state).SetTagsHash(hash).SaveX(ctx)
	}
	st := func(s entmediafile.TagsState) *entmediafile.TagsState { return &s }
	reload := func(mf *ent.MediaFile) *ent.MediaFile {
		return client.MediaFile.GetX(ctx, mf.ID)
	}

	It(
		"queues written, failed and never-tagged files, keeping a deferred one deferred",
		func() {
			written := file(st(entmediafile.TagsStateWritten), "w")
			failed := file(st(entmediafile.TagsStateFailed), "f")
			client.MediaFile.UpdateOne(failed).SetTagFailures(5).ExecX(ctx)
			never := file(nil, "n")
			deferred := file(st(entmediafile.TagsStateDeferred), "d")

			n, err := store.MarkAlbumTags(ctx, []uint32{alb.ID}, MarkStale)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(3))

			for _, mf := range []*ent.MediaFile{written, failed, never} {
				got := reload(mf)
				Expect(*got.TagsState).To(Equal(entmediafile.TagsStatePending))
				Expect(got.TagFailures).To(BeZero())
				Expect(got.TagsHash).To(Equal(mf.TagsHash))
			}
			Expect(
				*reload(deferred).TagsState,
			).To(Equal(entmediafile.TagsStateDeferred))
		},
	)

	It("empties every hash on a forced mark, the deferred file's too", func() {
		written := file(st(entmediafile.TagsStateWritten), "w")
		deferred := file(st(entmediafile.TagsStateDeferred), "d")

		_, err := store.MarkAllMusicTags(ctx, MarkForce)
		Expect(err).NotTo(HaveOccurred())

		Expect(reload(written).TagsHash).To(BeEmpty())
		Expect(reload(deferred).TagsHash).To(BeEmpty())
		Expect(*reload(deferred).TagsState).To(Equal(entmediafile.TagsStateDeferred))
	})

	It("lists pending files before deferred ones, and nothing else", func() {
		deferred := file(st(entmediafile.TagsStateDeferred), "d")
		pending := file(st(entmediafile.TagsStatePending), "p")
		file(st(entmediafile.TagsStateWritten), "w")
		file(nil, "n")
		client.MediaFile.Create().SetPath("/movie").SetSize(1).
			SetTagsState(entmediafile.TagsStatePending).ExecX(ctx)

		got, err := store.ListMediaFilesForTagSync(ctx, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveLen(2))
		Expect(got[0].ID).To(Equal(pending.ID))
		Expect(got[1].ID).To(Equal(deferred.ID))
		Expect(got[0].Edges.Track.Edges.Album.ID).To(Equal(alb.ID))
	})

	It("fails a file at the cap and records the write that succeeds", func() {
		mf := file(st(entmediafile.TagsStatePending), "")
		Expect(store.RecordMediaFileTagFailure(ctx, mf.ID, "boom", 2)).To(Succeed())
		Expect(*reload(mf).TagsState).To(Equal(entmediafile.TagsStatePending))
		Expect(store.RecordMediaFileTagFailure(ctx, mf.ID, "boom", 2)).To(Succeed())
		Expect(*reload(mf).TagsState).To(Equal(entmediafile.TagsStateFailed))

		at := time.Now()
		Expect(
			store.SetMediaFileTagsWritten(ctx, mf.ID, TagWrite{
				Hash: "h", ArtHash: "a", Mtime: 7, Size: 99, At: at,
			}),
		).To(Succeed())
		got := reload(mf)
		Expect(*got.TagsState).To(Equal(entmediafile.TagsStateWritten))
		Expect(got.TagFailures).To(BeZero())
		Expect(got.TagsError).To(BeEmpty())
		Expect(got.TagsHash).To(Equal("h"))
		Expect(got.TagsArtHash).To(Equal("a"))
		Expect(got.TagsMtime).To(Equal(int64(7)))
		Expect(got.Size).To(Equal(int64(99)))
	})

	It(
		"names the torrents of the album's own grabs and of the packs that expected it",
		func() {
			client.DownloadRecord.Create().SetTitle("own").SetSize(1).
				SetTorrentHash("own").
				SetDownloadClientName("q").SetAlbum(alb).ExecX(ctx)
			client.DownloadRecord.Create().SetTitle("pack").SetSize(1).
				SetTorrentHash("pack").
				SetDownloadClientName("q").AddAlbums(alb).ExecX(ctx)
			client.DownloadRecord.Create().SetTitle("other").SetSize(1).
				SetTorrentHash("other").SetDownloadClientName("q").ExecX(ctx)

			got, err := store.AlbumTorrentHashes(ctx, alb.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(ConsistOf("own", "pack"))
		},
	)
	It("queues an unmanaged album's files when someone asks for that album", func() {
		client.Album.UpdateOneID(alb.ID).SetTagsManaged(false).ExecX(ctx)
		written := file(st(entmediafile.TagsStateWritten), "w")

		n, err := store.MarkAlbumTags(ctx, []uint32{alb.ID}, MarkStale)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(BeZero(), "the writer's own re-queue skips it")

		n, err = store.QueueAlbumTags(ctx, alb.ID, MarkForce)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(1))
		Expect(*reload(written).TagsState).To(Equal(entmediafile.TagsStatePending))
		Expect(reload(written).TagsHash).To(BeEmpty())
	})

	It(
		"rotates the edit check over written files of managed albums, never checked first",
		func() {
			old := file(st(entmediafile.TagsStateWritten), "old")
			client.MediaFile.UpdateOne(old).
				SetTagsCheckedAt(time.Now().Add(-time.Hour)).
				ExecX(ctx)
			never := file(st(entmediafile.TagsStateWritten), "never")
			file(st(entmediafile.TagsStatePending), "p")

			got, err := store.ListMediaFilesForEditCheck(ctx, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(2))
			Expect(got[0].ID).To(Equal(never.ID))
			Expect(got[1].ID).To(Equal(old.ID))

			client.Album.UpdateOneID(alb.ID).SetTagsManaged(false).ExecX(ctx)
			got, err = store.ListMediaFilesForEditCheck(ctx, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		},
	)

	It(
		"stores a track's overrides and names its album; an empty set clears them",
		func() {
			id, err := store.SetTrackTagOverrides(
				ctx,
				tr.ID,
				map[string][]string{"MOOD": {"Calm"}},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal(alb.ID))
			Expect(client.Track.GetX(ctx, tr.ID).TagOverrides).
				To(Equal(map[string][]string{"MOOD": {"Calm"}}))

			_, err = store.SetTrackTagOverrides(ctx, tr.ID, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(client.Track.GetX(ctx, tr.ID).TagOverrides).To(BeNil())
		},
	)
})
