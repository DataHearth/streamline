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

	It("takes the episode's trickplay folder, so the season folder prunes", func() {
		dir := filepath.Join(root, "Bodies (2023)", "Season 01")
		media := touch(filepath.Join(dir, "Bodies (2023) - S01E01 [1080p].mkv"))
		touch(
			filepath.Join(
				dir,
				"Bodies (2023) - S01E01 [1080p].trickplay",
				"320 - 10x10",
				"0.jpg",
			),
		)

		Expect(RemoveMediaFile(context.Background(), media, root)).To(Succeed())

		Expect(filepath.Join(root, "Bodies (2023)")).NotTo(BeADirectory())
		Expect(root).To(BeADirectory())
	})

	It("keeps any other folder named after the episode", func() {
		dir := filepath.Join(root, "Movie (2001)")
		media := touch(filepath.Join(dir, "Movie (2001).mkv"))
		extras := touch(filepath.Join(dir, "Movie (2001)-extras", "behind.mkv"))

		Expect(RemoveMediaFile(context.Background(), media, root)).To(Succeed())

		Expect(extras).To(BeAnExistingFile())
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

	It("takes a copy a killed tag write left behind with the folder", func() {
		dir := filepath.Join(root, "Nirvana", "Nevermind")
		media := touch(filepath.Join(dir, "01 Smells Like Teen Spirit.flac"))
		touch(filepath.Join(dir, ".streamline-tags-123.flac"))

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

var _ = Describe("MoveSidecars", Label("unit", "library"), func() {
	touch := func(path string) {
		GinkgoHelper()
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, []byte("x"), 0o644)).To(Succeed())
	}

	It(
		"carries the trickplay folder and sidecars, so the old season folder prunes",
		func() {
			root := GinkgoT().TempDir()
			oldDir := filepath.Join(root, "Bodies (2023) (2023)", "Season 01")
			newDir := filepath.Join(root, "Bodies (2023)", "Season 01")
			from := filepath.Join(oldDir, "Bodies (2023) (2023) - S01E01.mkv")
			to := filepath.Join(newDir, "Bodies (2023) - S01E01.mkv")
			touch(from)
			touch(
				filepath.Join(
					oldDir,
					"Bodies (2023) (2023) - S01E01.trickplay",
					"320 - 10x10",
					"0.jpg",
				),
			)
			touch(filepath.Join(oldDir, "Bodies (2023) (2023) - S01E01.en.srt"))
			touch(filepath.Join(oldDir, "Bodies (2023) (2023) - S01E01-thumb.jpg"))
			Expect(os.MkdirAll(newDir, 0o755)).To(Succeed())
			Expect(os.Rename(from, to)).To(Succeed())

			MoveSidecars(context.Background(), from, to)
			PruneEmptyDirs(context.Background(), oldDir, root)

			Expect(
				filepath.Join(
					newDir,
					"Bodies (2023) - S01E01.trickplay",
					"320 - 10x10",
					"0.jpg",
				),
			).To(BeAnExistingFile())
			Expect(
				filepath.Join(newDir, "Bodies (2023) - S01E01.en.srt"),
			).To(BeAnExistingFile())
			Expect(
				filepath.Join(newDir, "Bodies (2023) - S01E01-thumb.jpg"),
			).To(BeAnExistingFile())
			Expect(filepath.Join(root, "Bodies (2023) (2023)")).NotTo(BeADirectory())
			Expect(root).To(BeADirectory())
		},
	)

	It(
		"leaves a sidecar whose target is taken and another episode's files alone",
		func() {
			dir := GinkgoT().TempDir()
			from := filepath.Join(dir, "Show - S01E01.mkv")
			to := filepath.Join(dir, "Show - S01E01 - Pilot.mkv")
			touch(to)
			touch(filepath.Join(dir, "Show - S01E01.nfo"))
			touch(filepath.Join(dir, "Show - S01E01 - Pilot.nfo"))
			touch(filepath.Join(dir, "Show - S01E01-E02.mkv"))

			MoveSidecars(context.Background(), from, to)

			Expect(filepath.Join(dir, "Show - S01E01.nfo")).To(BeAnExistingFile())
			Expect(
				filepath.Join(dir, "Show - S01E01-E02.mkv"),
			).To(BeAnExistingFile())
		},
	)
})
