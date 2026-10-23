package restapi

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe(
	"bookItemFormat",
	Label("unit", "server", "imports", "books"),
	func() {
		DescribeTable("reads the format and source path off an item's files",
			func(slot string, paths []string, wantFormat, wantSource string) {
				format, source := bookItemFormat(slot, paths)
				Expect(format).To(Equal(wantFormat))
				Expect(source).To(Equal(wantSource))
			},
			Entry("an ebook takes its best format by the ladder", "ebook",
				[]string{"/b/a.mobi", "/b/a.epub", "/b/a.pdf"}, "EPUB", "/b/a.epub"),
			Entry("an ebook off the ladder ranks last", "ebook",
				[]string{"/b/a.lit", "/b/a.pdf"}, "PDF", "/b/a.pdf"),
			Entry("an audiobook is its dominant extension and folder", "audiobook",
				[]string{"/a/Book/01.mp3", "/a/Book/02.mp3", "/a/Book/cover.m4b"},
				"MP3", "/a/Book"),
			Entry(
				"an audiobook split over disc folders is the common parent",
				"audiobook",
				[]string{
					"/a/Book/CD1/01.mp3",
					"/a/Book/CD2/01.mp3",
				},
				"MP3",
				"/a/Book",
			),
			Entry("an item with no files has neither", "ebook", []string{}, "", ""),
		)
	},
)
