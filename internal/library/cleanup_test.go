package library

import (
	"context"
	"io/fs"
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

		Expect(RemoveMediaFile(context.Background(), media, root)).To(Succeed())

		Expect(dir).NotTo(BeADirectory())
		Expect(root).To(BeADirectory())
	})

	It("refuses a path outside the root and deletes nothing", func() {
		outside := GinkgoT().TempDir()
		media := touch(filepath.Join(outside, "Escaped.mkv"))
		sidecar := touch(filepath.Join(outside, "Escaped.nfo"))

		err := RemoveMediaFile(context.Background(), media, root)

		Expect(err).To(MatchError(ErrOutsideRoot))
		Expect(media).To(BeAnExistingFile())
		Expect(sidecar).To(BeAnExistingFile())
	})

	It("refuses a path that climbs out of the root through dot-dot", func() {
		outside := filepath.Join(filepath.Dir(root), "sibling")
		media := touch(filepath.Join(outside, "Escaped.mkv"))
		DeferCleanup(os.RemoveAll, outside)

		err := RemoveMediaFile(
			context.Background(),
			filepath.Join(root, "..", "sibling", "Escaped.mkv"),
			root,
		)

		Expect(err).To(MatchError(ErrOutsideRoot))
		Expect(media).To(BeAnExistingFile())
	})

	It("keeps unrelated files and the folder holding them", func() {
		dir := filepath.Join(root, "Mixed")
		media := touch(filepath.Join(dir, "Movie [1080p].mkv"))
		keep := touch(filepath.Join(dir, "ratings.txt"))

		Expect(RemoveMediaFile(context.Background(), media, root)).To(Succeed())

		Expect(media).NotTo(BeAnExistingFile())
		Expect(keep).To(BeAnExistingFile())
		Expect(dir).To(BeADirectory())
	})

	It("takes the media server's own metadata and the folder with it", func() {
		dir := filepath.Join(root, "Migrated (2008)")
		media := touch(filepath.Join(dir, "Migrated (2008) [].mkv"))
		touch(filepath.Join(dir, "movie.nfo"))
		touch(filepath.Join(dir, "poster.jpg"))

		Expect(RemoveMediaFile(context.Background(), media, root)).To(Succeed())

		Expect(dir).NotTo(BeADirectory())
	})

	It("keeps a season's artwork while another episode is still on disk", func() {
		dir := filepath.Join(root, "Show", "Season 01")
		media := touch(filepath.Join(dir, "Show - S01E01.mkv"))
		other := touch(filepath.Join(dir, "Show - S01E02.mkv"))
		art := touch(filepath.Join(dir, "poster.jpg"))

		Expect(RemoveMediaFile(context.Background(), media, root)).To(Succeed())

		Expect(other).To(BeAnExistingFile())
		Expect(art).To(BeAnExistingFile())
	})

	It("never takes another episode's video, even on a prefix collision", func() {
		dir := filepath.Join(root, "Show", "Season 01")
		media := touch(filepath.Join(dir, "Show - S01E01 - Pilot.mkv"))
		other := touch(filepath.Join(dir, "Show - S01E01 - Pilot - Part 2.mkv"))
		upper := touch(filepath.Join(dir, "Show - S01E01 - Pilot - Part 3.MKV"))

		Expect(RemoveMediaFile(context.Background(), media, root)).To(Succeed())

		Expect(media).NotTo(BeAnExistingFile())
		Expect(other).To(BeAnExistingFile())
		Expect(upper).To(BeAnExistingFile())
	})

	It("prunes season and show folders but stops at the library root", func() {
		dir := filepath.Join(root, "Show", "Season 01")
		media := touch(filepath.Join(dir, "Show - S01E01.mkv"))

		Expect(RemoveMediaFile(context.Background(), media, root)).To(Succeed())

		Expect(filepath.Join(root, "Show")).NotTo(BeADirectory())
		Expect(root).To(BeADirectory())
	})

	It("reports a file that was not there to delete", func() {
		Expect(
			RemoveMediaFile(
				context.Background(),
				filepath.Join(root, "gone", "x.mkv"),
				root,
			),
		).
			To(MatchError(fs.ErrNotExist))
	})

	It("reports a drifted path even though the folder is intact", func() {
		dir := filepath.Join(root, "Drifted (2008)")
		onDisk := touch(filepath.Join(dir, "Drifted (2008) [].mkv"))

		err := RemoveMediaFile(
			context.Background(),
			filepath.Join(dir, "Drifted (2008) [1080p].mkv"),
			root,
		)

		Expect(err).To(MatchError(fs.ErrNotExist))
		Expect(onDisk).To(BeAnExistingFile())
	})
})
