package bittorrent

import (
	"os"
	"path/filepath"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("contentPaths", Label("unit", "bittorrent"), func() {
	const base = "/downloads"

	// place runs the two storage hooks the way OpenTorrent does, one file.
	place := func(p *contentPaths, info *metainfo.Info, ih metainfo.Hash, file []string) string {
		GinkgoHelper()
		Expect(p.torrentDir(base, info, ih)).To(Equal(base))
		return p.filePath(storage.FilePathMakerOpts{
			Info: info,
			File: &metainfo.FileInfo{Path: file},
		})
	}

	hashA := metainfo.Hash{1}
	hashB := metainfo.Hash{2}

	It("places a torrent under its own name", func() {
		p := newContentPaths()
		Expect(
			place(p, &metainfo.Info{Name: "Show.S01"}, hashA, []string{"e01.mkv"}),
		).
			To(Equal("Show.S01/e01.mkv"))
	})

	DescribeTable("refuses a name that would land on something else",
		func(name string) {
			p := newContentPaths()
			Expect(place(p, &metainfo.Info{Name: name}, hashA, nil)).To(Equal(".."))
		},
		Entry("empty: the download dir itself", ""),
		Entry("dot", "."),
		Entry("dot-dot", ".."),
		Entry("the engine's state dir", sessionDirName),
		Entry("a nested path", "a/b"),
	)

	It("refuses a file path that climbs out of the torrent's directory", func() {
		p := newContentPaths()
		Expect(
			place(
				p,
				&metainfo.Info{Name: "Mine"},
				hashA,
				[]string{"..", "Theirs", "x.mkv"},
			),
		).
			To(Equal(".."))
	})

	It("refuses another torrent's live name until it is released", func() {
		p := newContentPaths()
		Expect(
			place(p, &metainfo.Info{Name: "Release"}, hashA, nil),
		).To(Equal("Release"))
		Expect(place(p, &metainfo.Info{Name: "Release"}, hashB, nil)).To(Equal(".."))
		// The owner reopening its own storage is not a collision.
		Expect(
			place(p, &metainfo.Info{Name: "Release"}, hashA, nil),
		).To(Equal("Release"))

		p.release(hashA)

		Expect(
			place(p, &metainfo.Info{Name: "Release"}, hashB, nil),
		).To(Equal("Release"))
	})

	Context("with data already on disk under the name", func() {
		var dir string
		single := &metainfo.Info{Name: "Film.mkv", Length: 4}

		BeforeEach(func() { dir = GinkgoT().TempDir() })

		write := func(name string, size int) {
			GinkgoHelper()
			Expect(
				os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o644),
			).
				To(Succeed())
		}

		It("refuses data of another size", func() {
			write("Film.mkv", 9)
			Expect(newContentPaths().admits(dir, single, hashA)).To(BeFalse())
		})

		It("refuses a partial download it does not own", func() {
			write("Film.mkv.part", 2)
			Expect(newContentPaths().admits(dir, single, hashA)).To(BeFalse())
		})

		It("resumes data that is already its own size", func() {
			write("Film.mkv", 4)
			Expect(newContentPaths().admits(dir, single, hashA)).To(BeTrue())
		})

		It("trusts a restored torrent with its own partial data", func() {
			write("Film.mkv.part", 2)
			p := newContentPaths()
			p.trust(hashA)
			Expect(p.admits(dir, single, hashA)).To(BeTrue())
		})
	})
})

var _ = Describe("removeTorrentData", Label("unit", "bittorrent"), func() {
	var base string

	seed := func(rel string) string {
		GinkgoHelper()
		p := filepath.Join(base, filepath.FromSlash(rel))
		Expect(os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
		Expect(os.WriteFile(p, []byte("x"), 0o644)).To(Succeed())
		return p
	}

	BeforeEach(func() { base = GinkgoT().TempDir() })

	omens := &metainfo.Info{
		Name: "Lamb Of God",
		Files: []metainfo.FileInfo{
			{Path: []string{"2022 - Omens", "Lamb Of God - Omens.flac"}, Length: 1},
			{Path: []string{"2022 - Omens", "Scans", "Inside.png"}, Length: 1},
		},
	}

	It("keeps what another torrent left under the same name", func() {
		seed("Lamb Of God/2022 - Omens/Lamb Of God - Omens.flac")
		seed("Lamb Of God/2022 - Omens/Scans/Inside.png")
		resolution := seed(
			"Lamb Of God/2012 - Resolution/01. Straight For The Sun.flac",
		)

		Expect(removeTorrentData(base, omens)).To(Succeed())

		Expect(
			filepath.Join(base, "Lamb Of God", "2022 - Omens"),
		).NotTo(BeAnExistingFile())
		Expect(resolution).To(BeAnExistingFile())
	})

	It("removes the torrent's folder once nothing else is in it", func() {
		seed("Lamb Of God/2022 - Omens/Lamb Of God - Omens.flac")
		// Partly downloaded: anacrolix keeps the incomplete file at .part.
		seed("Lamb Of God/2022 - Omens/Scans/Inside.png.part")

		Expect(removeTorrentData(base, omens)).To(Succeed())

		Expect(filepath.Join(base, "Lamb Of God")).NotTo(BeAnExistingFile())
		Expect(base).To(BeADirectory())
	})

	It("removes a single-file torrent and its .part, and nothing beside it", func() {
		info := &metainfo.Info{Name: "payload.bin", Length: 1}
		seed("payload.bin")
		seed("payload.bin.part")
		other := seed("other.bin")

		Expect(removeTorrentData(base, info)).To(Succeed())

		Expect(filepath.Join(base, "payload.bin")).NotTo(BeAnExistingFile())
		Expect(filepath.Join(base, "payload.bin.part")).NotTo(BeAnExistingFile())
		Expect(other).To(BeAnExistingFile())
	})

	It("leaves a folder standing where a single-file torrent's name points", func() {
		info := &metainfo.Info{Name: "Lamb Of God", Length: 1}
		resolution := seed("Lamb Of God/2012 - Resolution/01.flac")

		Expect(removeTorrentData(base, info)).To(Succeed())

		Expect(resolution).To(BeAnExistingFile())
	})
})
