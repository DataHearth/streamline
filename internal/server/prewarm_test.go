package server

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("prewarm", Label("unit"), func() {
	ctx := context.Background()
	write := func(path string, size int) string {
		Expect(os.WriteFile(path, make([]byte, size), 0o600)).To(Succeed())
		return path
	}
	temp := func(name string) string {
		return filepath.Join(GinkgoT().TempDir(), name)
	}

	It("reads every byte when each range spans several chunks", func() {
		size := 2*prewarmStreams*prewarmChunk + 12345
		Expect(prewarm(ctx, []string{write(temp("a"), size)})).
			To(BeEquivalentTo(size))
	})

	It("reads files smaller than one range per stream, and adds them up", func() {
		files := []string{write(temp("a"), 3), write(temp("b"), 100)}
		Expect(prewarm(ctx, files)).To(BeEquivalentTo(103))
	})

	It("reads nothing from a missing, empty or non-regular file", func() {
		files := []string{
			temp("missing"),
			write(temp("empty"), 0),
			GinkgoT().TempDir(),
		}
		Expect(prewarm(ctx, files)).To(BeZero())
	})

	It("stops once the context ends", func() {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		Expect(
			prewarm(canceled, []string{write(temp("a"), prewarmChunk)}),
		).To(BeZero())
	})
})

var _ = Describe("bootFiles", Label("unit"), func() {
	It("names the WAL next to a symlinked database's target", func() {
		target := filepath.Join(GinkgoT().TempDir(), "streamline.db")
		link := filepath.Join(GinkgoT().TempDir(), "streamline.db")
		Expect(os.WriteFile(target, nil, 0o600)).To(Succeed())
		Expect(os.Symlink(target, link)).To(Succeed())
		resolved, err := filepath.EvalSymlinks(target)
		Expect(err).NotTo(HaveOccurred())
		Expect(bootFiles(link)[:2]).To(Equal([]string{resolved + "-wal", resolved}))
	})
})
