package upload_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/upload"
)

type testPart struct {
	name string
	body string
}

func escapeQuotes(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

func buildBody(parts ...testPart) (*bytes.Buffer, string) {
	GinkgoHelper()
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		h.Set(
			"Content-Disposition",
			fmt.Sprintf(
				`form-data; name="file"; filename="%s"`,
				escapeQuotes(p.name),
			),
		)
		h.Set("Content-Type", "application/octet-stream")
		pw, err := w.CreatePart(h)
		Expect(err).NotTo(HaveOccurred())
		_, err = pw.Write([]byte(p.body))
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(w.Close()).To(Succeed())
	return buf, w.Boundary()
}

func entries(dir string) []string {
	GinkgoHelper()
	es, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	Expect(err).NotTo(HaveOccurred())
	names := make([]string, 0, len(es))
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}

// fixture is a real audio file: the receiver refuses what holds no audio.
func fixture(name string) string {
	GinkgoHelper()
	data, err := os.ReadFile(
		filepath.Join("..", "library", "audiotags", "testdata", name),
	)
	Expect(err).NotTo(HaveOccurred())
	return string(data)
}

type errReader struct{}

func (errReader) Read(
	[]byte,
) (int, error) {
	return 0, errors.New("connection reset")
}

type panicReader struct{}

func (panicReader) Read(
	[]byte,
) (int, error) {
	panic("body read before the roots check")
}

var _ = Describe("Receive", Label("unit"), func() {
	var downloads string

	BeforeEach(func() {
		downloads = GinkgoT().TempDir()
		configtest.Setup(map[string]any{
			"library": map[string]any{"download_path": downloads},
		})
	})

	receive := func(kind upload.Kind, parts ...testPart) (*upload.Staged, error) {
		body, boundary := buildBody(parts...)
		return upload.Receive(
			context.Background(),
			multipart.NewReader(body, boundary),
			kind,
		)
	}

	It("keeps the kind's media and cue sheets and drops the rest", func() {
		audio := fixture("tagged.flac")
		staged, err := receive(upload.KindAlbum,
			testPart{"CD1/01.flac", audio},
			testPart{"album.cue", "cue"},
			testPart{"cover.jpg", "img"},
			testPart{"info.nfo", "nfo"},
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(staged.Discard)

		Expect(
			staged.Files,
		).To(Equal([]upload.File{{Path: "CD1/01.flac", Size: int64(len(audio))}, {Path: "album.cue", Size: 3}}))
		info, err := os.Stat(staged.Dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o700)))
		Expect(entries(upload.Root())).To(ConsistOf(filepath.Base(staged.Dir)))
		Expect(filepath.Join(staged.Dir, "cover.jpg")).NotTo(BeAnExistingFile())
	})

	DescribeTable("rejects unsafe names",
		func(name string) {
			_, err := receive(
				upload.KindAlbum,
				testPart{"ok.flac", "a"},
				testPart{name, "x"},
			)
			Expect(err).To(MatchError(upload.ErrRejected))
			Expect(entries(upload.Root())).To(BeEmpty())
		},
		Entry("parent", "../x.flac"),
		Entry("absolute", "/x.flac"),
		Entry("embedded parent", "a/../../x.flac"),
		Entry("empty segment", "a//b.flac"),
		Entry("drive letter", `C:\x.flac`),
		Entry("NUL", "a\x00.flac"),
	)

	It("refuses media of another kind, naming the file", func() {
		_, err := receive(
			upload.KindAlbum,
			testPart{"01.flac", "a"},
			testPart{"film.mkv", "v"},
		)
		Expect(err).To(MatchError(upload.ErrRejected))
		Expect(err.Error()).To(ContainSubstring("film.mkv"))

		_, err = receive(
			upload.KindEbook,
			testPart{"b.epub", "a"},
			testPart{"track.mp3", "v"},
		)
		Expect(err).To(MatchError(upload.ErrRejected))
		Expect(err.Error()).To(ContainSubstring("track.mp3"))
		Expect(entries(upload.Root())).To(BeEmpty())
	})

	It("rejects an upload with no media", func() {
		_, err := receive(
			upload.KindAlbum,
			testPart{"info.nfo", "x"},
			testPart{"a.cue", "x"},
		)
		Expect(err).To(MatchError(upload.ErrRejected))
		Expect(entries(upload.Root())).To(BeEmpty())
	})

	It("cleans up when the body fails mid-part", func() {
		body, boundary := buildBody(testPart{"01.flac", strings.Repeat("a", 4096)})
		cut := io.MultiReader(io.LimitReader(body, 300), errReader{})
		_, err := upload.Receive(
			context.Background(),
			multipart.NewReader(cut, boundary),
			upload.KindAlbum,
		)
		Expect(err).To(HaveOccurred())
		Expect(err).NotTo(MatchError(upload.ErrRejected))
		Expect(entries(upload.Root())).To(BeEmpty())
	})

	It(
		"takes a PDF as a booklet on music and as the book on an ebook upload",
		func() {
			Expect(
				upload.Classify(upload.KindAlbum, "Booklet.pdf"),
			).To(Equal(upload.Ignore))
			Expect(
				upload.Classify(upload.KindArtist, "A/Booklet.pdf"),
			).To(Equal(upload.Ignore))
			Expect(
				upload.Classify(upload.KindEbook, "book.pdf"),
			).To(Equal(upload.Keep))
			Expect(
				upload.Classify(upload.KindAudiobook, "book.pdf"),
			).To(Equal(upload.Refuse))
		},
	)

	It("checks names the way the receiver does, before any byte", func() {
		Expect(
			upload.CheckNames(upload.KindAlbum, []string{"CD1/01.flac", "x.jpg"}),
		).To(Succeed())
		err := upload.CheckNames(upload.KindAlbum, []string{"bonus.mkv"})
		Expect(err).To(MatchError(upload.ErrRejected))
		Expect(err).To(MatchError(ContainSubstring("bonus.mkv")))
		Expect(upload.CheckNames(upload.KindAlbum, []string{"../x.flac"})).
			To(MatchError(upload.ErrRejected))
	})

	It("refuses a body that ends without its closing boundary", func() {
		body := "--b\r\n" +
			"Content-Disposition: form-data; name=\"file\"; filename=\"01.flac\"\r\n\r\n" +
			"audio\r\n"
		_, err := upload.Receive(
			context.Background(),
			multipart.NewReader(strings.NewReader(body), "b"),
			upload.KindAlbum,
		)
		Expect(err).To(MatchError(upload.ErrRejected))
		Expect(err).To(MatchError(ContainSubstring("cut off")))
		Expect(entries(upload.Root())).To(BeEmpty())
	})

	DescribeTable("refuses names the filesystem cannot hold, without a server error",
		func(names ...string) {
			var buf bytes.Buffer
			mw := multipart.NewWriter(&buf)
			for _, n := range names {
				h := textproto.MIMEHeader{}
				h.Set("Content-Disposition",
					`form-data; name="file"; filename="`+n+`"`)
				w, err := mw.CreatePart(h)
				Expect(err).NotTo(HaveOccurred())
				_, err = w.Write([]byte("audio"))
				Expect(err).NotTo(HaveOccurred())
			}
			Expect(mw.Close()).To(Succeed())
			_, err := upload.Receive(
				context.Background(),
				multipart.NewReader(&buf, mw.Boundary()),
				upload.KindAlbum,
			)
			Expect(err).To(MatchError(upload.ErrRejected))
			Expect(entries(upload.Root())).To(BeEmpty())
		},
		Entry("a segment over 255 bytes", strings.Repeat("a", 300)+".flac"),
		Entry("a file where a folder must go", "a.flac", "a.flac/b.flac"),
		Entry("a folder where a file must go", "a.flac/b.flac", "a.flac"),
	)

	It("rejects an over-long name from a preview too", func() {
		Expect(
			upload.CheckNames(
				upload.KindAlbum,
				[]string{strings.Repeat("a", 300) + ".flac"},
			),
		).
			To(MatchError(upload.ErrRejected))
	})

	DescribeTable(
		"refuses a file named as audio that holds none, naming it",
		func(kind upload.Kind, name, body string) {
			_, err := receive(kind, testPart{name, body})
			Expect(err).To(MatchError(upload.ErrRejected))
			Expect(err).To(MatchError(ContainSubstring(name)))
			Expect(entries(upload.Root())).To(BeEmpty())
		},
		Entry("text on an album", upload.KindAlbum, "01.flac", "not audio"),
		Entry("an empty file on a track", upload.KindTrack, "01.flac", ""),
		Entry(
			"zeros in a discography",
			upload.KindArtist,
			"A/01.mp3",
			strings.Repeat("\x00", 4096),
		),
		Entry("text in an audiobook", upload.KindAudiobook, "01.m4b", "not audio"),
	)

	It("names every unreadable file at once, and only those", func() {
		_, err := receive(upload.KindAlbum,
			testPart{"good.flac", fixture("tagged.flac")},
			testPart{"bad1.flac", "x"},
			testPart{"bad2.mp3", "y"},
		)
		Expect(err).To(MatchError(upload.ErrRejected))
		Expect(err).To(MatchError(And(
			ContainSubstring("bad1.flac"), ContainSubstring("bad2.mp3"),
		)))
		Expect(err.Error()).NotTo(ContainSubstring("good.flac"))
	})

	DescribeTable("takes every real audio format",
		func(name string) {
			s, err := receive(
				upload.KindAlbum,
				testPart{"01" + filepath.Ext(name), fixture(name)},
			)
			Expect(err).NotTo(HaveOccurred())
			s.Done()
		},
		Entry("flac", "tagged.flac"),
		Entry("mp3", "tagged.mp3"),
		Entry("m4a", "tagged.m4a"),
		Entry("ogg", "tagged.ogg"),
		Entry("opus", "tagged.opus"),
	)

	It("does not read an ebook as audio", func() {
		s, err := receive(upload.KindEbook, testPart{"book.epub", "zip bytes"})
		Expect(err).NotTo(HaveOccurred())
		s.Done()
	})

	It("skips the space bound when the check is turned off", func() {
		configtest.Setup(map[string]any{
			"library":  map[string]any{"download_path": downloads},
			"download": map[string]any{"space_check": false},
		})
		Expect(upload.CheckSpace(1 << 62)).To(Succeed())
	})

	It("still refuses for the roots with the space check off", func() {
		configtest.Setup(map[string]any{
			"library": map[string]any{
				"download_path":          downloads,
				"allowed_download_roots": []string{"/elsewhere"},
			},
			"download": map[string]any{"space_check": false},
		})
		Expect(upload.CheckSpace(1)).To(MatchError(upload.ErrRejected))
	})

	It("refuses a declared length that cannot fit", func() {
		Expect(upload.CheckSpace(1 << 62)).To(MatchError(upload.ErrNoSpace))
		Expect(upload.CheckSpace(-1)).To(Succeed())
		Expect(upload.CheckSpace(1)).To(Succeed())
	})

	It(
		"refuses when no allowed_download_roots entry covers the staging root",
		func() {
			configtest.Setup(map[string]any{
				"library": map[string]any{
					"download_path":          downloads,
					"allowed_download_roots": []string{"/elsewhere"},
				},
			})
			_, err := upload.Receive(
				context.Background(),
				multipart.NewReader(panicReader{}, "x"),
				upload.KindAlbum,
			)
			Expect(err).To(MatchError(upload.ErrRejected))
			Expect(err.Error()).To(ContainSubstring("allowed_download_roots"))
		},
	)
})
