package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

func TestDemo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Demo Suite")
}

var _ = BeforeSuite(func() { DeferCleanup(testutil.InstallSlog()) })
