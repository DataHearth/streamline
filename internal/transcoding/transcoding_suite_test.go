package transcoding

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestTranscoding(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Transcoding Suite")
}

var _ = BeforeSuite(func() { DeferCleanup(testutil.InstallSlog()) })
