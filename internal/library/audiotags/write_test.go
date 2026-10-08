package audiotags

import (
	"os"
	"path/filepath"

	"github.com/bogem/id3v2/v2"
	"github.com/dhowden/tag"
	"github.com/go-flac/flacvorbis/v2"
	flac "github.com/go-flac/go-flac/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

func rawTags(path string) map[string]any {
	GinkgoHelper()
	f, err := os.Open(path)
	Expect(err).NotTo(HaveOccurred())
	defer f.Close()
	m, err := tag.ReadFrom(f)
	Expect(err).NotTo(HaveOccurred())
	return m.Raw()
}

var _ = Describe("Write", Label("unit", "audiotags"), func() {
	tags := WriteTags{
		Artist: "Nirvana", AlbumArtist: "Nirvana", Album: "Nevermind",
		Title: "Lithium", Track: 5, Disc: 1, Year: 1991,
		MBArtistID: "mbid-1", MBReleaseGroupID: "rg-1", MBRecordingID: "rec-5",
	}

	It("writes ID3v2 tags and MusicBrainz IDs to an MP3", func() {
		path := copyFixture("testdata/tagged.mp3")
		Expect(Write(path, tags)).To(Succeed())

		info, err := Read(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Artist).To(Equal("Nirvana"))
		Expect(info.AlbumArtist).To(Equal("Nirvana"))
		Expect(info.Album).To(Equal("Nevermind"))
		Expect(info.Title).To(Equal("Lithium"))
		Expect(info.Track).To(Equal(uint16(5)))
		Expect(info.Disc).To(Equal(uint8(1)))
		Expect(info.Year).To(Equal(uint16(1991)))

		t, err := id3v2.Open(path, id3v2.Options{Parse: true})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(t.Close)
		got := map[string]string{}
		for _, fr := range t.GetFrames("TXXX") {
			udf, ok := fr.(id3v2.UserDefinedTextFrame)
			Expect(ok).To(BeTrue())
			got[udf.Description] = udf.Value
		}
		Expect(got).To(Equal(map[string]string{
			"MusicBrainz Artist Id":        "mbid-1",
			"MusicBrainz Release Group Id": "rg-1",
			"MusicBrainz Track Id":         "rec-5",
		}))
	})

	It("writes vorbis comments to a FLAC", func() {
		path := copyFixture("testdata/tagged.flac")
		Expect(Write(path, tags)).To(Succeed())

		info, err := Read(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Format).To(Equal("flac"))
		Expect(info.Artist).To(Equal("Nirvana"))
		Expect(info.AlbumArtist).To(Equal("Nirvana"))
		Expect(info.Album).To(Equal("Nevermind"))
		Expect(info.Title).To(Equal("Lithium"))
		Expect(info.Track).To(Equal(uint16(5)))
		Expect(info.Disc).To(Equal(uint8(1)))
		Expect(info.Year).To(Equal(uint16(1991)))

		raw := rawTags(path)
		Expect(raw).To(HaveKeyWithValue("musicbrainz_artistid", "mbid-1"))
		Expect(raw).To(HaveKeyWithValue("musicbrainz_releasegroupid", "rg-1"))
		Expect(raw).To(HaveKeyWithValue("musicbrainz_trackid", "rec-5"))
	})

	It("replaces rather than appends on a second write", func() {
		path := copyFixture("testdata/tagged.flac")
		first := tags
		first.Title = "Breed"
		first.MBArtistID = "mbid-old"
		Expect(Write(path, first)).To(Succeed())
		Expect(Write(path, tags)).To(Succeed())

		f, err := flac.ParseFile(path)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(f.Close)
		var cmt *flacvorbis.MetaDataBlockVorbisComment
		for _, blk := range f.Meta {
			if blk.Type == flac.VorbisComment {
				cmt, err = flacvorbis.ParseFromMetaDataBlock(*blk)
				Expect(err).NotTo(HaveOccurred())
			}
		}
		Expect(cmt).NotTo(BeNil())

		title, err := cmt.Get("TITLE")
		Expect(err).NotTo(HaveOccurred())
		Expect(title).To(Equal([]string{"Lithium"}))
		artist, err := cmt.Get("MUSICBRAINZ_ARTISTID")
		Expect(err).NotTo(HaveOccurred())
		Expect(artist).To(Equal([]string{"mbid-1"}))
	})

	It("returns ErrUnsupportedFormat for other extensions", func() {
		Expect(Write("x.opus", WriteTags{})).To(MatchError(ErrUnsupportedFormat))
	})
})
