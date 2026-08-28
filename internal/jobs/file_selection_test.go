package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeResolver returns a scripted pending count per call and records how many
// times the pass ran.
type fakeResolver struct {
	calls   atomic.Int32
	pending []int
	err     error
}

func (f *fakeResolver) RunSelectionPass(context.Context) (int, error) {
	n := int(f.calls.Add(1)) - 1
	if f.err != nil {
		return 0, f.err
	}
	if n < len(f.pending) {
		return f.pending[n], nil
	}
	return 0, nil
}

var _ = Describe("FileSelection", Label("unit", "jobs"), func() {
	It("runs the pass once when nothing is pending", func() {
		r := &fakeResolver{}

		Expect(FileSelection(r)(context.Background())).To(Succeed())
		Expect(r.calls.Load()).To(Equal(int32(1)))
	})

	It("keeps polling while the pass still finds pending records", func() {
		r := &fakeResolver{pending: []int{2, 1}}

		done := make(chan error, 1)
		go func() { done <- FileSelection(r)(context.Background()) }()

		// Two pending passes at selectionBusyInterval apart, then a third
		// that finds nothing and ends the run.
		Eventually(func() int32 { return r.calls.Load() }).
			WithTimeout(3 * selectionBusyInterval).
			Should(Equal(int32(3)))
		Eventually(done).WithTimeout(selectionBusyInterval).Should(Receive(BeNil()))
	})

	It("returns the pass error without polling again", func() {
		r := &fakeResolver{err: errors.New("boom")}

		Expect(FileSelection(r)(context.Background())).To(MatchError("boom"))
		Expect(r.calls.Load()).To(Equal(int32(1)))
	})

	It("stops polling when the context is cancelled", func() {
		r := &fakeResolver{pending: []int{1, 1, 1, 1, 1, 1, 1, 1}}
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan error, 1)
		go func() { done <- FileSelection(r)(ctx) }()

		Eventually(func() int32 { return r.calls.Load() }).
			WithTimeout(2 * selectionBusyInterval).
			Should(BeNumerically(">=", 1))
		cancel()

		Eventually(done).WithTimeout(2 * time.Second).Should(Receive(BeNil()))
	})
})
