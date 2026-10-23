package sysinfo

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestSysinfo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sysinfo Suite")
}

var _ = BeforeSuite(func() { DeferCleanup(testutil.InstallSlog()) })
