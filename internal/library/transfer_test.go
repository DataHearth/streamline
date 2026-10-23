package library

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("transferFile", Label("unit", "library"), func() {
	var tmp, src, dst string

	BeforeEach(func() {
		tmp = GinkgoT().TempDir()
		src = filepath.Join(tmp, "src.mkv")
		dst = filepath.Join(tmp, "dst.mkv")
		Expect(os.WriteFile(src, []byte("bytes"), 0o600)).To(Succeed())
	})

	It("hardlinks the file", func() {
		Expect(transferFile(src, dst, "hardlink")).To(Succeed())

		srcStat, err := os.Stat(src)
		Expect(err).NotTo(HaveOccurred())
		dstStat, err := os.Stat(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.SameFile(srcStat, dstStat)).To(BeTrue())
	})

	It("moves the file", func() {
		Expect(transferFile(src, dst, "move")).To(Succeed())

		_, err := os.Stat(src)
		Expect(os.IsNotExist(err)).To(BeTrue())
		contents, err := os.ReadFile(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(contents)).To(Equal("bytes"))
	})

	It("copies the file preserving source", func() {
		Expect(transferFile(src, dst, "copy")).To(Succeed())

		_, err := os.Stat(src)
		Expect(err).NotTo(HaveOccurred())
		contents, err := os.ReadFile(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(contents)).To(Equal("bytes"))
	})

	It("rejects an unknown import mode", func() {
		err := transferFile(src, dst, "teleport")
		Expect(err).To(MatchError(ContainSubstring("unknown import mode")))
	})

	It("returns error when the source is missing (hardlink)", func() {
		err := transferFile(filepath.Join(tmp, "missing"), dst, "hardlink")
		Expect(err).To(HaveOccurred())
	})

	It("returns error when copy cannot open the source", func() {
		err := transferFile(filepath.Join(tmp, "missing"), dst, "copy")
		Expect(err).To(HaveOccurred())
	})

	It("returns error when copy cannot create the destination dir", func() {
		err := transferFile(src, filepath.Join(tmp, "missing-dir", "dst"), "copy")
		Expect(err).To(HaveOccurred())
	})
})
