package ebookmeta

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestEbookmeta(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ebookmeta Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})
