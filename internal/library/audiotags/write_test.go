package audiotags

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

func copyFixture(src string) string {
	GinkgoHelper()
	data, err := os.ReadFile(src)
	Expect(err).NotTo(HaveOccurred())
	dst := filepath.Join(GinkgoT().TempDir(), "file"+filepath.Ext(src))
	//nolint:gosec // dst is a fresh temp dir joined with a fixed name
	Expect(os.WriteFile(dst, data, 0o600)).To(Succeed())
	return dst
}

func rawTags(path string) map[string][]string {
	GinkgoHelper()
	tags, err := taglib.ReadTags(path)
	Expect(err).NotTo(HaveOccurred())
	return tags
}

func dirEntries(path string) []string {
	GinkgoHelper()
	entries, err := os.ReadDir(filepath.Dir(path))
	Expect(err).NotTo(HaveOccurred())
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

var _ = Describe("Write", Label("unit", "audiotags"), func() {
	full := Tags{
		taglib.Artist:                    {"Nirvana"},
		taglib.Artists:                   {"Nirvana"},
		taglib.AlbumArtist:               {"Nirvana"},
		taglib.Album:                     {"Nevermind"},
		taglib.Title:                     {"Lithium"},
		taglib.TrackNumber:               {"5"},
		TrackTotal:                       {"12"},
		taglib.DiscNumber:                {"1"},
		DiscTotal:                        {"1"},
		taglib.Date:                      {"1991-09-24"},
		taglib.OriginalDate:              {"1991-09-24"},
		taglib.Genre:                     {"Grunge", "Alternative Rock"},
		taglib.Label:                     {"DGC"},
		taglib.MusicBrainzArtistID:       {"mbid-1"},
		taglib.MusicBrainzAlbumArtistID:  {"mbid-1"},
		taglib.MusicBrainzAlbumID:        {"rel-1"},
		taglib.MusicBrainzReleaseGroupID: {"rg-1"},
		taglib.MusicBrainzTrackID:        {"rec-5"},
	}

	DescribeTable("writes every managed key and reads back the basics",
		func(fixture string) {
			path := copyFixture(fixture)
			Expect(Write(path, full, DefaultStrip)).To(Succeed())

			info, err := Read(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Artist).To(Equal("Nirvana"))
			Expect(info.AlbumArtist).To(Equal("Nirvana"))
			Expect(info.Album).To(Equal("Nevermind"))
			Expect(info.Title).To(Equal("Lithium"))
			Expect(info.Track).To(Equal(uint16(5)))
			Expect(info.Disc).To(Equal(uint8(1)))
			Expect(info.Year).To(Equal(uint16(1991)))

			raw := rawTags(path)
			Expect(
				raw,
			).To(HaveKeyWithValue(taglib.Genre, []string{"Grunge", "Alternative Rock"}))
			Expect(
				raw,
			).To(HaveKeyWithValue(taglib.MusicBrainzAlbumID, []string{"rel-1"}))
			Expect(
				raw,
			).To(HaveKeyWithValue(taglib.MusicBrainzReleaseGroupID, []string{"rg-1"}))
			Expect(
				raw,
			).To(HaveKeyWithValue(taglib.MusicBrainzTrackID, []string{"rec-5"}))
			Expect(raw).To(HaveKeyWithValue(taglib.Label, []string{"DGC"}))
		},
		Entry("MP3", "testdata/tagged.mp3"),
		Entry("FLAC", "testdata/tagged.flac"),
		Entry("M4A", "testdata/tagged.m4a"),
		Entry("Ogg Vorbis", "testdata/tagged.ogg"),
		Entry("Opus", "testdata/tagged.opus"),
	)

	It(
		"folds the totals into the number for ID3 and keeps them apart for Vorbis comments",
		func() {
			mp3 := copyFixture("testdata/tagged.mp3")
			Expect(Write(mp3, full, nil)).To(Succeed())
			raw := rawTags(mp3)
			Expect(raw).To(HaveKeyWithValue(taglib.TrackNumber, []string{"5/12"}))
			Expect(raw).To(HaveKeyWithValue(taglib.DiscNumber, []string{"1/1"}))
			Expect(raw).NotTo(HaveKey(TrackTotal))

			flac := copyFixture("testdata/tagged.flac")
			Expect(Write(flac, full, nil)).To(Succeed())
			raw = rawTags(flac)
			Expect(raw).To(HaveKeyWithValue(taglib.TrackNumber, []string{"5"}))
			Expect(raw).To(HaveKeyWithValue(TrackTotal, []string{"12"}))
		},
	)

	DescribeTable("keeps the keys it does not manage",
		func(fixture string) {
			path := copyFixture(fixture)
			Expect(taglib.WriteTags(path, map[string][]string{
				"REPLAYGAIN_TRACK_GAIN": {"-7.20 dB"},
				taglib.Lyrics:           {"Light up, light up"},
			}, 0)).To(Succeed())

			Expect(Write(path, full, DefaultStrip)).To(Succeed())

			raw := rawTags(path)
			Expect(
				raw,
			).To(HaveKeyWithValue("REPLAYGAIN_TRACK_GAIN", []string{"-7.20 dB"}))
			Expect(
				raw,
			).To(HaveKeyWithValue(taglib.Lyrics, []string{"Light up, light up"}))
		},
		Entry("MP3", "testdata/tagged.mp3"),
		Entry("FLAC", "testdata/tagged.flac"),
	)

	It("keeps the embedded picture", func() {
		path := copyFixture("testdata/withpicture.mp3")
		Expect(Write(path, full, DefaultStrip)).To(Succeed())

		data, mime, err := Picture(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(mime).To(Equal("image/jpeg"))
		Expect(data[:2]).To(Equal([]byte{0xff, 0xd8}))
	})

	It(
		"deletes the stripped keys, by name and by prefix, unless they are managed",
		func() {
			path := copyFixture("testdata/tagged.flac")
			Expect(taglib.WriteTags(path, map[string][]string{
				taglib.Comment:   {"Ripped by someone, seeded at tracker.example"},
				taglib.EncodedBy: {"someone"},
				"RIPPER_NOTE":    {"EAC secure mode"},
				"RIPPER_LOG":     {"ok"},
			}, 0)).To(Succeed())

			Expect(Write(path, Tags{
				taglib.Title:     {"Lithium"},
				taglib.EncodedBy: {"kept on purpose"},
			}, []string{taglib.Comment, taglib.EncodedBy, "RIPPER_*"})).To(Succeed())

			raw := rawTags(path)
			Expect(raw).NotTo(HaveKey(taglib.Comment))
			Expect(raw).NotTo(HaveKey("RIPPER_NOTE"))
			Expect(raw).NotTo(HaveKey("RIPPER_LOG"))
			Expect(
				raw,
			).To(HaveKeyWithValue(taglib.EncodedBy, []string{"kept on purpose"}))
		},
	)

	It("deletes a managed key passed with no value", func() {
		path := copyFixture("testdata/tagged.flac")
		Expect(Write(path, full, nil)).To(Succeed())
		Expect(
			Write(path, Tags{taglib.Label: nil, taglib.Genre: {" "}}, nil),
		).To(Succeed())

		raw := rawTags(path)
		Expect(raw).NotTo(HaveKey(taglib.Label))
		Expect(raw).NotTo(HaveKey(taglib.Genre))
		Expect(raw).To(HaveKeyWithValue(taglib.Title, []string{"Lithium"}))
	})

	It("replaces rather than appends on a second write", func() {
		path := copyFixture("testdata/tagged.flac")
		first := Tags{
			taglib.Title:               {"Breed"},
			taglib.MusicBrainzArtistID: {"mbid-old"},
		}
		Expect(Write(path, first, nil)).To(Succeed())
		Expect(Write(path, full, nil)).To(Succeed())

		raw := rawTags(path)
		Expect(raw).To(HaveKeyWithValue(taglib.Title, []string{"Lithium"}))
		Expect(
			raw,
		).To(HaveKeyWithValue(taglib.MusicBrainzArtistID, []string{"mbid-1"}))
	})

	It("never writes through a hard link", func() {
		path := copyFixture("testdata/tagged.flac")
		seeding := filepath.Join(GinkgoT().TempDir(), "seeding.flac")
		Expect(os.Link(path, seeding)).To(Succeed())

		Expect(Write(path, full, nil)).To(Succeed())

		Expect(rawTags(path)).To(HaveKeyWithValue(taglib.Title, []string{"Lithium"}))
		Expect(
			rawTags(seeding),
		).To(HaveKeyWithValue(taglib.Title, []string{"Smells Like Teen Spirit"}))
	})

	It("keeps the permission bits and leaves no copy behind", func() {
		path := copyFixture("testdata/tagged.mp3")
		Expect(os.Chmod(path, 0o640)).To(Succeed())

		Expect(Write(path, full, nil)).To(Succeed())

		st, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(st.Mode().Perm()).To(Equal(os.FileMode(0o640)))
		Expect(dirEntries(path)).To(Equal([]string{"file.mp3"}))
	})

	It("leaves the original and no copy when the file is not audio", func() {
		path := filepath.Join(GinkgoT().TempDir(), "broken.flac")
		Expect(os.WriteFile(path, []byte("not audio"), 0o600)).To(Succeed())

		Expect(Write(path, full, nil)).NotTo(Succeed())

		data, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal("not audio"))
		Expect(dirEntries(path)).To(Equal([]string{"broken.flac"}))
	})

	It("returns ErrUnsupportedFormat for other extensions", func() {
		Expect(Write("x.wav", Tags{}, nil)).To(MatchError(ErrUnsupportedFormat))
	})
})
