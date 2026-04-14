package jobs

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	rssmocks "github.com/datahearth/streamline/internal/rss/mocks"
)

var _ = Describe("MissingSearch", Label("unit"), func() {
	var (
		ctx    context.Context
		runner *rssmocks.MockMissingSearchRunner
	)

	BeforeEach(func() {
		ctx = context.Background()
		runner = rssmocks.NewMockMissingSearchRunner(GinkgoT())
	})

	It("delegates to runner.Run", func() {
		runner.EXPECT().Run(mock.Anything).Return(nil).Once()
		Expect(MissingSearch(runner)(ctx)).To(Succeed())
	})

	It("propagates runner error", func() {
		boom := errors.New("sync failed")
		runner.EXPECT().Run(mock.Anything).Return(boom).Once()
		Expect(MissingSearch(runner)(ctx)).To(MatchError(boom))
	})
})
