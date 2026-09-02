package langcode_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestLangCode(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "LangCode Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})
