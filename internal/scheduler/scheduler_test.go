package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scheduler", Label("unit", "scheduler"), func() {
	Describe("Register and Start", func() {
		It("should run registered jobs at their interval", func() {
			s := New()

			var count atomic.Int32
			s.Register(
				"test-job",
				50*time.Millisecond,
				func(_ context.Context) error {
					count.Add(1)
					return nil
				},
			)

			ctx, cancel := context.WithTimeout(
				context.Background(),
				200*time.Millisecond,
			)
			defer cancel()

			s.Start(ctx)

			// Initial run + ~3 ticks in 200ms with 50ms interval
			Expect(count.Load()).To(BeNumerically(">=", 2))
		})

		It("logs and continues when a job returns an error", func() {
			s := New()

			var count atomic.Int32
			s.Register(
				"fail-job",
				30*time.Millisecond,
				func(_ context.Context) error {
					count.Add(1)
					return errors.New("job blew up")
				},
			)

			ctx, cancel := context.WithTimeout(
				context.Background(),
				120*time.Millisecond,
			)
			defer cancel()

			s.Start(ctx)

			// Must have executed at least once and not panicked.
			Expect(count.Load()).To(BeNumerically(">=", 1))
		})

		It("Start exits promptly when ctx is canceled", func() {
			s := New()
			s.Register(
				"noop",
				time.Hour,
				func(_ context.Context) error { return nil },
			)

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				s.Start(ctx)
				close(done)
			}()

			cancel()
			Eventually(done).WithTimeout(500 * time.Millisecond).Should(BeClosed())
		})

		It("should not run duplicate jobs concurrently", func() {
			s := New()

			var running atomic.Int32
			var maxConcurrent atomic.Int32

			s.Register(
				"slow-job",
				10*time.Millisecond,
				func(_ context.Context) error {
					cur := running.Add(1)
					for {
						old := maxConcurrent.Load()
						if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
							break
						}
					}
					time.Sleep(50 * time.Millisecond)
					running.Add(-1)
					return nil
				},
			)

			ctx, cancel := context.WithTimeout(
				context.Background(),
				200*time.Millisecond,
			)
			defer cancel()

			s.Start(ctx)

			Expect(maxConcurrent.Load()).To(Equal(int32(1)))
		})
	})

	Describe("List", func() {
		It("returns registered jobs sorted by name", func() {
			s := New()
			s.Register(
				"zeta",
				time.Second,
				func(context.Context) error { return nil },
			)
			s.Register(
				"alpha",
				2*time.Second,
				func(context.Context) error { return nil },
			)
			s.Register(
				"mike",
				3*time.Second,
				func(context.Context) error { return nil },
				WithSystem(),
			)

			got := s.List()
			Expect(got).To(HaveLen(3))
			Expect(got[0].Name).To(Equal("alpha"))
			Expect(got[1].Name).To(Equal("mike"))
			Expect(got[2].Name).To(Equal("zeta"))
			Expect(got[1].System).To(BeTrue())
			Expect(got[0].System).To(BeFalse())
			Expect(got[0].Interval).To(Equal(2 * time.Second))
			Expect(got[0].Running).To(BeFalse())
		})
	})

	Describe("StateHook", func() {
		It("calls OnStart then OnEnd with success on a clean run", func() {
			h := &fakeHook{}
			s := New(WithStateHook(h))
			s.Register("ok", time.Hour, func(context.Context) error { return nil })

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { s.Start(ctx); close(done) }()

			Eventually(
				func() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.ends) },
			).
				WithTimeout(2 * time.Second).
				Should(Equal(1))

			cancel()
			<-done

			Expect(h.starts).To(ConsistOf("ok"))
			Expect(h.ends).To(HaveLen(1))
			Expect(h.ends[0].Status).To(Equal("success"))
			Expect(h.ends[0].HasError).To(BeFalse())
		})

		It("reports status=error and propagates the run error to OnEnd", func() {
			h := &fakeHook{}
			s := New(WithStateHook(h))
			boom := errors.New("boom")
			s.Register("bad", time.Hour, func(context.Context) error { return boom })

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { s.Start(ctx); close(done) }()

			Eventually(
				func() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.ends) },
			).
				WithTimeout(2 * time.Second).
				Should(Equal(1))

			cancel()
			<-done

			Expect(h.ends[0].Status).To(Equal("error"))
			Expect(h.ends[0].HasError).To(BeTrue())
		})

		It(
			"reports status=skipped without OnStart when previous run still active",
			func() {
				h := &fakeHook{}
				s := New(WithStateHook(h))

				gate := make(chan struct{})
				release := make(chan struct{})
				var first atomic.Bool
				s.Register("slow", 25*time.Millisecond, func(context.Context) error {
					if !first.Swap(true) {
						close(gate)
						<-release
					}
					return nil
				})

				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan struct{})
				go func() { s.Start(ctx); close(done) }()

				<-gate
				Eventually(func() int {
					h.mu.Lock()
					defer h.mu.Unlock()
					n := 0
					for _, e := range h.ends {
						if e.Status == "skipped" {
							n++
						}
					}
					return n
				}).WithTimeout(2 * time.Second).Should(BeNumerically(">=", 1))

				close(release)

				Eventually(func() bool {
					h.mu.Lock()
					defer h.mu.Unlock()
					for _, e := range h.ends {
						if e.Status == "success" {
							return true
						}
					}
					return false
				}).WithTimeout(2 * time.Second).Should(BeTrue())

				cancel()
				<-done

				h.mu.Lock()
				defer h.mu.Unlock()
				Expect(h.starts).ToNot(BeEmpty())
				var sawSkipped, sawSuccess bool
				for _, e := range h.ends {
					if e.Status == "skipped" {
						sawSkipped = true
					}
					if e.Status == "success" {
						sawSuccess = true
					}
				}
				Expect(sawSkipped).To(BeTrue())
				Expect(sawSuccess).To(BeTrue())
			},
		)
	})

	Describe("Pause/Resume", func() {
		It("stops scheduling new ticks and resumes them on demand", func() {
			var calls atomic.Int32
			s := New()
			s.Register("counter", 50*time.Millisecond, func(context.Context) error {
				calls.Add(1)
				return nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { s.Start(ctx); close(done) }()

			Eventually(
				calls.Load,
			).WithTimeout(2 * time.Second).
				Should(BeNumerically(">=", 1))

			Expect(s.Pause("counter")).To(Succeed())
			// Wait for any in-flight run to settle, then snapshot.
			time.Sleep(50 * time.Millisecond)
			paused := calls.Load()
			Consistently(calls.Load, 250*time.Millisecond).Should(Equal(paused))

			Expect(s.Resume("counter")).To(Succeed())
			Eventually(
				calls.Load,
			).WithTimeout(2 * time.Second).
				Should(BeNumerically(">", paused))

			cancel()
			<-done
		})

		It("rejects Pause/Resume on system jobs", func() {
			s := New()
			s.Register(
				"sys",
				time.Hour,
				func(context.Context) error { return nil },
				WithSystem(),
			)
			Expect(s.Pause("sys")).To(MatchError(ErrJobSystem))
			Expect(s.Resume("sys")).To(MatchError(ErrJobSystem))
		})

		It("returns ErrJobUnknown for an unknown name", func() {
			s := New()
			Expect(s.Pause("nope")).To(MatchError(ErrJobUnknown))
			Expect(s.Resume("nope")).To(MatchError(ErrJobUnknown))
		})

		It(
			"returns ErrJobAlreadyPaused on double-pause and ErrJobNotPaused on resume-when-not-paused",
			func() {
				s := New()
				s.Register(
					"x",
					time.Hour,
					func(context.Context) error { return nil },
				)
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan struct{})
				go func() { s.Start(ctx); close(done) }()

				// Wait for Start to register the goroutine + stopCh.
				Eventually(func() bool {
					j, err := s.job("x")
					if err != nil {
						return false
					}
					j.mu.Lock()
					defer j.mu.Unlock()
					return j.stopCh != nil
				}).WithTimeout(time.Second).Should(BeTrue())

				Expect(s.Resume("x")).To(MatchError(ErrJobNotPaused))
				Expect(s.Pause("x")).To(Succeed())
				Expect(s.Pause("x")).To(MatchError(ErrJobAlreadyPaused))

				cancel()
				<-done
			},
		)

		It("lets an in-flight run complete after Pause", func() {
			release := make(chan struct{})
			started := make(chan struct{})
			var ended atomic.Bool
			s := New()
			s.Register("slow", time.Hour, func(ctx context.Context) error {
				close(started)
				<-release
				ended.Store(true)
				return nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { s.Start(ctx); close(done) }()

			<-started
			Expect(s.Pause("slow")).To(Succeed())
			Expect(
				ended.Load(),
			).To(BeFalse(), "Pause should not abort in-flight run")
			close(release)
			Eventually(ended.Load).WithTimeout(time.Second).Should(BeTrue())

			cancel()
			<-done
		})
	})

	Describe("Reschedule", func() {
		It("applies the new interval to subsequent ticks", func() {
			var calls atomic.Int32
			s := New()
			s.Register("metro", 200*time.Millisecond, func(context.Context) error {
				calls.Add(1)
				return nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { s.Start(ctx); close(done) }()

			Eventually(
				calls.Load,
			).WithTimeout(time.Second).
				Should(BeNumerically(">=", 1))

			Expect(s.Reschedule("metro", 30*time.Millisecond)).To(Succeed())

			Eventually(
				calls.Load,
			).WithTimeout(time.Second).
				Should(BeNumerically(">=", 5))

			cancel()
			<-done
		})

		It("rejects Reschedule on system jobs and unknown names", func() {
			s := New()
			s.Register(
				"sys",
				time.Hour,
				func(context.Context) error { return nil },
				WithSystem(),
			)
			Expect(s.Reschedule("sys", time.Minute)).To(MatchError(ErrJobSystem))
			Expect(s.Reschedule("nope", time.Minute)).To(MatchError(ErrJobUnknown))
		})
	})

	Describe("RunNow", func() {
		It("triggers a one-off execution", func() {
			var calls atomic.Int32
			s := New()
			s.Register("once", time.Hour, func(context.Context) error {
				calls.Add(1)
				return nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { s.Start(ctx); close(done) }()

			Eventually(
				calls.Load,
			).WithTimeout(time.Second).
				Should(BeNumerically(">=", 1))
			// calls>=1 only means fn returned; running stays true through
			// executeJob's tail, so wait for it to clear or RunNow races
			// to ErrJobBusy.
			Eventually(func() bool {
				info, err := s.Get("once")
				Expect(err).NotTo(HaveOccurred())
				return info.Running
			}).WithTimeout(time.Second).Should(BeFalse())
			before := calls.Load()

			Expect(s.RunNow("once")).To(Succeed())
			Eventually(calls.Load).WithTimeout(time.Second).Should(Equal(before + 1))

			cancel()
			<-done
		})

		It("returns ErrJobBusy when the job is currently running", func() {
			release := make(chan struct{})
			started := make(chan struct{})
			s := New()
			s.Register("slow", time.Hour, func(ctx context.Context) error {
				close(started)
				<-release
				return nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { s.Start(ctx); close(done) }()

			<-started
			Expect(s.RunNow("slow")).To(MatchError(ErrJobBusy))
			close(release)
			cancel()
			<-done
		})

		It("rejects RunNow on system jobs and unknown names", func() {
			s := New()
			s.Register(
				"sys",
				time.Hour,
				func(context.Context) error { return nil },
				WithSystem(),
			)
			Expect(s.RunNow("sys")).To(MatchError(ErrJobSystem))
			Expect(s.RunNow("nope")).To(MatchError(ErrJobUnknown))
		})

		It("is allowed even when the job is paused", func() {
			var calls atomic.Int32
			s := New()
			s.Register("paused", time.Hour, func(context.Context) error {
				calls.Add(1)
				return nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { s.Start(ctx); close(done) }()

			Eventually(
				calls.Load,
			).WithTimeout(time.Second).
				Should(BeNumerically(">=", 1))
			Expect(s.Pause("paused")).To(Succeed())
			// Wait for the initial run to clear running before RunNow.
			Eventually(func() bool {
				info, err := s.Get("paused")
				Expect(err).NotTo(HaveOccurred())
				return info.Running
			}).WithTimeout(time.Second).Should(BeFalse())
			before := calls.Load()
			Expect(s.RunNow("paused")).To(Succeed())
			Eventually(calls.Load).WithTimeout(time.Second).Should(Equal(before + 1))

			cancel()
			<-done
		})
	})
})

type fakeHook struct {
	mu     sync.Mutex
	starts []string
	ends   []hookEnd
}

type hookEnd struct {
	Name     string
	Status   string
	HasError bool
	Duration time.Duration
}

func (f *fakeHook) OnStart(_ context.Context, name string, _ time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, name)
}

func (f *fakeHook) OnEnd(
	_ context.Context,
	name string,
	_ time.Time,
	status string,
	runErr error,
	dur time.Duration,
) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ends = append(
		f.ends,
		hookEnd{Name: name, Status: status, HasError: runErr != nil, Duration: dur},
	)
}
