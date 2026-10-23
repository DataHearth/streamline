package audiotags

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestAudioTags(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AudioTags Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})
