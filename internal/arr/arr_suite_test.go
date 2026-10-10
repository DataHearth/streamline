package arr

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestArr(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Arr Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})
