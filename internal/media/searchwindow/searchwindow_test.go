package searchwindow

import (
	"context"
	"fmt"
	"math"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/scheduler"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("searchwindow", Label("unit"), func() {
	Describe("Current", func() {
		BeforeEach(func() {
			configtest.Setup(map[string]any{
				"library": map[string]any{
					"no_match_cooldown": "2h",
					"max_grab_failures": 4,
				},
			})
		})

		It("reads the cap and cooldown from the library config", func() {
			w, err := Current(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(w.MaxGrabFailures).To(Equal(uint8(4)))
			Expect(w.NotSearchedSince).To(
				BeTemporally("~", time.Now().Add(-2*time.Hour), time.Minute))
		})

		It("waives both throttles on a manual run", func() {
			got := make(chan context.Context, 4)
			s := scheduler.New()
			s.Register("probe", time.Hour, func(ctx context.Context) error {
				got <- ctx
				return nil
			})
			root, cancel := context.WithCancel(context.Background())
			DeferCleanup(cancel)
			go s.Start(root)
			Eventually(got).Should(Receive())
			Eventually(func() bool {
				info, err := s.Get("probe")
				Expect(err).NotTo(HaveOccurred())
				return info.Running
			}).Should(BeFalse())
			Expect(s.RunNow("probe")).To(Succeed())
			var manual context.Context
			Eventually(got).Should(Receive(&manual))

			w, err := Current(manual)
			Expect(err).NotTo(HaveOccurred())
			Expect(w.MaxGrabFailures).To(Equal(uint8(math.MaxUint8)))
			Expect(w.NotSearchedSince).To(BeTemporally("~", time.Now(), time.Minute))
		})
	})

	Describe("TransportFailure", func() {
		It("recognises an unreachable download client or indexer", func() {
			Expect(TransportFailure(
				fmt.Errorf("grab: %w", download.ErrUnreachable))).To(BeTrue())
			Expect(TransportFailure(
				fmt.Errorf("search: %w", indexer.ErrUnreachable))).To(BeTrue())
		})

		It("does not treat a release-level failure as transport", func() {
			Expect(TransportFailure(download.ErrNoWantedFiles)).To(BeFalse())
			Expect(TransportFailure(nil)).To(BeFalse())
		})
	})
})
