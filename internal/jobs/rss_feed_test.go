package jobs

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	rssmocks "github.com/datahearth/streamline/internal/rss/mocks"
)

var _ = Describe("RSSFeed", Label("unit"), func() {
	It("delegates to FeedRunner.Run", func() {
		runner := rssmocks.NewMockFeedRunner(GinkgoT())
		runner.EXPECT().Run(mock.Anything).Return(nil).Once()
		Expect(RSSFeed(runner)(context.Background())).To(Succeed())
	})

	It("propagates runner error", func() {
		boom := errors.New("feed failed")
		runner := rssmocks.NewMockFeedRunner(GinkgoT())
		runner.EXPECT().Run(mock.Anything).Return(boom).Once()
		Expect(RSSFeed(runner)(context.Background())).To(MatchError(boom))
	})
})
