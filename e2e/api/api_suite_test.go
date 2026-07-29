package api

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
	"github.com/datahearth/streamline/internal/testutil/apptest"
)

var baseURL string

func TestAPI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E API Suite", Label("api"))
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
	baseURL = apptest.Start()
})
