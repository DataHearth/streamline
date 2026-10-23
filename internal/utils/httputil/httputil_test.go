package httputil

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

var _ = Describe("RetryAfterSeconds", Label("unit"), func() {
	It("floors a sub-second wait at one second", func() {
		Expect(RetryAfterSeconds(400 * time.Millisecond)).To(Equal("1"))
	})

	It("never renders zero, whatever it is handed", func() {
		Expect(RetryAfterSeconds(0)).To(Equal("1"))
		Expect(RetryAfterSeconds(-time.Hour)).To(Equal("1"))
	})

	It("rounds a partial second up", func() {
		Expect(RetryAfterSeconds(2100 * time.Millisecond)).To(Equal("3"))
	})

	It("passes a whole wait through", func() {
		Expect(RetryAfterSeconds(15 * time.Minute)).To(Equal("900"))
	})
})

func TestHTTPUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HTTPUtil Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})
