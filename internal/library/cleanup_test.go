package library

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RemoveMediaFile", Label("unit", "library"), func() {
	var root string

	touch := func(path string) string {
		GinkgoHelper()
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, []byte("x"), 0o644)).To(Succeed())
		return path
	}

	BeforeEach(func() {
		root = GinkgoT().TempDir()
	})

	It("removes the media file, its sidecars and the emptied folder", func() {
		dir := filepath.Join(root, "Amélie (2001)")
		media := touch(filepath.Join(dir, "Amélie (2001) [1080p].mkv"))
		touch(filepath.Join(dir, "Amélie (2001) [1080p].nfo"))
		touch(filepath.Join(dir, "Amélie (2001) [1080p].en.srt"))
		touch(filepath.Join(dir, "Amélie (2001) [1080p]-thumb.jpg"))

		Expect(RemoveMediaFile(media, root)).To(Succeed())

		Expect(dir).NotTo(BeADirectory())
		Expect(root).To(BeADirectory())
	})

	It("keeps unrelated files and the folder holding them", func() {
		dir := filepath.Join(root, "Mixed")
		media := touch(filepath.Join(dir, "Movie [1080p].mkv"))
		keep := touch(filepath.Join(dir, "poster.jpg"))

		Expect(RemoveMediaFile(media, root)).To(Succeed())

		Expect(media).NotTo(BeAnExistingFile())
		Expect(keep).To(BeAnExistingFile())
	})

	It("never takes another episode's video, even on a prefix collision", func() {
		dir := filepath.Join(root, "Show", "Season 01")
		media := touch(filepath.Join(dir, "Show - S01E01 - Pilot.mkv"))
		other := touch(filepath.Join(dir, "Show - S01E01 - Pilot - Part 2.mkv"))

		Expect(RemoveMediaFile(media, root)).To(Succeed())

		Expect(media).NotTo(BeAnExistingFile())
		Expect(other).To(BeAnExistingFile())
	})

	It("prunes season and show folders but stops at the library root", func() {
		dir := filepath.Join(root, "Show", "Season 01")
		media := touch(filepath.Join(dir, "Show - S01E01.mkv"))

		Expect(RemoveMediaFile(media, root)).To(Succeed())

		Expect(filepath.Join(root, "Show")).NotTo(BeADirectory())
		Expect(root).To(BeADirectory())
	})

	It("is a no-op when the folder is already gone", func() {
		Expect(RemoveMediaFile(filepath.Join(root, "gone", "x.mkv"), root)).
			To(Succeed())
	})
})
