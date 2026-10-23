package jobs

import (
	"bytes"
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	authmocks "github.com/datahearth/streamline/internal/auth/mocks"
)

var _ = Describe("PurgeSessions", Label("unit"), func() {
	var (
		ctx    context.Context
		purger *authmocks.MockSessionPurger
	)

	BeforeEach(func() {
		ctx = context.Background()
		purger = authmocks.NewMockSessionPurger(GinkgoT())
	})

	It("passes a cutoff that lags now by SessionPurgeGrace", func() {
		var captured time.Time
		purger.EXPECT().
			PurgeExpiredSessions(mock.Anything, mock.AnythingOfType("time.Time")).
			Run(func(_ context.Context, before time.Time) {
				captured = before
			}).
			Return(0, nil).
			Once()

		before := time.Now()
		Expect(PurgeSessions(purger)(ctx)).To(Succeed())
		after := time.Now()

		Expect(
			captured,
		).To(BeTemporally(">=", before.Add(-SessionPurgeGrace).Add(-time.Second)))
		Expect(
			captured,
		).To(BeTemporally("<=", after.Add(-SessionPurgeGrace).Add(time.Second)))
	})

	It("logs the count when rows were purged", func() {
		var buf bytes.Buffer
		GinkgoWriter.TeeTo(&buf)
		DeferCleanup(GinkgoWriter.ClearTeeWriters)

		purger.EXPECT().
			PurgeExpiredSessions(mock.Anything, mock.AnythingOfType("time.Time")).
			Return(7, nil).
			Once()

		Expect(PurgeSessions(purger)(ctx)).To(Succeed())
		Expect(buf.String()).To(ContainSubstring("purged expired sessions"))
		Expect(buf.String()).To(ContainSubstring("count=7"))
	})

	It("skips logging when zero rows purged", func() {
		var buf bytes.Buffer
		GinkgoWriter.TeeTo(&buf)
		DeferCleanup(GinkgoWriter.ClearTeeWriters)

		purger.EXPECT().
			PurgeExpiredSessions(mock.Anything, mock.AnythingOfType("time.Time")).
			Return(0, nil).
			Once()

		Expect(PurgeSessions(purger)(ctx)).To(Succeed())
		Expect(buf.String()).NotTo(ContainSubstring("purged expired sessions"))
	})

	It("propagates purger error", func() {
		boom := errors.New("db down")
		purger.EXPECT().
			PurgeExpiredSessions(mock.Anything, mock.AnythingOfType("time.Time")).
			Return(0, boom).
			Once()

		err := PurgeSessions(purger)(ctx)
		Expect(err).To(MatchError(boom))
	})
})
