package jobs

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	bookmocks "github.com/datahearth/streamline/internal/media/book/mocks"
	musicmocks "github.com/datahearth/streamline/internal/media/music/mocks"
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

	It("runs every runner in order", func() {
		music := musicmocks.NewMockMissingSearcher(GinkgoT())
		var order []string
		runner.EXPECT().Run(mock.Anything).
			RunAndReturn(func(context.Context) error {
				order = append(order, "movie")
				return nil
			}).Once()
		music.EXPECT().SearchMissing(mock.Anything).
			RunAndReturn(func(context.Context) error {
				order = append(order, "music")
				return nil
			}).Once()

		Expect(MissingSearch(runner, RunnerFunc(music.SearchMissing))(ctx)).
			To(Succeed())
		Expect(order).To(Equal([]string{"movie", "music"}))
	})

	It("joins errors while the other runners still run", func() {
		music := musicmocks.NewMockMissingSearcher(GinkgoT())
		boom := errors.New("movie sync failed")
		musicBoom := errors.New("music search failed")
		runner.EXPECT().Run(mock.Anything).Return(boom).Once()
		music.EXPECT().SearchMissing(mock.Anything).Return(musicBoom).Once()

		err := MissingSearch(runner, RunnerFunc(music.SearchMissing))(ctx)
		Expect(err).To(MatchError(boom))
		Expect(err).To(MatchError(musicBoom))
	})

	It("runs the book searcher after the music searcher", func() {
		music := musicmocks.NewMockMissingSearcher(GinkgoT())
		books := bookmocks.NewMockMissingSearcher(GinkgoT())
		boom := errors.New("book search failed")
		var order []string
		runner.EXPECT().Run(mock.Anything).
			RunAndReturn(func(context.Context) error {
				order = append(order, "movie")
				return nil
			}).Once()
		music.EXPECT().SearchMissing(mock.Anything).
			RunAndReturn(func(context.Context) error {
				order = append(order, "music")
				return nil
			}).Once()
		books.EXPECT().SearchMissing(mock.Anything).
			RunAndReturn(func(context.Context) error {
				order = append(order, "book")
				return boom
			}).Once()

		err := MissingSearch(
			runner,
			RunnerFunc(music.SearchMissing),
			RunnerFunc(books.SearchMissing),
		)(ctx)
		Expect(err).To(MatchError(boom))
		Expect(order).To(Equal([]string{"movie", "music", "book"}))
	})
})
