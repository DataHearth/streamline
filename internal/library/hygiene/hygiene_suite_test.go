package hygiene

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestHygiene(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Hygiene Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})
