package tvshow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediaevent"
	enttvshow "github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/mediaserver"
	msmocks "github.com/datahearth/streamline/internal/mediaserver/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

var _ = Describe("RenameService", Label("unit", "series"), func() {
	// Test-only naming template (kept tight & deterministic).
	const naming = "{title}/Season {season}/{title} - S{season:2}E{episode:2}.{ext}"

	var (
		ctx     context.Context
		store   *dbmocks.MockStore
		storeMk *dbmocks.MockStore_Expecter
		svc     *RenameService
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		storeMk = store.EXPECT()
		svc = newTestRenameService(store, nil, "/library/tv", naming)
	})

	It("maps NotFound to ErrSeriesNotFound on preview", func() {
		storeMk.FindTVShowByID(mock.Anything, uint32(99)).
			Return(nil, &ent.NotFoundError{}).Once()

		_, err := svc.Preview(ctx, 99)
		Expect(err).To(MatchError(ErrSeriesNotFound))
		Expect(err).To(MatchError(ContainSubstring("series 99")))
	})

	It("maps NotFound to ErrSeriesNotFound on apply", func() {
		storeMk.FindTVShowByID(mock.Anything, uint32(99)).
			Return(nil, &ent.NotFoundError{}).Once()

		_, err := svc.Apply(ctx, 99)
		Expect(err).To(MatchError(ErrSeriesNotFound))
		Expect(err).To(MatchError(ContainSubstring("series 99")))
	})

	It("wraps generic lookup errors without the not-found sentinel", func() {
		storeErr := errors.New("db down")
		storeMk.FindTVShowByID(mock.Anything, uint32(1)).
			Return(nil, storeErr).Once()

		_, err := svc.Preview(ctx, 1)
		Expect(err).To(MatchError(storeErr))
		Expect(err).To(MatchError(ContainSubstring("find series")))
		Expect(err).NotTo(MatchError(ErrSeriesNotFound))
	})

	It("returns an empty plan when the show has no episode files", func() {
		storeMk.FindTVShowByID(mock.Anything, uint32(1)).
			Return(&ent.TVShow{ID: 1, Title: "The Black Sea"}, nil).Once()

		plan, err := svc.Preview(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(BeEmpty())
	})

	It("skips a missing file and still renames the others", func() {
		since := time.Now()
		show := &ent.TVShow{ID: 1, Title: "The Wire", Year: 2002, TvdbID: 1}
		show.Edges.Seasons = []*ent.Season{
			{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
				{ID: 100, Number: 1, Edges: ent.EpisodeEdges{
					MediaFiles: []*ent.MediaFile{
						{ID: 10, Path: "/old/gone.mkv", MissingSince: &since},
					},
				}},
				{ID: 101, Number: 2, Edges: ent.EpisodeEdges{
					MediaFiles: []*ent.MediaFile{{ID: 11, Path: "/old/here.mkv"}},
				}},
			}}},
		}
		storeMk.FindTVShowByID(mock.Anything, uint32(1)).Return(show, nil).Once()

		plan, err := svc.Preview(ctx, 1)

		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(HaveLen(1))
		Expect(plan.Operations[0].MediaFileID).To(Equal(uint32(11)))
	})

	It("plans against the root and template the config holds now", func() {
		show := &ent.TVShow{ID: 1, Title: "The Wire", Year: 2002, TvdbID: 1}
		show.Edges.Seasons = []*ent.Season{
			{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
				{ID: 100, Number: 2, Edges: ent.EpisodeEdges{
					MediaFiles: []*ent.MediaFile{{
						ID:   11,
						Path: "/library/tv/The Wire/Season 1/The Wire - S01E02.mkv",
					}},
				}},
			}}},
		}
		storeMk.FindTVShowByID(mock.Anything, uint32(1)).Return(show, nil).Times(3)

		plan, err := svc.Preview(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(BeEmpty())

		// What a path migration does to the root, with no restart.
		moved := "/mnt/new/tv"
		_, err = config.UpdateLibrary(ctx, config.LibraryPatch{SeriesPath: &moved})
		Expect(err).NotTo(HaveOccurred())

		plan, err = svc.Preview(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(HaveLen(1))
		Expect(plan.Operations[0].To).To(
			Equal("/mnt/new/tv/The Wire/Season 1/The Wire - S01E02.mkv"),
		)

		// And what Settings → Library does to the template.
		flat := "{title}/{title} {season}x{episode:2}.{ext}"
		_, err = config.UpdateLibrary(ctx, config.LibraryPatch{SeriesNaming: &flat})
		Expect(err).NotTo(HaveOccurred())

		plan, err = svc.Preview(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(HaveLen(1))
		Expect(plan.Operations[0].To).To(
			Equal("/mnt/new/tv/The Wire/The Wire 1x02.mkv"),
		)
	})

	Describe("Apply's event recording", func() {
		var (
			client *ent.Client
			msMk   *msmocks.MockRefresher_Expecter
			tmp    string
			showID uint32
		)

		BeforeEach(func() {
			client = dbtest.SetupTestDB(ctx)
			DeferCleanup(client.Close)
			events.Register(client)

			tmp = GinkgoT().TempDir()
			ms := msmocks.NewMockRefresher(GinkgoT())
			msMk = ms.EXPECT()
			svc = newTestRenameService(store, ms, tmp, naming)

			row := client.TVShow.Create().
				SetTitle("The Wire").
				SetOriginalTitle("The Wire").
				SetYear(2002).
				SetTvdbID(1).
				SaveX(ctx)
			showID = row.ID
		})

		It(
			"records one series-scoped file_renamed event across every season touched",
			func() {
				src1 := filepath.Join(tmp, "ep1.mkv")
				src2 := filepath.Join(tmp, "ep2.mkv")
				Expect(os.WriteFile(src1, []byte("x"), 0o644)).To(Succeed())
				Expect(os.WriteFile(src2, []byte("x"), 0o644)).To(Succeed())

				show := &ent.TVShow{
					ID:     showID,
					Title:  "The Wire",
					Year:   2002,
					TvdbID: 1,
				}
				show.Edges.Seasons = []*ent.Season{
					{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
						{ID: 100, Number: 1, Edges: ent.EpisodeEdges{
							MediaFiles: []*ent.MediaFile{{ID: 10, Path: src1}},
						}},
					}}},
					{Number: 2, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
						{ID: 200, Number: 1, Edges: ent.EpisodeEdges{
							MediaFiles: []*ent.MediaFile{{ID: 20, Path: src2}},
						}},
					}}},
				}
				storeMk.FindTVShowByID(mock.Anything, showID).
					Return(show, nil).
					Once()
				storeMk.UpdateMediaFilePath(
					mock.Anything, uint32(10), mock.AnythingOfType("string"),
				).Return(nil).Once()
				storeMk.UpdateMediaFilePath(
					mock.Anything, uint32(20), mock.AnythingOfType("string"),
				).Return(nil).Once()
				refreshed := make(chan struct{})
				msMk.RefreshAll(mock.Anything, "series", tmp, mock.Anything).
					Run(func(context.Context, string, string, ...string) { close(refreshed) }).
					Return(nil).Once()

				plan, err := svc.Apply(ctx, showID)
				Expect(err).NotTo(HaveOccurred())
				Expect(plan.Operations).To(HaveLen(2))
				Eventually(refreshed).Should(BeClosed())

				evs := client.MediaEvent.Query().
					Where(
						mediaevent.TypeEQ(mediaevent.Type(events.TypeFileRenamed)),
						mediaevent.HasTvShowWith(enttvshow.IDEQ(showID)),
					).
					AllX(ctx)
				Expect(evs).To(HaveLen(1))
				Expect(evs[0].Payload).To(HaveKeyWithValue("episodes", float64(2)))
				Expect(
					evs[0].Payload["seasons"],
				).To(ConsistOf(float64(1), float64(2)))
			},
		)

		It("applies under a root changed after the service was built", func() {
			newRoot := filepath.Join(GinkgoT().TempDir(), "tv")
			_, err := config.UpdateLibrary(
				ctx, config.LibraryPatch{SeriesPath: &newRoot},
			)
			Expect(err).NotTo(HaveOccurred())

			// A misnamed episode in its own folder under the new root: the
			// rename empties that folder, so the prune has to run under the
			// new root to remove it.
			oldDir := filepath.Join(newRoot, "the.wire.s01")
			Expect(os.MkdirAll(oldDir, 0o755)).To(Succeed())
			src := filepath.Join(oldDir, "ep2.mkv")
			Expect(os.WriteFile(src, []byte("x"), 0o644)).To(Succeed())
			want := filepath.Join(
				newRoot, "The Wire", "Season 1", "The Wire - S01E02.mkv",
			)

			show := &ent.TVShow{ID: showID, Title: "The Wire", Year: 2002, TvdbID: 1}
			show.Edges.Seasons = []*ent.Season{
				{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
					{ID: 100, Number: 2, Edges: ent.EpisodeEdges{
						MediaFiles: []*ent.MediaFile{{ID: 11, Path: src}},
					}},
				}}},
			}
			storeMk.FindTVShowByID(mock.Anything, showID).Return(show, nil).Once()
			storeMk.UpdateMediaFilePath(mock.Anything, uint32(11), want).
				Return(nil).Once()
			refreshed := make(chan struct{})
			msMk.RefreshAll(mock.Anything, "series", newRoot, mock.Anything).
				Run(func(context.Context, string, string, ...string) { close(refreshed) }).
				Return(nil).Once()

			plan, err := svc.Apply(ctx, showID)
			Expect(err).NotTo(HaveOccurred())
			Expect(plan.Operations).To(HaveLen(1))
			Expect(want).To(BeARegularFile())
			Expect(oldDir).NotTo(BeADirectory())
			Expect(newRoot).To(BeADirectory())
			Expect(filepath.Join(tmp, "The Wire")).NotTo(BeADirectory())
			Eventually(refreshed).Should(BeClosed())
		})

		It("records nothing when the plan has no operations", func() {
			storeMk.FindTVShowByID(mock.Anything, showID).
				Return(&ent.TVShow{ID: showID, Title: "The Wire"}, nil).Once()

			plan, err := svc.Apply(ctx, showID)
			Expect(err).NotTo(HaveOccurred())
			Expect(plan.Operations).To(BeEmpty())

			evs := client.MediaEvent.Query().
				Where(mediaevent.TypeEQ(mediaevent.Type(events.TypeFileRenamed))).
				AllX(ctx)
			Expect(evs).To(BeEmpty())
		})
	})
})

// newTestRenameService loads a config whose series root and naming template
// are the given ones, backed by a file so a spec can change them through
// config.Update, and returns a service over it.
func newTestRenameService(
	store db.Store, ms mediaserver.Refresher, root, naming string,
) *RenameService {
	GinkgoHelper()
	configtest.SetupFile(map[string]any{
		"library": map[string]any{"series_path": root, "series_naming": naming},
	})
	return NewRenameService(store, ms)
}
