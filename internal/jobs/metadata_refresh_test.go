package jobs

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	moviemocks "github.com/datahearth/streamline/internal/media/movie/mocks"
)

var _ = Describe("MetadataRefresh", Label("unit"), func() {
	It("delegates to MetadataRefresher.RefreshStale", func() {
		r := moviemocks.NewMockMetadataRefresher(GinkgoT())
		r.EXPECT().RefreshStale(mock.Anything).Return(nil).Once()
		Expect(MetadataRefresh(r)(context.Background())).To(Succeed())
	})

	It("propagates runner error", func() {
		boom := errors.New("refresh failed")
		r := moviemocks.NewMockMetadataRefresher(GinkgoT())
		r.EXPECT().RefreshStale(mock.Anything).Return(boom).Once()
		Expect(MetadataRefresh(r)(context.Background())).To(MatchError(boom))
	})
})
