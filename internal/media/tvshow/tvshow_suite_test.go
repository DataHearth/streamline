package tvshow

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestTVShowService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TVShow Service Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})
