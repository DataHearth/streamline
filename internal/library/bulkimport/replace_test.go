package bulkimport

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Replacing a tracked file", Label("unit", "bulkimport"), func() {
	var (
		ctx     context.Context
		dir     string
		tracked string
		scanned string
	)

	write := func(path, content string) {
		GinkgoHelper()
		Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
	}
	read := func(path string) string {
		GinkgoHelper()
		body, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		return string(body)
	}

	BeforeEach(func() {
		ctx = context.Background()
		dir = GinkgoT().TempDir()
		tracked = filepath.Join(dir, "Movie.mkv")
		scanned = filepath.Join(dir, "Movie 1080p.mkv")
		write(tracked, "original")
		write(scanned, "replacement")
	})

	Describe("putBack", func() {
		It("removes what a failed import left at the path", func() {
			moved, err := setAside(ctx, tracked)
			Expect(err).NotTo(HaveOccurred())
			// A copy that ran out of space part-way.
			write(tracked, "repl")

			putBack(ctx, moved, scanned)

			Expect(read(tracked)).To(Equal("original"))
			Expect(tracked + replacedSuffix).NotTo(BeAnExistingFile())
			Expect(read(scanned)).To(Equal("replacement"))
		})

		It("keeps what sits at the path when the scanned file is gone", func() {
			moved, err := setAside(ctx, tracked)
			Expect(err).NotTo(HaveOccurred())
			// A move import: the file at the path is the only copy.
			Expect(os.Rename(scanned, tracked)).To(Succeed())

			putBack(ctx, moved, scanned)

			Expect(read(tracked)).To(Equal("replacement"))
			Expect(read(tracked + replacedSuffix)).To(Equal("original"))
		})

		It("keeps the new file when the import succeeded", func() {
			moved, err := setAside(ctx, tracked)
			Expect(err).NotTo(HaveOccurred())
			write(tracked, "replacement")

			putBack(ctx, moved, "")

			Expect(read(tracked)).To(Equal("replacement"))
			Expect(read(tracked + replacedSuffix)).To(Equal("original"))
		})
	})

	Describe("setAside", func() {
		It("never renames over a file an earlier replacement set aside", func() {
			write(tracked+replacedSuffix, "earlier original")

			moved, err := setAside(ctx, tracked)

			Expect(err).To(HaveOccurred())
			Expect(moved).To(BeEmpty())
			Expect(read(tracked)).To(Equal("original"))
			Expect(read(tracked + replacedSuffix)).To(Equal("earlier original"))
		})

		It("puts back what it moved when a later file cannot be set aside", func() {
			other := filepath.Join(dir, "Other.mkv")
			write(other, "other")
			write(other+replacedSuffix, "earlier original")

			_, err := setAside(ctx, tracked, other)

			Expect(err).To(HaveOccurred())
			Expect(read(tracked)).To(Equal("original"))
			Expect(tracked + replacedSuffix).NotTo(BeAnExistingFile())
		})
	})

	Describe("sameFile", func() {
		It("is an error, not a difference, when the scanned file is gone", func() {
			Expect(os.Remove(scanned)).To(Succeed())

			_, err := sameFile(tracked, scanned)

			Expect(err).To(HaveOccurred())
		})

		It("reads a missing tracked file as a different one", func() {
			Expect(os.Remove(tracked)).To(Succeed())

			same, err := sameFile(tracked, scanned)

			Expect(err).NotTo(HaveOccurred())
			Expect(same).To(BeFalse())
		})

		It("recognises a hardlink of the tracked file", func() {
			link := filepath.Join(dir, "link.mkv")
			Expect(os.Link(tracked, link)).To(Succeed())

			same, err := sameFile(tracked, link)

			Expect(err).NotTo(HaveOccurred())
			Expect(same).To(BeTrue())
		})
	})
})
