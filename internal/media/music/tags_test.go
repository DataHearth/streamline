package music

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/scheduler"
)

var _ = Describe("Music tag sync", Label("unit", "integration", "music"), func() {
	var (
		e        *env
		musicDir string
		dlDir    string
		alb      *ent.Album
		tr       *ent.Track
	)

	tagConfig := func(tags map[string]any) {
		GinkgoHelper()
		e.setConfig(map[string]any{"library": map[string]any{
			"music_path":        musicDir,
			"download_path":     dlDir,
			"no_match_cooldown": "6h",
			"max_grab_failures": 3,
			"music_tags":        tags,
		}})
	}

	// placeFile copies a tagged fixture to rel under the music directory and
	// records it against the track, in the given tag state.
	placeFile := func(rel string, state *entmediafile.TagsState) *ent.MediaFile {
		GinkgoHelper()
		data, err := os.ReadFile("../../library/audiotags/testdata/tagged.flac")
		Expect(err).NotTo(HaveOccurred())
		path := filepath.Join(musicDir, rel)
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, data, 0o644)).To(Succeed())
		return e.client.MediaFile.Create().
			SetPath(path).SetSize(int64(len(data))).SetFormat("flac").
			SetTrackID(tr.ID).SetNillableTagsState(state).SaveX(e.ctx)
	}

	pending := func() *entmediafile.TagsState {
		st := entmediafile.TagsStatePending
		return &st
	}

	reload := func(mf *ent.MediaFile) *ent.MediaFile {
		GinkgoHelper()
		return e.client.MediaFile.GetX(e.ctx, mf.ID)
	}

	titleOf := func(path string) string {
		GinkgoHelper()
		tags, err := audiotags.ReadTags(path)
		Expect(err).NotTo(HaveOccurred())
		if len(tags[audiotags.KeyTitle]) == 0 {
			return ""
		}
		return tags[audiotags.KeyTitle][0]
	}

	noTorrents := func() {
		e.dl.EXPECT().SeedingTorrents(mock.Anything).
			Return(download.NewSeedingSet(true, nil, nil)).Maybe()
	}

	BeforeEach(func() {
		e = newEnv(false)
		stubKindPaths(e.posters, artistKind)
		musicDir = GinkgoT().TempDir()
		dlDir = GinkgoT().TempDir()
		tagConfig(map[string]any{"mode": config.MusicTagsImport})
		_, albums := e.seedArtist("Nirvana",
			albumSeed{mbid: "rg-1", title: "Nevermind", tracks: []string{"Lithium"}})
		alb = albums[0]
		tr = alb.QueryTracks().OnlyX(e.ctx)
	})

	It("writes a pending file, records what it wrote and settles it", func() {
		noTorrents()
		mf := placeFile("Nirvana/Nevermind/01.flac", pending())

		Expect(e.svc.SyncTags(e.ctx)).To(Succeed())

		got := reload(mf)
		Expect(*got.TagsState).To(Equal(entmediafile.TagsStateWritten))
		Expect(got.TagsHash).NotTo(BeEmpty())
		Expect(got.TaggedAt).NotTo(BeNil())
		st, err := os.Stat(mf.Path)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.TagsMtime).To(Equal(st.ModTime().UnixNano()))
		Expect(got.Size).To(Equal(st.Size()))
		Expect(titleOf(mf.Path)).To(Equal("Lithium"))
	})

	It(
		"writes the full tag set, keeps what it does not own and drops a tracker comment",
		func() {
			noTorrents()
			mf := placeFile("Nirvana/Nevermind/01.flac", pending())
			Expect(audiotags.Write(mf.Path, audiotags.Tags{
				"REPLAYGAIN_TRACK_GAIN": {"-7.20 dB"},
				"COMMENT":               {"seeded at tracker.example"},
				audiotags.KeyLabel:      {"Some Bootleg Label"},
			}, audiotags.Policy{})).To(Succeed())
			e.client.Album.UpdateOneID(alb.ID).
				SetReleaseMbid("rel-1").SetOriginalDate("1991-09-24").
				SetEditionDate("2011-09-26").SetGenres([]string{"Grunge"}).
				SetLabel("").SetBarcode("720642442524").ExecX(e.ctx)
			e.client.Track.UpdateOneID(tr.ID).
				SetReleaseTrackMbid("reltrack-1").
				ExecX(e.ctx)
			e.client.MusicCredit.Create().SetKind("featuring").SetName("A Guest").
				SetMbid("g-1").SetOrdinal(0).SetTrackID(tr.ID).ExecX(e.ctx)
			tagConfig(map[string]any{
				"mode": config.MusicTagsImport, "strip": []string{"COMMENT"},
			})

			Expect(e.svc.SyncTags(e.ctx)).To(Succeed())

			tags, err := audiotags.ReadTags(mf.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(
				tags,
			).To(HaveKeyWithValue(audiotags.KeyArtist, []string{"Nirvana feat. A Guest"}))
			Expect(
				tags,
			).To(HaveKeyWithValue(audiotags.KeyArtists, []string{"Nirvana", "A Guest"}))
			Expect(
				tags,
			).To(HaveKeyWithValue(audiotags.KeyAlbumArtist, []string{"Nirvana"}))
			Expect(
				tags,
			).To(HaveKeyWithValue(audiotags.KeyMBAlbumID, []string{"rel-1"}))
			Expect(
				tags,
			).To(HaveKeyWithValue(audiotags.KeyMBReleaseGroupID, []string{"rg-1"}))
			Expect(
				tags,
			).To(HaveKeyWithValue(audiotags.KeyMBReleaseTrackID, []string{"reltrack-1"}))
			Expect(
				tags,
			).To(HaveKeyWithValue(audiotags.KeyDate, []string{"2011-09-26"}))
			Expect(
				tags,
			).To(HaveKeyWithValue(audiotags.KeyOriginalDate, []string{"1991-09-24"}))
			Expect(tags).To(HaveKeyWithValue(audiotags.KeyGenre, []string{"Grunge"}))
			Expect(
				tags,
			).To(HaveKeyWithValue(audiotags.KeyBarcode, []string{"720642442524"}))
			Expect(
				tags,
			).To(HaveKeyWithValue("REPLAYGAIN_TRACK_GAIN", []string{"-7.20 dB"}))
			Expect(tags).NotTo(HaveKey("COMMENT"))
			Expect(tags).NotTo(HaveKey(audiotags.KeyLabel))
		},
	)

	It("does not touch a file whose tags already match", func() {
		noTorrents()
		mf := placeFile("Nirvana/Nevermind/01.flac", pending())
		Expect(e.svc.SyncTags(e.ctx)).To(Succeed())
		before, err := os.Stat(mf.Path)
		Expect(err).NotTo(HaveOccurred())

		_, err = e.store.MarkAlbumTags(e.ctx, []uint32{alb.ID}, db.MarkStale)
		Expect(err).NotTo(HaveOccurred())
		time.Sleep(10 * time.Millisecond)
		Expect(e.svc.SyncTags(e.ctx)).To(Succeed())

		after, err := os.Stat(mf.Path)
		Expect(err).NotTo(HaveOccurred())
		Expect(after.ModTime()).To(Equal(before.ModTime()))
		Expect(*reload(mf).TagsState).To(Equal(entmediafile.TagsStateWritten))
	})

	It("rewrites a file a forced retag reaches, whatever it carries", func() {
		noTorrents()
		mf := placeFile("Nirvana/Nevermind/01.flac", pending())
		Expect(e.svc.SyncTags(e.ctx)).To(Succeed())
		Expect(audiotags.Write(mf.Path, audiotags.Tags{
			audiotags.KeyTitle: {"Edited elsewhere"},
		}, audiotags.Policy{})).To(Succeed())

		n, err := e.svc.RetagLibrary(e.ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(1))
		Expect(e.svc.SyncTags(e.ctx)).To(Succeed())

		Expect(titleOf(mf.Path)).To(Equal("Lithium"))
	})

	It("leaves the files alone when tags are off", func() {
		tagConfig(map[string]any{"mode": config.MusicTagsOff})
		mf := placeFile("Nirvana/Nevermind/01.flac", pending())

		Expect(e.svc.SyncTags(e.ctx)).To(Succeed())

		Expect(*reload(mf).TagsState).To(Equal(entmediafile.TagsStatePending))
		Expect(titleOf(mf.Path)).To(Equal("Smells Like Teen Spirit"))
		_, err := e.svc.RetagLibrary(e.ctx)
		Expect(err).To(MatchError(ErrTagsOff))
	})

	Describe("the seeding guard", func() {
		It(
			"defers a file a listed torrent serves, and writes it once the torrent is gone",
			func() {
				mf := placeFile("Nirvana/Nevermind/01.flac", pending())
				root := filepath.Join(musicDir, "Nirvana")
				e.dl.EXPECT().SeedingTorrents(mock.Anything).
					Return(download.NewSeedingSet(true, []string{root}, []string{"abc"})).
					Once()

				Expect(e.svc.SyncTags(e.ctx)).To(Succeed())
				Expect(
					*reload(mf).TagsState,
				).To(Equal(entmediafile.TagsStateDeferred))
				Expect(titleOf(mf.Path)).To(Equal("Smells Like Teen Spirit"))

				e.dl.EXPECT().SeedingTorrents(mock.Anything).
					Return(download.NewSeedingSet(true, nil, nil)).Once()
				Expect(e.svc.SyncTags(e.ctx)).To(Succeed())
				Expect(
					*reload(mf).TagsState,
				).To(Equal(entmediafile.TagsStateWritten))
				Expect(titleOf(mf.Path)).To(Equal("Lithium"))
			},
		)

		Describe("a library file hardlinked to the torrent's", func() {
			var mf *ent.MediaFile
			var seed string

			BeforeEach(func() {
				mf = placeFile("Nirvana/Nevermind/01.flac", pending())
				seed = filepath.Join(dlDir, "Nevermind", "01.flac")
				Expect(os.MkdirAll(filepath.Dir(seed), 0o755)).To(Succeed())
				Expect(os.Link(mf.Path, seed)).To(Succeed())
				e.client.DownloadRecord.Create().
					SetTitle("Nirvana - Nevermind").SetSize(1).
					SetTorrentHash("ABC").SetDownloadClientName("qbit").
					SetAlbumID(alb.ID).SaveX(e.ctx)
			})

			live := func() {
				e.dl.EXPECT().SeedingTorrents(mock.Anything).
					Return(download.NewSeedingSet(true, nil, []string{"abc"})).Once()
			}

			It("waits for the torrent under seeding: defer", func() {
				live()
				Expect(e.svc.SyncTags(e.ctx)).To(Succeed())

				Expect(
					*reload(mf).TagsState,
				).To(Equal(entmediafile.TagsStateDeferred))
				Expect(titleOf(seed)).To(Equal("Smells Like Teen Spirit"))
			})

			It(
				"splits the link and tags at once under seeding: copy, leaving the torrent's bytes",
				func() {
					tagConfig(map[string]any{
						"mode":    config.MusicTagsImport,
						"seeding": config.MusicTagsSeedingCopy,
					})
					live()
					Expect(e.svc.SyncTags(e.ctx)).To(Succeed())

					Expect(
						*reload(mf).TagsState,
					).To(Equal(entmediafile.TagsStateWritten))
					Expect(titleOf(mf.Path)).To(Equal("Lithium"))
					Expect(titleOf(seed)).To(Equal("Smells Like Teen Spirit"))
				},
			)

			It("tags it once the torrent is gone, even with its files kept", func() {
				e.dl.EXPECT().SeedingTorrents(mock.Anything).
					Return(download.NewSeedingSet(true, nil, nil)).Once()
				Expect(e.svc.SyncTags(e.ctx)).To(Succeed())

				Expect(titleOf(mf.Path)).To(Equal("Lithium"))
				Expect(titleOf(seed)).To(Equal("Smells Like Teen Spirit"))
			})

			It("waits when a download client could not be listed", func() {
				e.dl.EXPECT().SeedingTorrents(mock.Anything).
					Return(download.NewSeedingSet(false, nil, nil)).Once()
				Expect(e.svc.SyncTags(e.ctx)).To(Succeed())

				Expect(
					*reload(mf).TagsState,
				).To(Equal(entmediafile.TagsStateDeferred))
			})

			It(
				"waits for a listed torrent no download record names, by the file it shares",
				func() {
					// Adopted from a library another tool hardlinked out of
					// the client's downloads: no record, so no hash ties
					// the album to the torrent.
					e.client.DownloadRecord.Delete().ExecX(e.ctx)
					e.dl.EXPECT().SeedingTorrents(mock.Anything).
						Return(download.NewSeedingSet(
							true, []string{filepath.Join(dlDir, "Nevermind")},
							[]string{"def"},
						)).Once()

					Expect(e.svc.SyncTags(e.ctx)).To(Succeed())

					Expect(
						*reload(mf).TagsState,
					).To(Equal(entmediafile.TagsStateDeferred))
					Expect(titleOf(mf.Path)).To(Equal("Smells Like Teen Spirit"))
				},
			)
		})
	})

	It("never puts back a file deleted while its write waited on the album", func() {
		mf := placeFile("Nirvana/Nevermind/01.flac", pending())
		listed := make(chan struct{})
		e.dl.EXPECT().SeedingTorrents(mock.Anything).
			RunAndReturn(func(context.Context) download.SeedingSet {
				close(listed)
				return download.NewSeedingSet(true, nil, nil)
			}).Once()
		unlock := library.LockAlbumFiles(alb.ID)
		done := make(chan error)
		go func() { done <- e.svc.SyncTags(e.ctx) }()

		// The pass has listed the file; a track-file delete lands first.
		Eventually(listed).Should(BeClosed())
		Expect(os.Remove(mf.Path)).To(Succeed())
		e.client.MediaFile.DeleteOneID(mf.ID).ExecX(e.ctx)
		unlock()

		Eventually(done).Should(Receive(BeNil()))
		Expect(mf.Path).NotTo(BeAnExistingFile())
		Expect(dirEntries(filepath.Dir(mf.Path))).To(BeEmpty())
	})

	Describe("failures", func() {
		It("counts failed writes and gives up after the cap", func() {
			noTorrents()
			mf := placeFile("Nirvana/Nevermind/01.flac", pending())
			Expect(os.WriteFile(mf.Path, []byte("not audio"), 0o600)).To(Succeed())

			for range int(maxTagFailures) - 1 {
				Expect(e.svc.SyncTags(e.ctx)).To(Succeed())
			}
			got := reload(mf)
			Expect(*got.TagsState).To(Equal(entmediafile.TagsStatePending))
			Expect(got.TagFailures).To(Equal(maxTagFailures - 1))
			Expect(got.TagsError).NotTo(BeEmpty())

			Expect(e.svc.SyncTags(e.ctx)).To(Succeed())
			Expect(*reload(mf).TagsState).To(Equal(entmediafile.TagsStateFailed))
		})

		It("fails a format it cannot write at once", func() {
			noTorrents()
			mf := placeFile("Nirvana/Nevermind/01.flac", pending())
			wav := mf.Path[:len(mf.Path)-len(".flac")] + ".wav"
			Expect(os.Rename(mf.Path, wav)).To(Succeed())
			e.client.MediaFile.UpdateOneID(mf.ID).SetPath(wav).ExecX(e.ctx)

			Expect(e.svc.SyncTags(e.ctx)).To(Succeed())

			Expect(*reload(mf).TagsState).To(Equal(entmediafile.TagsStateFailed))
		})
	})

	Describe("what queues a file", func() {
		It(
			"queues an album's files after hydration under managed, not under import",
			func() {
				noTorrents()
				mf := placeFile("Nirvana/Nevermind/01.flac", pending())
				Expect(e.svc.SyncTags(e.ctx)).To(Succeed())
				e.provider.EXPECT().GetReleaseGroup(mock.Anything, "rg-1").
					Return(hydrated("rg-1", "Lithium"), nil)

				Expect(
					e.svc.hydrateAlbum(e.ctx, alb.ID, true),
				).Error().
					NotTo(HaveOccurred())
				Expect(
					*reload(mf).TagsState,
				).To(Equal(entmediafile.TagsStateWritten))

				tagConfig(map[string]any{"mode": config.MusicTagsManaged})
				Expect(
					e.svc.hydrateAlbum(e.ctx, alb.ID, true),
				).Error().
					NotTo(HaveOccurred())
				Expect(
					*reload(mf).TagsState,
				).To(Equal(entmediafile.TagsStatePending))
			},
		)

		It(
			"queues a shared album's files when its owner is deleted, under managed",
			func() {
				noTorrents()
				mf := placeFile("Nirvana/Nevermind/01.flac", pending())
				Expect(e.svc.SyncTags(e.ctx)).To(Succeed())
				Expect(
					*reload(mf).TagsState,
				).To(Equal(entmediafile.TagsStateWritten))
				e.seedArtist("Guest", albumSeed{mbid: "rg-1", title: "Nevermind"})
				e.posters.EXPECT().
					Remove(mock.Anything, mock.Anything).
					Return(nil).
					Maybe()

				tagConfig(map[string]any{"mode": config.MusicTagsManaged})
				Expect(
					e.svc.Delete(e.ctx, alb.QueryArtist().OnlyIDX(e.ctx), false),
				).To(Succeed())
				Expect(
					*reload(mf).TagsState,
				).To(Equal(entmediafile.TagsStatePending))
			},
		)

		It(
			"queues every file when the settings change what a file carries, under managed",
			func() {
				noTorrents()
				mf := placeFile("Nirvana/Nevermind/01.flac", pending())
				Expect(e.svc.SyncTags(e.ctx)).To(Succeed())
				before := config.Get().Library.MusicTags

				after := before
				after.Mode = config.MusicTagsManaged
				e.svc.TagSettingsChanged(e.ctx, before, after)
				Expect(
					*reload(mf).TagsState,
				).To(Equal(entmediafile.TagsStatePending))

				Expect(e.svc.SyncTags(e.ctx)).To(Succeed())
				Expect(
					*reload(mf).TagsState,
				).To(Equal(entmediafile.TagsStateWritten))
				e.svc.TagSettingsChanged(e.ctx, after, after)
				Expect(
					*reload(mf).TagsState,
				).To(Equal(entmediafile.TagsStateWritten))
			},
		)
	})

	It("takes everything on a manual run and only a batch otherwise", func() {
		noTorrents()
		for range tagSyncBatch + 1 {
			e.client.MediaFile.Create().
				SetPath(filepath.Join(musicDir, "missing.flac")).SetSize(1).
				SetTrackID(tr.ID).SetTagsState(entmediafile.TagsStatePending).
				ExecX(e.ctx)
		}
		Expect(e.svc.SyncTags(e.ctx)).To(Succeed())
		Expect(e.client.MediaFile.Query().
			Where(entmediafile.TagFailuresGT(0)).
			CountX(e.ctx)).To(Equal(tagSyncBatch))

		Expect(e.svc.SyncTags(manualCtx(e.ctx))).To(Succeed())
		Expect(e.client.MediaFile.Query().
			Where(entmediafile.TagFailuresGT(0)).
			CountX(e.ctx)).To(Equal(tagSyncBatch + 1))
	})

	It("tags one album's waiting files on request, leaving the others", func() {
		noTorrents()
		mf := placeFile("Nirvana/Nevermind/01.flac", pending())
		_, others := e.seedArtist(
			"Hole",
			albumSeed{
				mbid:   "rg-2",
				title:  "Live Through This",
				tracks: []string{"Doll Parts"},
			},
		)
		otherTrack := others[0].QueryTracks().OnlyX(e.ctx)
		other := e.client.MediaFile.Create().
			SetPath(filepath.Join(musicDir, "other.flac")).SetSize(1).
			SetTrackID(otherTrack.ID).SetTagsState(entmediafile.TagsStatePending).
			SaveX(e.ctx)

		// The cover is resolved first; there is none to fetch here.
		e.posters.EXPECT().Fetch(mock.Anything, coverKind, alb.ID, mock.Anything).
			Return(errors.New("no cover")).Maybe()
		_, err := e.svc.TagAlbum(e.ctx, alb.ID)
		Expect(err).NotTo(HaveOccurred())

		Expect(*reload(mf).TagsState).To(Equal(entmediafile.TagsStateWritten))
		Expect(*reload(other).TagsState).To(Equal(entmediafile.TagsStatePending))
	})

	It("lists the download clients once for albums tagged back to back", func() {
		// A bulk-import commit tags album after album.
		e.dl.EXPECT().SeedingTorrents(mock.Anything).
			Return(download.NewSeedingSet(true, nil, nil)).Once()
		e.posters.EXPECT().
			Fetch(mock.Anything, coverKind, mock.Anything, mock.Anything).
			Return(errors.New("no cover")).
			Maybe()
		_, others := e.seedArtist("Hole", albumSeed{
			mbid: "rg-2", title: "Live Through This", tracks: []string{"Doll Parts"},
		})
		first := placeFile("Nirvana/Nevermind/01.flac", pending())
		second := e.client.MediaFile.Create().
			SetPath(filepath.Join(musicDir, "Hole", "01.flac")).SetSize(1).
			SetTrackID(others[0].QueryTracks().OnlyX(e.ctx).ID).
			SetTagsState(entmediafile.TagsStatePending).SaveX(e.ctx)

		for _, id := range []uint32{alb.ID, others[0].ID} {
			_, err := e.svc.TagAlbum(e.ctx, id)
			Expect(err).NotTo(HaveOccurred())
		}

		Expect(*reload(first).TagsState).To(Equal(entmediafile.TagsStateWritten))
		Expect(
			reload(second).TagFailures,
		).To(Equal(uint8(1)), "the second album was reached")
	})
})

// manualCtx marks ctx the way POST /schedules/{name}/run does.
func manualCtx(ctx context.Context) context.Context {
	return scheduler.WithManual(ctx)
}

func dirEntries(dir string) []string {
	GinkgoHelper()
	entries, err := os.ReadDir(dir)
	Expect(err).NotTo(HaveOccurred())
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
