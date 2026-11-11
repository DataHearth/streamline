package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/scheduledjob"
	"github.com/datahearth/streamline/internal/scheduler"
	schedulermocks "github.com/datahearth/streamline/internal/scheduler/mocks"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe("libraryScanner", Label("unit"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		jobs   *schedulermocks.MockController
		scan   libraryScanner

		albumID uint32
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = dbtest.SetupTestDB(ctx)
		DeferCleanup(client.Close)
		jobs = schedulermocks.NewMockController(GinkgoT())
		scan = libraryScanner{client: client, jobs: jobs}
		artist := client.Artist.Create().SetMbid("a").SetName("A").SaveX(ctx)
		albumID = client.Album.Create().SetMbid("b").SetTitle("B").
			SetArtist(artist).SaveX(ctx).ID
	})

	addTrack := func(n int, missing bool) {
		GinkgoHelper()
		t := client.Track.Create().SetTitle("T").SetPosition(uint16(n)).
			SetAlbumID(albumID).SaveX(ctx)
		f := client.MediaFile.Create().SetPath(fmt.Sprintf("/m/%d.flac", n)).
			SetSize(1).SetTrack(t)
		if missing {
			f.SetMissingSince(time.Now())
		}
		f.SaveX(ctx)
	}

	It("counts the tracks holding a file that is on disk", func() {
		addTrack(1, false)
		addTrack(2, false)
		addTrack(3, true)
		jobs.EXPECT().Get("drift-check").
			Return(scheduler.JobInfo{Running: true}, nil).Once()

		st, err := scan.ScanStatus(ctx)

		Expect(err).NotTo(HaveOccurred())
		Expect(st.Count).To(Equal(int64(2)))
		Expect(st.Scanning).To(BeTrue())
		Expect(st.LastScan).To(BeNil())
	})

	It("reports when the job last finished", func() {
		finished := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
		client.ScheduledJob.Create().SetName("drift-check").
			SetLastFinishedAt(finished).
			SetLastStatus(scheduledjob.LastStatusSuccess).SaveX(ctx)
		jobs.EXPECT().Get("drift-check").Return(scheduler.JobInfo{}, nil).Once()

		st, err := scan.ScanStatus(ctx)

		Expect(err).NotTo(HaveOccurred())
		Expect(st.LastScan).To(HaveValue(BeTemporally("==", finished)))
	})

	It("starts the drift-check job", func() {
		jobs.EXPECT().RunNow("drift-check").Return(nil).Once()
		Expect(scan.StartScan(ctx)).To(Succeed())
	})

	It("treats a scan already running as started", func() {
		jobs.EXPECT().RunNow("drift-check").Return(scheduler.ErrJobBusy).Once()
		Expect(scan.StartScan(ctx)).To(Succeed())
	})

	It("reports a job that cannot be started", func() {
		jobs.EXPECT().RunNow("drift-check").Return(scheduler.ErrNotStarted).Once()
		err := scan.StartScan(ctx)
		Expect(errors.Is(err, scheduler.ErrNotStarted)).To(BeTrue())
	})
})
