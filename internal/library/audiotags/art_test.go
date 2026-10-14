package audiotags

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

func jpegOf(c color.Color) []byte {
	GinkgoHelper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := range 4 {
		for y := range 4 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	Expect(jpeg.Encode(&b, img, nil)).To(Succeed())
	return b.Bytes()
}

func images(path string) []taglib.ImageDesc {
	GinkgoHelper()
	p, err := taglib.ReadProperties(path)
	Expect(err).NotTo(HaveOccurred())
	return p.Images
}

var _ = Describe("WriteWithArt", Label("unit", "audiotags"), func() {
	tags := Tags{taglib.Title: {"Lithium"}}
	red, blue := jpegOf(
		color.RGBA{R: 255, A: 255},
	), jpegOf(
		color.RGBA{B: 255, A: 255},
	)

	DescribeTable(
		"embeds a front cover in a file that has none, and replaces only its own",
		func(fixture string) {
			path := copyFixture(fixture)
			ours, err := WriteWithArt(
				path,
				tags,
				Policy{},
				Art{Manage: true, Front: red},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(ours).To(Equal(ArtHash(red)))
			data, _, err := Picture(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(data).To(Equal(red))

			ours, err = WriteWithArt(
				path,
				tags,
				Policy{},
				Art{Manage: true, Front: blue, Ours: ours},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(ours).To(Equal(ArtHash(blue)))
			data, _, _ = Picture(path)
			Expect(data).To(Equal(blue))
			Expect(images(path)).To(HaveLen(1))

			ours, err = WriteWithArt(
				path,
				tags,
				Policy{},
				Art{Manage: true, Ours: ours},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(ours).To(BeEmpty())
			Expect(images(path)).To(BeEmpty())
			Expect(
				rawTags(path),
			).To(HaveKeyWithValue(taglib.Title, []string{"Lithium"}))
		},
		Entry("MP3", "testdata/tagged.mp3"),
		Entry("FLAC", "testdata/tagged.flac"),
		Entry("M4A, which types no picture", "testdata/tagged.m4a"),
		Entry("Ogg", "testdata/tagged.ogg"),
		Entry("Opus", "testdata/tagged.opus"),
	)

	It("never replaces or removes a front cover it did not embed", func() {
		path := copyFixture("testdata/withpicture.mp3")
		own, _, err := Picture(path)
		Expect(err).NotTo(HaveOccurred())

		ours, err := WriteWithArt(
			path,
			tags,
			Policy{},
			Art{Manage: true, Front: red},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(ours).To(BeEmpty())
		data, _, _ := Picture(path)
		Expect(data).To(Equal(own))

		_, err = WriteWithArt(
			path,
			tags,
			Policy{},
			Art{Manage: true, Ours: ArtHash(red)},
		)
		Expect(err).NotTo(HaveOccurred())
		data, _, _ = Picture(path)
		Expect(data).To(Equal(own))
	})

	DescribeTable(
		"takes a picture typed Other for the release's own cover, and adds none beside it",
		func(fixture string) {
			path := copyFixture(fixture)
			Expect(
				taglib.WriteImageOptions(path, blue, 0, "Other", "", "image/jpeg"),
			).To(Succeed())

			ours, err := WriteWithArt(
				path,
				tags,
				Policy{},
				Art{Manage: true, Front: red},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(ours).To(BeEmpty())
			Expect(images(path)).To(HaveLen(1))
			data, _, _ := Picture(path)
			Expect(
				data,
			).To(Equal(blue), "the release's art stays the file's picture")
		},
		Entry("MP3", "testdata/tagged.mp3"),
		Entry("FLAC", "testdata/tagged.flac"),
	)

	It(
		"reads the picture it takes for the front cover back as the file's art",
		func() {
			path := copyFixture("testdata/tagged.flac")
			Expect(
				taglib.WriteImageOptions(
					path,
					blue,
					0,
					"Back Cover",
					"",
					"image/jpeg",
				),
			).To(Succeed())
			Expect(
				taglib.WriteImageOptions(path, red, 1, "Other", "", "image/jpeg"),
			).To(Succeed())

			ours, err := WriteWithArt(
				path,
				tags,
				Policy{},
				Art{Manage: true, Front: blue},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(ours).To(BeEmpty())
			data, _, _ := Picture(path)
			Expect(data).To(Equal(red), "the release's cover, never its back")
		},
	)

	It("keeps the other pictures beside the front cover it replaces", func() {
		path := copyFixture("testdata/tagged.flac")
		Expect(
			taglib.WriteImageOptions(path, blue, 0, "Back Cover", "", "image/jpeg"),
		).
			To(Succeed())

		ours, err := WriteWithArt(
			path,
			tags,
			Policy{},
			Art{Manage: true, Front: red},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(ours).To(Equal(ArtHash(red)))
		got := images(path)
		Expect(got).To(HaveLen(2))
		Expect(got[0].Type).To(Equal("Back Cover"))
		Expect(got[1]).To(Equal(taglib.ImageDesc{
			Type:        "Front Cover",
			Description: OwnArtDescription,
			MIMEType:    "image/jpeg",
		}))
	})

	It("leaves the pictures alone when art is not managed", func() {
		path := copyFixture("testdata/tagged.mp3")
		ours, err := WriteWithArt(
			path,
			tags,
			Policy{},
			Art{Front: red, Ours: "kept"},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(ours).To(Equal("kept"))
		Expect(images(path)).To(BeEmpty())
	})
})
