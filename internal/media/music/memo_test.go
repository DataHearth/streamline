package music

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("memo", Label("unit", "music"), func() {
	It("loads once per key within the TTL", func() {
		m := newMemo[int](time.Minute, 4)
		var calls int
		load := func() (int, error) { calls++; return 7, nil }
		for range 3 {
			v, err := m.do("k", load)
			Expect(err).NotTo(HaveOccurred())
			Expect(v).To(Equal(7))
		}
		Expect(calls).To(Equal(1))
	})

	It("loads again once the entry has expired", func() {
		m := newMemo[int](20*time.Millisecond, 4)
		var calls int
		load := func() (int, error) { calls++; return calls, nil }
		_, _ = m.do("k", load)
		time.Sleep(40 * time.Millisecond)
		v, err := m.do("k", load)
		Expect(err).NotTo(HaveOccurred())
		Expect(v).To(Equal(2))
	})

	It("does not cache a failure", func() {
		m := newMemo[int](time.Minute, 4)
		boom := errors.New("boom")
		_, err := m.do("k", func() (int, error) { return 0, boom })
		Expect(err).To(MatchError(boom))
		v, err := m.do("k", func() (int, error) { return 3, nil })
		Expect(err).NotTo(HaveOccurred())
		Expect(v).To(Equal(3))
	})

	It("collapses concurrent loads of one key into one", func() {
		m := newMemo[int](time.Minute, 4)
		var (
			calls   atomic.Int32
			release = make(chan struct{})
			wg      sync.WaitGroup
		)
		for range 5 {
			wg.Go(func() {
				defer GinkgoRecover()
				v, err := m.do("k", func() (int, error) {
					calls.Add(1)
					<-release
					return 9, nil
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(v).To(Equal(9))
			})
		}
		Eventually(calls.Load).Should(Equal(int32(1)))
		close(release)
		wg.Wait()
		Expect(calls.Load()).To(Equal(int32(1)))
	})

	It("evicts the oldest entry past its bound", func() {
		m := newMemo[int](time.Minute, 2)
		for i, k := range []string{"a", "b", "c"} {
			m.put(k, i)
			time.Sleep(2 * time.Millisecond)
		}
		_, ok := m.get("a")
		Expect(ok).To(BeFalse())
		_, ok = m.get("c")
		Expect(ok).To(BeTrue())
	})
})
