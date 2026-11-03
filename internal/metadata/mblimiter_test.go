package metadata

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("mbLimiter", Label("unit", "metadata"), func() {
	It("gives an interactive caller its token at once", func() {
		l := newMBLimiter(time.Hour, 0)
		start := time.Now()
		Expect(l.Wait(context.Background())).To(Succeed())
		Expect(time.Since(start)).To(BeNumerically("<", time.Second))
	})

	It("keeps the background gap between two background calls", func() {
		l := newMBLimiter(10*time.Millisecond, 250*time.Millisecond)
		bg := Background(context.Background())
		Expect(l.Wait(bg)).To(Succeed())
		first := time.Now()
		Expect(l.Wait(bg)).To(Succeed())
		Expect(time.Since(first)).To(BeNumerically(">=", 240*time.Millisecond))
	})

	It("lets an interactive caller go ahead of a waiting background one", func() {
		l := newMBLimiter(300*time.Millisecond, 10*time.Millisecond)
		bg := Background(context.Background())
		Expect(l.Wait(bg)).To(Succeed())

		var (
			mu    sync.Mutex
			order []string
			wg    sync.WaitGroup
		)
		record := func(who string) {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, who)
		}
		wg.Go(func() {
			Expect(l.Wait(context.Background())).To(Succeed())
			record("interactive")
		})
		time.Sleep(20 * time.Millisecond)
		wg.Go(func() {
			Expect(l.Wait(bg)).To(Succeed())
			record("background")
		})
		wg.Wait()
		Expect(order).To(Equal([]string{"interactive", "background"}))
	})

	It("serves interactive, then prefetch, then background callers", func() {
		l := newMBLimiter(300*time.Millisecond, 10*time.Millisecond)
		Expect(l.Wait(context.Background())).To(Succeed())

		var (
			mu    sync.Mutex
			order []string
			wg    sync.WaitGroup
		)
		wait := func(ctx context.Context, who string) {
			Expect(l.Wait(ctx)).To(Succeed())
			mu.Lock()
			defer mu.Unlock()
			order = append(order, who)
		}
		wg.Go(func() { wait(Background(context.Background()), "background") })
		time.Sleep(20 * time.Millisecond)
		wg.Go(func() { wait(Prefetch(context.Background()), "prefetch") })
		time.Sleep(20 * time.Millisecond)
		wg.Go(func() { wait(context.Background(), "interactive") })
		wg.Wait()
		Expect(order).To(Equal([]string{"interactive", "prefetch", "background"}))
	})

	It("keeps no gap between two prefetch calls", func() {
		l := newMBLimiter(10*time.Millisecond, time.Hour)
		pf := Prefetch(context.Background())
		Expect(l.Wait(pf)).To(Succeed())
		first := time.Now()
		Expect(l.Wait(pf)).To(Succeed())
		Expect(time.Since(first)).To(BeNumerically("<", time.Second))
	})

	It("stops waiting when the context ends", func() {
		l := newMBLimiter(time.Hour, time.Hour)
		bg := Background(context.Background())
		Expect(l.Wait(bg)).To(Succeed())
		ctx, cancel := context.WithTimeout(bg, 50*time.Millisecond)
		defer cancel()
		Expect(l.Wait(ctx)).To(MatchError(context.DeadlineExceeded))
	})
})
