package searchwindow

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestSearchWindow(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Search Window Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})
