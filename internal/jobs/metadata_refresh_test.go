package jobs

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	moviemocks "github.com/datahearth/streamline/internal/media/movie/mocks"
	musicmocks "github.com/datahearth/streamline/internal/media/music/mocks"
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

	It("refreshes every vertical", func() {
		movies := moviemocks.NewMockMetadataRefresher(GinkgoT())
		music := musicmocks.NewMockMetadataRefresher(GinkgoT())
		movies.EXPECT().RefreshStale(mock.Anything).Return(nil).Once()
		music.EXPECT().RefreshStale(mock.Anything).Return(nil).Once()

		Expect(MetadataRefresh(movies, music)(context.Background())).To(Succeed())
	})

	It("joins errors while the other refreshers still run", func() {
		movies := moviemocks.NewMockMetadataRefresher(GinkgoT())
		music := musicmocks.NewMockMetadataRefresher(GinkgoT())
		boom := errors.New("movie refresh failed")
		musicBoom := errors.New("artist refresh failed")
		movies.EXPECT().RefreshStale(mock.Anything).Return(boom).Once()
		music.EXPECT().RefreshStale(mock.Anything).Return(musicBoom).Once()

		err := MetadataRefresh(movies, music)(context.Background())
		Expect(err).To(MatchError(boom))
		Expect(err).To(MatchError(musicBoom))
	})
})
