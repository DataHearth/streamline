package subsonic

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestSubsonic(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Subsonic Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})
