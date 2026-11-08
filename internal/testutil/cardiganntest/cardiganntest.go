// Package cardiganntest serves a few real upstream Cardigann definitions
// (testdata/, provenance in testdata/NOTICE) to tests: converted the way a
// fetch converts them, as the catalog in force or as a version an indexer
// entry is pinned to.
package cardiganntest

import (
	"crypto/sha1"
	"embed"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/cardigann/snapshot"
)

//go:embed testdata/*.yml
var fixtures embed.FS

// Upstream is the revision the fixtures were read at.
var Upstream = cardigann.Upstream{
	Repository: "https://github.com/Prowlarr/Indexers",
	Commit:     "8b78ad64e4d1dfc531af697c236d570f77c1a22c",
	Date:       "2026-10-12T09:43:47+02:00",
}

// Files returns the fixtures as upstream files.
func Files() []snapshot.File {
	GinkgoHelper()
	entries, err := fixtures.ReadDir("testdata")
	Expect(err).NotTo(HaveOccurred())
	out := make([]snapshot.File, 0, len(entries))
	for _, e := range entries {
		b, err := fixtures.ReadFile("testdata/" + e.Name())
		Expect(err).NotTo(HaveOccurred())
		out = append(out, snapshot.File{Version: 11, Name: e.Name(), Data: b})
	}
	return out
}

// Install puts the fixtures in force as the fetched catalog for the calling
// spec.
func Install() {
	GinkgoHelper()
	cat, defs := snapshot.Build(Files(), Upstream)
	Expect(cat.Skipped).To(BeEmpty())
	prev := definitions.Install(definitions.NewSet(cat, defs))
	DeferCleanup(func() { definitions.Install(prev) })
}

// Spec is one fixture converted, as an indexer runs it.
func Spec(file string) []byte {
	GinkgoHelper()
	for _, f := range Files() {
		if strings.TrimSuffix(f.Name, ".yml") == file {
			_, defs := snapshot.Build([]snapshot.File{f}, Upstream)
			Expect(defs).To(HaveKey(file))
			return defs[file]
		}
	}
	Fail("no cardigann fixture " + file)
	return nil
}

// Definition is one fixture converted and decoded.
func Definition(file string) *cardigann.Definition {
	GinkgoHelper()
	d, err := definitions.Decode(Spec(file))
	Expect(err).NotTo(HaveOccurred())
	return d
}

// Commit is a commit id made up from spec's content.
func Commit(spec []byte) string {
	return fmt.Sprintf(
		"%x",
		sha1.Sum(spec),
	)
}

// Keep stores spec as a version of file under dataDir, at Commit(spec),
// which it returns for an entry to be pinned to.
func Keep(dataDir, file string, spec []byte) string {
	GinkgoHelper()
	commit := Commit(spec)
	Expect(
		definitions.Keep(dataDir, file, commit, spec),
	).To(Succeed())
	return commit
}
