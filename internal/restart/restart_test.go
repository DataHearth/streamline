package restart

import (
	"sync"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = g.Describe("Restart flag", g.Label("unit"), func() {
	g.BeforeEach(func() {
		ResetForTest()
	})

	g.It("starts unset", func() {
		Expect(Pending()).To(BeFalse())
	})

	g.It("flips to true after Mark", func() {
		Mark()
		Expect(Pending()).To(BeTrue())
	})

	g.It("stays true across repeat Mark calls", func() {
		Mark()
		Mark()
		Expect(Pending()).To(BeTrue())
	})

	g.It("clears via ResetForTest", func() {
		Mark()
		ResetForTest()
		Expect(Pending()).To(BeFalse())
	})

	g.It("survives concurrent Mark callers", func() {
		var wg sync.WaitGroup
		for range 64 {
			wg.Go(func() {
				Mark()
			})
		}
		wg.Wait()
		Expect(Pending()).To(BeTrue())
	})
})
