package jobs

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/download"
	dlmocks "github.com/datahearth/streamline/internal/download/mocks"
	impmocks "github.com/datahearth/streamline/internal/importer/mocks"
)

var _ = Describe("DownloadMonitor", Label("unit"), func() {
	var (
		ctx     context.Context
		checker *dlmocks.MockChecker
		adopter *dlmocks.MockAdopter
		enq     *impmocks.MockEnqueuer
	)

	BeforeEach(func() {
		ctx = context.Background()
		checker = dlmocks.NewMockChecker(GinkgoT())
		adopter = dlmocks.NewMockAdopter(GinkgoT())
		enq = impmocks.NewMockEnqueuer(GinkgoT())
	})

	It("enqueues each completed download then runs adoption", func() {
		checker.EXPECT().
			CheckStatus(mock.Anything).
			Return([]download.CompletedDownload{
				{Record: &ent.DownloadRecord{ID: 11}},
				{Record: &ent.DownloadRecord{ID: 22}},
				{Record: &ent.DownloadRecord{ID: 33}},
			}, nil).
			Once()
		checker.EXPECT().ReconcileEpisodeStatuses(mock.Anything).Return(nil).Once()
		enq.EXPECT().Enqueue(uint32(11)).Once()
		enq.EXPECT().Enqueue(uint32(22)).Once()
		enq.EXPECT().Enqueue(uint32(33)).Once()
		adopter.EXPECT().AdoptManualTorrents(mock.Anything).Return(nil, nil).Once()

		Expect(DownloadMonitor(checker, adopter, enq)(ctx)).To(Succeed())
	})

	It("enqueues adopted record ids after the completion pass", func() {
		checker.EXPECT().CheckStatus(mock.Anything).Return(nil, nil).Once()
		checker.EXPECT().ReconcileEpisodeStatuses(mock.Anything).Return(nil).Once()
		adopter.EXPECT().AdoptManualTorrents(mock.Anything).
			Return([]uint32{42}, nil).Once()
		enq.EXPECT().Enqueue(uint32(42)).Once()

		Expect(DownloadMonitor(checker, adopter, enq)(ctx)).To(Succeed())
	})

	It("returns nil and enqueues nothing on an empty result", func() {
		checker.EXPECT().CheckStatus(mock.Anything).Return(nil, nil).Once()
		checker.EXPECT().ReconcileEpisodeStatuses(mock.Anything).Return(nil).Once()
		adopter.EXPECT().AdoptManualTorrents(mock.Anything).Return(nil, nil).Once()

		Expect(DownloadMonitor(checker, adopter, enq)(ctx)).To(Succeed())
	})

	It("swallows a reconcile failure (completion pass must not die)", func() {
		checker.EXPECT().CheckStatus(mock.Anything).Return(nil, nil).Once()
		checker.EXPECT().ReconcileEpisodeStatuses(mock.Anything).
			Return(errors.New("reconcile boom")).Once()
		adopter.EXPECT().AdoptManualTorrents(mock.Anything).Return(nil, nil).Once()

		Expect(DownloadMonitor(checker, adopter, enq)(ctx)).To(Succeed())
	})

	It("swallows an adoption failure (completion pass must not die)", func() {
		checker.EXPECT().CheckStatus(mock.Anything).Return(nil, nil).Once()
		checker.EXPECT().ReconcileEpisodeStatuses(mock.Anything).Return(nil).Once()
		adopter.EXPECT().AdoptManualTorrents(mock.Anything).
			Return(nil, errors.New("adopt boom")).Once()

		Expect(DownloadMonitor(checker, adopter, enq)(ctx)).To(Succeed())
	})

	It("propagates checker error and skips adoption + enqueue", func() {
		boom := errors.New("poll failed")
		checker.EXPECT().CheckStatus(mock.Anything).Return(nil, boom).Once()

		err := DownloadMonitor(checker, adopter, enq)(ctx)
		Expect(err).To(MatchError(boom))
	})
})
