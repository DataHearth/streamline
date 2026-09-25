package bittorrent

import (
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
})
