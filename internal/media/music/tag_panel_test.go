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
	"github.com/datahearth/streamline/internal/library/audiotags"
)

var _ = Describe(
	"Music tag panel and edit check",
	Label("unit", "integration", "music"),
	func() {
		var (
			e        *env
			musicDir string
			alb      *ent.Album
			tr       *ent.Track
		)

		tagConfig := func(mode string) {
			GinkgoHelper()
			e.setConfig(map[string]any{"library": map[string]any{
				"music_path":        musicDir,
				"download_path":     GinkgoT().TempDir(),
				"no_match_cooldown": "6h",
				"max_grab_failures": 3,
				"music_tags": map[string]any{
					"mode":        mode,
					"embed_art":   false,
					"sidecar_art": false,
				},
			}})
		}

		placeFile := func(state *entmediafile.TagsState) *ent.MediaFile {
			GinkgoHelper()
			data, err := os.ReadFile("../../library/audiotags/testdata/tagged.flac")
			Expect(err).NotTo(HaveOccurred())
			path := filepath.Join(musicDir, "Nirvana", "Nevermind", "01.flac")
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
		reload := func(mf *ent.MediaFile) *ent.MediaFile { return e.client.MediaFile.GetX(e.ctx, mf.ID) }
		tagsOf := func(mf *ent.MediaFile) map[string][]string {
			GinkgoHelper()
			t, err := audiotags.ReadTags(mf.Path)
			Expect(err).NotTo(HaveOccurred())
			return t
		}
		sync := func() {
			GinkgoHelper()
			Expect(e.svc.SyncTags(manualCtx(e.ctx))).To(Succeed())
		}

		BeforeEach(func() {
			e = newEnv(false)
			stubKindPaths(e.posters, artistKind)
			e.dl.EXPECT().SeedingTorrents(mock.Anything).
				Return(download.NewSeedingSet(true, nil, nil)).Maybe()
			musicDir = GinkgoT().TempDir()
			tagConfig(config.MusicTagsImport)
			_, albums := e.seedArtist(
				"Nirvana",
				albumSeed{
					mbid:   "rg-1",
					title:  "Nevermind",
					tracks: []string{"Lithium"},
				},
			)
			alb = albums[0]
			tr = alb.QueryTracks().OnlyX(e.ctx)
		})

		It("writes the album's overrides and the track's over them", func() {
			mf := placeFile(pending())
			Expect(e.svc.SetAlbumTagSettings(e.ctx, alb.ID, AlbumTagSettings{
				Overrides: &map[string][]string{
					"GENRE": {"Grunge"},
					"MOOD":  {"Loud"},
				},
			})).To(Succeed())
			Expect(e.svc.SetTrackTagOverrides(e.ctx, tr.ID, map[string][]string{
				"MOOD": {"Quiet"}, "TITLE": {"Lithium (Live)"},
			})).To(Succeed())

			sync()

			got := tagsOf(mf)
			Expect(got).To(HaveKeyWithValue("GENRE", []string{"Grunge"}))
			Expect(got).To(HaveKeyWithValue("MOOD", []string{"Quiet"}))
			Expect(got).To(HaveKeyWithValue("TITLE", []string{"Lithium (Live)"}))
		})

		It("compares each file with what a write would leave in it", func() {
			placeFile(nil)
			Expect(
				e.svc.SetTrackTagOverrides(
					e.ctx,
					tr.ID,
					map[string][]string{"MOOD": {"Calm"}},
				),
			).
				To(Succeed())

			v, err := e.svc.AlbumTags(e.ctx, alb.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(v.Mode).To(Equal(config.MusicTagsImport))
			Expect(v.Files).To(HaveLen(1))
			f := v.Files[0]
			Expect(f.ReadError).To(BeEmpty())
			byKey := map[string]audiotags.KeyState{}
			for _, k := range f.Keys {
				byKey[k.Key] = k
			}
			Expect(
				byKey["TITLE"].Current,
			).To(Equal([]string{"Smells Like Teen Spirit"}))
			Expect(byKey["TITLE"].Desired).To(Equal([]string{"Lithium"}))
			Expect(byKey["TITLE"].Status).To(Equal(audiotags.KeyDiffers))
			Expect(byKey["MOOD"].Desired).To(Equal([]string{"Calm"}))
			Expect(byKey["ENCODER"].Status).To(Equal(audiotags.KeyKept))

			_, err = e.svc.AlbumTags(e.ctx, 9999)
			Expect(err).To(MatchError(ErrAlbumNotFound))
		})

		It(
			"writes an unmanaged album's files on request, and refuses with tagging off",
			func() {
				mf := placeFile(nil)
				e.client.Album.UpdateOneID(alb.ID).SetTagsManaged(false).ExecX(e.ctx)

				n, err := e.svc.ApplyAlbumTags(e.ctx, alb.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(n).To(Equal(1))
				Eventually(
					func() *entmediafile.TagsState { return reload(mf).TagsState },
				).
					Should(HaveValue(Equal(entmediafile.TagsStateWritten)))
				Expect(tagsOf(mf)).To(HaveKeyWithValue("TITLE", []string{"Lithium"}))

				tagConfig(config.MusicTagsOff)
				_, err = e.svc.ApplyAlbumTags(e.ctx, alb.ID)
				Expect(err).To(MatchError(ErrTagsOff))
			},
		)

		It("queues the album when it is taken back under management", func() {
			mf := placeFile(nil)
			e.client.Album.UpdateOneID(alb.ID).SetTagsManaged(false).ExecX(e.ctx)

			managed := true
			Expect(
				e.svc.SetAlbumTagSettings(
					e.ctx,
					alb.ID,
					AlbumTagSettings{Managed: &managed},
				),
			).
				To(Succeed())

			Expect(e.client.Album.GetX(e.ctx, alb.ID).TagsManaged).To(BeTrue())
			Expect(*reload(mf).TagsState).To(Equal(entmediafile.TagsStatePending))
		})

		Context("under managed", func() {
			var mf *ent.MediaFile

			BeforeEach(func() {
				tagConfig(config.MusicTagsManaged)
				mf = placeFile(pending())
				sync()
				Expect(
					*reload(mf).TagsState,
				).To(Equal(entmediafile.TagsStateWritten))
			})

			edit := func(tags map[string][]string) {
				GinkgoHelper()
				Expect(
					audiotags.Write(mf.Path, tags, audiotags.Policy{}),
				).To(Succeed())
				// A distinct modification time, whatever the filesystem's
				// resolution.
				later := time.Now().Add(time.Minute)
				Expect(os.Chtimes(mf.Path, later, later)).To(Succeed())
			}

			It("undoes an edit made elsewhere to a tag it owns", func() {
				edit(map[string][]string{"TITLE": {"Lithium (my edit)"}})

				sync()

				Expect(tagsOf(mf)).To(HaveKeyWithValue("TITLE", []string{"Lithium"}))
				Expect(
					*reload(mf).TagsState,
				).To(Equal(entmediafile.TagsStateWritten))
			})

			It(
				"leaves an edit to a tag it does not own, and records the file as seen",
				func() {
					edit(map[string][]string{"REPLAYGAIN_TRACK_GAIN": {"-7.20 dB"}})

					sync()

					Expect(
						tagsOf(mf),
					).To(HaveKeyWithValue("REPLAYGAIN_TRACK_GAIN", []string{"-7.20 dB"}))
					got := reload(mf)
					st, err := os.Stat(mf.Path)
					Expect(err).NotTo(HaveOccurred())
					Expect(got.TagsMtime).To(Equal(st.ModTime().UnixNano()))
					Expect(got.TagsCheckedAt).NotTo(BeNil())
				},
			)

			It(
				"gives a file it cannot compare its turn, so the rotation moves on",
				func() {
					edit(map[string][]string{"TITLE": {"Lithium (my edit)"}})
					before := reload(mf)
					turn := before.TagsCheckedAt.Add(time.Hour)
					timeNow = func() time.Time { return turn }
					DeferCleanup(func() { timeNow = time.Now })
					e.svc.db = albumless{e.store}

					Expect(e.svc.checkEdits(e.ctx,
						config.Get().Library.MusicTags, 1)).To(BeZero())

					got := reload(mf)
					Expect(*got.TagsCheckedAt).To(BeTemporally("==", turn))
					Expect(got.TagsMtime).To(Equal(before.TagsMtime),
						"the change is still seen on its next turn")
				},
			)

			It("does not take a change it could not read for seen", func() {
				before := reload(mf)
				Expect(
					os.WriteFile(mf.Path, []byte("mid-write"), 0o644),
				).To(Succeed())

				Expect(e.svc.checkEdits(e.ctx,
					config.Get().Library.MusicTags, 1)).To(BeZero())

				got := reload(mf)
				Expect(got.TagsMtime).To(Equal(before.TagsMtime))
				Expect(got.TagsSize).To(Equal(before.TagsSize),
					"the next turn compares the file again")
			})

			It("checks nothing of an album it does not manage", func() {
				e.client.Album.UpdateOneID(alb.ID).SetTagsManaged(false).ExecX(e.ctx)
				edit(map[string][]string{"TITLE": {"Lithium (my edit)"}})

				sync()

				Expect(
					tagsOf(mf),
				).To(HaveKeyWithValue("TITLE", []string{"Lithium (my edit)"}))
			})
		})
	},
)

// albumless is a store whose albums cannot be read: an album deleted under the
// edit check, or a database error.
type albumless struct{ db.Store }

func (albumless) FindAlbumByID(context.Context, uint32) (*ent.Album, error) {
	return nil, errors.New("album gone")
}
