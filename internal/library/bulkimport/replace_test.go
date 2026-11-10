package bulkimport

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("sameFile", Label("unit", "bulkimport"), func() {
	var dir, tracked, scanned string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		tracked = filepath.Join(dir, "Movie.mkv")
		scanned = filepath.Join(dir, "Movie 1080p.mkv")
		Expect(os.WriteFile(tracked, []byte("tracked"), 0o644)).To(Succeed())
		Expect(os.WriteFile(scanned, []byte("scanned"), 0o644)).To(Succeed())
	})

	It("tells two files in one folder apart", func() {
		Expect(sameFile(tracked, scanned)).To(BeFalse())
	})

	It("recognises a hardlink of the tracked file", func() {
		link := filepath.Join(dir, "link.mkv")
		Expect(os.Link(tracked, link)).To(Succeed())

		Expect(sameFile(tracked, link)).To(BeTrue())
	})

	It("recognises the tracked file reached through a symlinked folder", func() {
		alias := filepath.Join(GinkgoT().TempDir(), "alias")
		symlinkOrSkip(dir, alias)

		Expect(sameFile(tracked, filepath.Join(alias, "Movie.mkv"))).To(BeTrue())
	})

	It("reads a missing tracked file as a different one", func() {
		Expect(os.Remove(tracked)).To(Succeed())

		Expect(sameFile(tracked, scanned)).To(BeFalse())
	})
})
