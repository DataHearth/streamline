package config

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
	// Every spec here loads config, and the loader reads STREAMLINE_* over
	// the file; the specs that test that layer set their own variables.
	DeferCleanup(testutil.IsolateEnv())
})
