package db

import (
	"context"
	"time"

	"github.com/datahearth/streamline/internal/providerid"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/mediaevent"
	"github.com/datahearth/streamline/ent/request"
	"github.com/datahearth/streamline/ent/user"
)

var _ = Describe("GetStats", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
		now    time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
		now = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	})

	event := func(m *ent.Movie, t mediaevent.Type, ago time.Duration) {
		GinkgoHelper()
		client.MediaEvent.Create().
			SetMovie(m).
			SetType(t).
			SetCreateTime(now.Add(-ago)).
			ExecX(ctx)
	}

	It("buckets the range by day and keeps the stretch before it apart", func() {
		m := client.Movie.Create().
			SetTmdbID("1").
			SetTitle("Heat").
			SetOriginalTitle("Heat").
			SetYear(1995).
			SaveX(ctx)
		event(m, mediaevent.TypeGrabbed, time.Hour)
		event(m, mediaevent.TypeImported, time.Hour)
		event(m, mediaevent.TypeImportFailed, 2*24*time.Hour)
		event(m, mediaevent.TypeGrabbed, 10*24*time.Hour)
		event(m, mediaevent.TypeGrabbed, 40*24*time.Hour)
		for _, source := range []string{"wizard", "orphan", "bulk_import"} {
			client.MediaEvent.Create().
				SetMovie(m).
				SetType(mediaevent.TypeImported).
				SetCreateTime(now.Add(-time.Hour)).
				SetPayload(map[string]any{"source": source}).
				ExecX(ctx)
		}

		st, err := store.GetStats(ctx, 7, now)
		Expect(err).NotTo(HaveOccurred())

		Expect(st.Pipeline.Daily).To(HaveLen(7))
		Expect(st.Pipeline.Daily[0].Day).To(Equal("2026-10-31"))
		today := st.Pipeline.Daily[6]
		Expect(today.Day).To(Equal("2026-11-06"))
		Expect(today.Grabbed).To(BeEquivalentTo(1))
		Expect(today.Imported).To(BeEquivalentTo(1))
		Expect(st.Pipeline.Daily[4].Failed).To(BeEquivalentTo(1))
		Expect(st.Pipeline.Grabbed).To(BeEquivalentTo(1))
		Expect(st.Previous.Grabbed).To(BeEquivalentTo(1))
	})

	It("breaks probed video files down and counts the unprobed apart", func() {
		m := client.Movie.Create().
			SetTmdbID("1").
			SetTitle("Heat").
			SetOriginalTitle("Heat").
			SetYear(1995).
			SaveX(ctx)
		client.MediaFile.Create().
			SetMovie(m).
			SetPath("/m/Heat (1995) [2160p].mkv").
			SetSize(100).
			SetWidth(3840).
			SetHeight(1608).
			SetVideoCodec("hevc").
			SetAudioCodec("truehd").
			SetDynamicRange("dv").
			SetProbedAt(now).
			ExecX(ctx)
		client.MediaFile.Create().
			SetMovie(m).
			SetPath("/m/Heat (1995) [1080p].mkv").
			SetSize(10).
			SetWidth(1920).
			SetHeight(800).
			SetVideoCodec("mpeg2video").
			SetAudioCodec("mp3").
			SetProbedAt(now).
			ExecX(ctx)
		client.MediaFile.Create().
			SetMovie(m).
			SetPath("/m/Heat.unprobed.mkv").
			SetSize(1).
			ExecX(ctx)

		st, err := store.GetStats(ctx, 7, now)
		Expect(err).NotTo(HaveOccurred())

		Expect(st.Library.Resolution).To(ConsistOf(
			StatsSlice{"2160p", 1, 100}, StatsSlice{"1080p", 1, 10},
		))
		Expect(st.Library.VideoCodec).To(ConsistOf(
			StatsSlice{"hevc", 1, 100}, StatsSlice{"other", 1, 10},
		))
		Expect(st.Library.HDR).To(ConsistOf(
			StatsSlice{"dv", 1, 100}, StatsSlice{"unknown", 1, 10},
		))
		Expect(st.Library.Audio).To(ConsistOf(
			StatsSlice{"truehd", 1, 100}, StatsSlice{"other", 1, 10},
		))
		Expect(st.Library.Unprobed).To(BeEquivalentTo(1))
		Expect(st.Library.Media[0].Files).To(BeEquivalentTo(3))
		Expect(st.Library.Media[0].Bytes).To(BeEquivalentTo(111))
		Expect(st.Library.Largest[0].Title).To(Equal("Heat"))
		Expect(st.Library.Largest[0].Href).To(HavePrefix("/movies/"))
		Expect(st.Library.Largest[0].Quality).To(Equal("2160p"))
	})

	It("files failures under codes and tallies indexers and wait times", func() {
		rec := func(status downloadrecord.Status, reason string, imported *time.Time) {
			GinkgoHelper()
			client.DownloadRecord.Create().
				SetTitle("r").
				SetStatus(status).
				SetFailureReason(reason).
				SetIndexerName("idx").
				SetCreateTime(now.Add(-2 * time.Hour)).
				SetUpdateTime(now.Add(-time.Hour)).
				SetNillableImportedAt(imported).
				ExecX(ctx)
		}
		at := now.Add(-time.Hour)
		rec(downloadrecord.StatusCompleted, "", &at)
		rec(downloadrecord.StatusFailed, "download stalled past the limit", nil)
		rec(downloadrecord.StatusFailed, "something nobody planned for", nil)
		rec(downloadrecord.StatusHeld, "", nil)

		st, err := store.GetStats(ctx, 7, now)
		Expect(err).NotTo(HaveOccurred())

		reasons := map[string]uint32{}
		for _, f := range st.Pipeline.Failures {
			reasons[f.Reason] = f.Count
		}
		Expect(reasons).To(Equal(map[string]uint32{"stalled": 1, "other": 1}))
		Expect(st.Pipeline.Indexers).To(Equal([]StatsIndexer{
			{Name: "idx", Grabs: 4, Imported: 1, Failed: 2},
		}))
		Expect(st.Pipeline.GrabToImportP50).To(BeEquivalentTo(3600))
		Expect(st.Pipeline.Held).To(BeEquivalentTo(1))
	})

	It("names every account with its requests and when it was last seen", func() {
		ann := client.User.Create().
			SetEmail("ann@example.test").
			SetDisplayName("Ann").
			SetRole(user.RoleMember).
			SaveX(ctx)
		client.User.Create().
			SetEmail("bob@example.test").
			SetRole(user.RoleAdmin).
			ExecX(ctx)
		req := func(st request.Status, created time.Time) {
			GinkgoHelper()
			client.Request.Create().
				SetRequester(ann).
				SetMediaType(request.MediaTypeMovie).
				SetMediaID(providerid.Format(uint32(created.Unix()))).
				SetTitle("t").
				SetStatus(st).
				SetCreateTime(created).
				SetUpdateTime(created.Add(time.Hour)).
				ExecX(ctx)
		}
		req(request.StatusAvailable, now.Add(-5*time.Hour))
		req(request.StatusDenied, now.Add(-4*time.Hour))
		req(request.StatusPending, now.Add(-90*24*time.Hour))
		client.Session.Create().
			SetUser(ann).
			SetJti("j").
			SetExpiresAt(now.Add(time.Hour)).
			SetCreateTime(now.Add(-time.Hour)).
			ExecX(ctx)

		st, err := store.GetStats(ctx, 7, now)
		Expect(err).NotTo(HaveOccurred())

		Expect(st.Users.Total).To(BeEquivalentTo(2))
		Expect(st.Users.Active).To(BeEquivalentTo(1))
		Expect(
			st.Users.Pending,
		).To(BeEquivalentTo(1), "pending counts whenever it was asked")
		Expect(st.Users.RequestsTotal).To(BeEquivalentTo(2))
		Expect(
			st.Users.Approved,
		).To(BeEquivalentTo(1), "available counts as approved")
		Expect(st.Users.DecideP50).To(BeEquivalentTo(3600))
		Expect(st.Users.AvailableP50).To(BeEquivalentTo(3600))
		first := st.Users.People[0]
		Expect(first.Name).To(Equal("Ann"))
		Expect(first.Requests).To(BeEquivalentTo(2))
		Expect(first.LastSeenAt).NotTo(BeNil())
		Expect(st.Users.People[1].Name).To(Equal("bob@example.test"))
		Expect(st.Users.People[1].LastSeenAt).To(BeNil())
	})

	DescribeTable(
		"failureCode",
		func(reason, code string) { Expect(failureCode(reason)).To(Equal(code)) },
		Entry("space", "write failed: no space left on device", "no_space"),
		Entry("stalled", "torrent stalled", "stalled"),
		Entry(
			"verification",
			"failed verification against the release claim",
			"verification",
		),
		Entry("unpack", "could not extract archive", "unpack"),
		Entry("tracker", "client refused the torrent", "tracker"),
		Entry(
			"no match",
			"no files in this release matched wanted episodes S01E01",
			"no_match",
		),
		Entry("anything else", "boom", "other"),
	)

	It("takes the nearest rank", func() {
		Expect(percentileSecs(nil, 50)).To(BeZero())
		Expect(percentileSecs([]int64{9, 1, 5}, 50)).To(BeEquivalentTo(5))
		Expect(percentileSecs([]int64{9, 1, 5}, 90)).To(BeEquivalentTo(5))
	})
})
