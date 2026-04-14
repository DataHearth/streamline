package restart

import (
	"testing"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestRestart(t *testing.T) {
	RegisterFailHandler(g.Fail)
	g.RunSpecs(t, "Restart Suite")
}

var _ = g.BeforeSuite(func() {
	g.DeferCleanup(testutil.InstallSlog())
})
