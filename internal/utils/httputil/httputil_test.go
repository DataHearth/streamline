package httputil

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestHTTPUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HTTPUtil Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})
