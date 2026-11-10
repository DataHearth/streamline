package bulkimport

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync"

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

	It("recognises the tracked file reached through a symlinked folder", func() {
		alias := filepath.Join(GinkgoT().TempDir(), "alias")
		symlinkOrSkip(dir, alias)

		same, err := sameFile(tracked, filepath.Join(alias, "Movie.mkv"))

		Expect(err).NotTo(HaveOccurred())
		Expect(same).To(BeTrue())
	})

	It("reads a missing tracked file as a different one", func() {
		Expect(os.Remove(tracked)).To(Succeed())

		same, err := sameFile(tracked, scanned)

		Expect(err).NotTo(HaveOccurred())
		Expect(same).To(BeFalse())
	})

	It("is an error, not a difference, when the scanned file is gone", func() {
		Expect(os.Remove(scanned)).To(Succeed())

		_, err := sameFile(tracked, scanned)

		Expect(err).To(MatchError(fs.ErrNotExist))
	})

	It("is an error when the scanned path is not a regular file", func() {
		_, err := sameFile(tracked, dir)

		Expect(err).To(MatchError(ContainSubstring("not a regular file")))
	})

	It("is an error when the tracked path cannot be read", func() {
		// A path through a regular file fails with ENOTDIR, not absence.
		_, err := sameFile(filepath.Join(scanned, "Movie.mkv"), scanned)

		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("keyedMutex", Label("unit", "bulkimport"), func() {
	It("lets one holder of a key in at a time", func() {
		var (
			k       keyedMutex
			wg      sync.WaitGroup
			inside  int
			entered int
		)
		for range 50 {
			wg.Go(func() {
				defer k.lock(42)()
				inside++
				Expect(inside).To(Equal(1))
				entered++
				inside--
			})
		}
		wg.Wait()

		Expect(entered).To(Equal(50))
	})

	It("does not make one key wait for another", func() {
		var k keyedMutex
		unlockA := k.lock(1)
		unlockB := k.lock(2)
		unlockB()
		unlockA()
	})

	It("forgets a key once nobody holds it", func() {
		var k keyedMutex
		unlock := k.lock(42)
		Expect(k.locks).To(HaveKey(uint32(42)))

		unlock()

		Expect(k.locks).To(BeEmpty())
	})
})
