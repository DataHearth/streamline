package jobs

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	dlmocks "github.com/datahearth/streamline/internal/download/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Cleanup", Label("unit"), func() {
	BeforeEach(func() {
		configtest.Setup(map[string]any{
			"events": map[string]any{"retention": "2160h"},
		})
	})

	It("runs old-records then orphan purge then events purge", func() {
		c := dlmocks.NewMockCleaner(GinkgoT())
		c.EXPECT().PurgeOldRecords(mock.Anything).Return(nil).Once()
		c.EXPECT().PurgeOrphanedTorrents(mock.Anything).Return(nil).Once()
		Expect(Cleanup(c)(context.Background())).To(Succeed())
	})

	It("propagates old-records error and skips the rest", func() {
		boom := errors.New("cleanup failed")
		c := dlmocks.NewMockCleaner(GinkgoT())
		c.EXPECT().PurgeOldRecords(mock.Anything).Return(boom).Once()
		Expect(Cleanup(c)(context.Background())).To(MatchError(boom))
	})

	It("logs-and-continues when the orphan purge fails", func() {
		c := dlmocks.NewMockCleaner(GinkgoT())
		c.EXPECT().PurgeOldRecords(mock.Anything).Return(nil).Once()
		c.EXPECT().PurgeOrphanedTorrents(mock.Anything).
			Return(errors.New("orphan boom")).Once()
		Expect(Cleanup(c)(context.Background())).To(Succeed())
	})

	It("does not error on bad retention — logs and returns nil", func() {
		configtest.Setup(map[string]any{
			"events": map[string]any{"retention": "garbage"},
		})
		c := dlmocks.NewMockCleaner(GinkgoT())
		c.EXPECT().PurgeOldRecords(mock.Anything).Return(nil).Once()
		c.EXPECT().PurgeOrphanedTorrents(mock.Anything).Return(nil).Once()
		Expect(Cleanup(c)(context.Background())).To(Succeed())
	})
})
