package bittorrent

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestBittorrent(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bittorrent Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})
