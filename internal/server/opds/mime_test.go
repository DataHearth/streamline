package opds

import (
	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

var _ = g.Describe("ebookContentType", g.Label("unit"), func() {
	g.DescribeTable("maps a format to its media type, whatever the case",
		func(format, want string) {
			Expect(ebookContentType(format)).To(Equal(want))
		},
		g.Entry("epub", "EPUB", "application/epub+zip"),
		g.Entry("lower-case epub", "epub", "application/epub+zip"),
		g.Entry("cbz", "CBZ", "application/vnd.comicbook+zip"),
		g.Entry("cbr", "cbr", "application/vnd.comicbook-rar"),
		g.Entry("anything else", "OTHER", "application/octet-stream"),
	)

	g.It("ranks over the new ladder", func() {
		files := bestOf("CBZ", "EPUB", "PDF")
		Expect(files.Quality).To(Equal("EPUB"))
		Expect(bestOf("CBR", "CBZ").Quality).To(Equal("CBZ"))
	})
})

func bestOf(qualities ...string) *ent.MediaFile {
	files := make([]*ent.MediaFile, len(qualities))
	for i, q := range qualities {
		files[i] = &ent.MediaFile{Quality: q}
	}
	return bestFile(files)
}
