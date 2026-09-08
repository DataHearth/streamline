// Package indexers holds container-backed specs that drive streamline's
// indexer clients against the real services they speak to.
//
// Everything here is Label("e2e", "containers") so `task test:e2e:containers`
// selects it and every other target skips it. It is a package of its own
// rather than a file in e2e/api because these specs never touch streamline's
// REST API — they exercise internal/indexer directly against a real daemon.
package indexers

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestIndexers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Indexers Suite")
}

var _ = BeforeSuite(func() { DeferCleanup(testutil.InstallSlog()) })
