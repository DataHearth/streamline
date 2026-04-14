package random

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestRandom(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Random Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})

var _ = Describe("Must", Label("unit"), func() {
	It("returns base64url-encoded string of non-zero length", func() {
		Expect(Must(32)).NotTo(BeEmpty())
	})

	It("returns unique values across calls", func() {
		Expect(Must(32)).NotTo(Equal(Must(32)))
	})

	DescribeTable("size scales with byte count",
		func(small, large int) {
			Expect(len(Must(large))).To(BeNumerically(">", len(Must(small))))
		},
		Entry("16 vs 64", 16, 64),
		Entry("8 vs 32", 8, 32),
	)
})
