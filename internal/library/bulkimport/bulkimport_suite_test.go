package bulkimport

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestBulkImport(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BulkImport Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})
