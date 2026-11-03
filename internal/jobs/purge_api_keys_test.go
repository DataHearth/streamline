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

var _ = Describe("PurgeAPIKeys", Label("unit"), func() {
	var (
		ctx    context.Context
		purger *authmocks.MockAPIKeyPurger
	)

	BeforeEach(func() {
		ctx = context.Background()
		purger = authmocks.NewMockAPIKeyPurger(GinkgoT())
	})

	It("passes a cutoff that lags now by APIKeyPurgeGrace", func() {
		var captured time.Time
		purger.EXPECT().
			PurgeExpiredAPIKeys(mock.Anything, mock.AnythingOfType("time.Time")).
			Run(func(_ context.Context, before time.Time) {
				captured = before
			}).
			Return(0, nil).
			Once()

		before := time.Now()
		Expect(PurgeAPIKeys(purger)(ctx)).To(Succeed())
		after := time.Now()

		Expect(captured).To(BeTemporally(">=",
			before.Add(-APIKeyPurgeGrace).Add(-time.Second)))
		Expect(captured).To(BeTemporally("<=",
			after.Add(-APIKeyPurgeGrace).Add(time.Second)))
	})

	It("logs the count when rows were purged", func() {
		var buf bytes.Buffer
		GinkgoWriter.TeeTo(&buf)
		DeferCleanup(GinkgoWriter.ClearTeeWriters)

		purger.EXPECT().
			PurgeExpiredAPIKeys(mock.Anything, mock.AnythingOfType("time.Time")).
			Return(3, nil).
			Once()

		Expect(PurgeAPIKeys(purger)(ctx)).To(Succeed())
		Expect(buf.String()).To(ContainSubstring("purged expired API keys"))
		Expect(buf.String()).To(ContainSubstring("count=3"))
	})

	It("propagates purger error", func() {
		boom := errors.New("db down")
		purger.EXPECT().
			PurgeExpiredAPIKeys(mock.Anything, mock.AnythingOfType("time.Time")).
			Return(0, boom).
			Once()

		Expect(PurgeAPIKeys(purger)(ctx)).To(MatchError(boom))
	})
})
