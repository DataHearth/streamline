package qualityctx_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestQualityCtx(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "QualityCtx Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})
