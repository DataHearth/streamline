package download

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func TestDownload(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Download Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})

// Seed the config singleton with defaults + a tmp data_dir for every spec;
// the Manager pulls data_dir from config.Get() at construction.
var _ = BeforeEach(func() {
	configtest.Setup()
})
