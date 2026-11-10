package bulkimport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/library"
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

	Describe("partialSource", func() {
		It("names the scanned file when the transfer itself failed", func() {
			err := fmt.Errorf("transfer file: %w", errors.New("no space left"))

			Expect(partialSource(err, scanned)).To(Equal(scanned))
		})

		DescribeTable("names nothing when the import was refused before it",
			func(refusal error) {
				err := fmt.Errorf("import: %w", refusal)

				Expect(partialSource(err, scanned)).To(BeEmpty())
			},
			Entry("destination taken", library.ErrDestExists),
			Entry("no media", library.ErrNoMedia),
			Entry("several media files", library.ErrMultipleMedia),
			Entry("unsafe path", library.ErrUnsafePath),
		)

		It("leaves another commit's file at the path of a refused import", func() {
			moved, err := setAside(ctx, tracked)
			Expect(err).NotTo(HaveOccurred())
			// Another import landed there first; its scanned file may be
			// gone, and this one's is still in place.
			write(tracked, "someone else's")

			putBack(ctx, moved, partialSource(library.ErrDestExists, scanned))

			Expect(read(tracked)).To(Equal("someone else's"))
			Expect(read(tracked + replacedSuffix)).To(Equal("original"))
		})
	})

	Describe("removeReplaced", func() {
		It("removes a tracked file the replacement sits beside", func() {
			removeReplaced(ctx, tracked, scanned)

			Expect(tracked).NotTo(BeAnExistingFile())
			Expect(read(scanned)).To(Equal("replacement"))
		})

		It("leaves a replacement that took the tracked path", func() {
			removeReplaced(ctx, tracked, tracked)

			Expect(read(tracked)).To(Equal("original"))
		})

		It("leaves a hardlink of the replacement", func() {
			link := filepath.Join(dir, "link.mkv")
			Expect(os.Link(tracked, link)).To(Succeed())

			removeReplaced(ctx, tracked, link)

			Expect(read(tracked)).To(Equal("original"))
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
