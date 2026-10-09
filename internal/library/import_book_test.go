package library

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("ImportService books", Label("unit", "library"), func() {
	var (
		ctx    context.Context
		svc    *ImportService
		src    string
		ebooks string
		audio  string
		author *ent.Author
		book   *ent.Book
	)

	write := func(dir, name string) string {
		GinkgoHelper()
		p := filepath.Join(dir, name)
		Expect(os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
		Expect(os.WriteFile(p, []byte("bytes of "+name), 0o644)).To(Succeed())
		return p
	}

	BeforeEach(func() {
		ctx = context.Background()
		svc = NewImportService()
		tmp := GinkgoT().TempDir()
		src = filepath.Join(tmp, "dl")
		ebooks = filepath.Join(tmp, "ebooks")
		audio = filepath.Join(tmp, "audio")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		configtest.Setup(map[string]any{
			"library": map[string]any{
				"ebook_path":     ebooks,
				"audiobook_path": audio,
				"import_mode":    "copy",
			},
		})
		author = &ent.Author{Name: "Brandon Sanderson"}
		rel := time.Date(2005, 4, 21, 0, 0, 0, 0, time.UTC)
		book = &ent.Book{ID: 1, Title: "Elantris", ReleaseDate: &rel}
	})

	Describe("ImportEbook", func() {
		profile := config.BookQualityProfileEntry{
			Name: "p",
			Ebook: config.EbookSlot{
				Formats: []string{"EPUB", "MOBI"}, Preferred: "EPUB",
			},
		}

		It("imports the best format and ignores the rest", func() {
			write(src, "elantris.mobi")
			epub := write(src, "elantris.epub")
			write(src, "elantris.nfo")

			got, err := svc.ImportEbook(ctx, src, author, book, profile, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Path).To(Equal(
				filepath.Join(ebooks, "Brandon Sanderson", "Elantris (2005).epub"),
			))
			want, err := os.ReadFile(epub)
			Expect(err).NotTo(HaveOccurred())
			have, err := os.ReadFile(got.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(have).To(Equal(want))
			Expect(got.Parsed.Extension).To(Equal("epub"))
		})

		It("never imports a format outside the profile", func() {
			write(src, "elantris.pdf")
			_, err := svc.ImportEbook(ctx, src, author, book, profile, false)
			Expect(err).To(MatchError(ErrNoMedia))
		})

		It("reports sample-only downloads", func() {
			write(src, "elantris.sample.epub")
			_, err := svc.ImportEbook(ctx, src, author, book, profile, false)
			Expect(err).To(MatchError(ErrSampleOnly))
		})

		It("refuses an existing destination unless replacing", func() {
			write(src, "elantris.epub")
			write(ebooks, "Brandon Sanderson/Elantris (2005).epub")
			_, err := svc.ImportEbook(ctx, src, author, book, profile, false)
			Expect(err).To(MatchError(ErrDestExists))
		})
	})

	Describe("ImportAudiobook", func() {
		It("keeps every file under its original relative path", func() {
			write(src, "Part 01.mp3")
			write(src, "Disc 2/Part 01.mp3")
			write(src, "cover.jpg")

			got, err := svc.ImportAudiobook(ctx, src, author, book, false)
			Expect(err).NotTo(HaveOccurred())
			dir := filepath.Join(audio, "Brandon Sanderson", "Elantris (2005)")
			paths := make([]string, 0, len(got))
			for _, f := range got {
				paths = append(paths, f.Path)
			}
			Expect(paths).To(ConsistOf(
				filepath.Join(dir, "Part 01.mp3"),
				filepath.Join(dir, "Disc 2", "Part 01.mp3"),
			))
		})

		It("refuses a non-empty destination folder unless replacing", func() {
			write(src, "Part 01.mp3")
			write(audio, "Brandon Sanderson/Elantris (2005)/old.mp3")
			_, err := svc.ImportAudiobook(ctx, src, author, book, false)
			Expect(err).To(MatchError(ErrDestExists))

			_, err = svc.ImportAudiobook(ctx, src, author, book, true)
			Expect(err).NotTo(HaveOccurred())
		})

		It("removes what it placed when a later file fails", func() {
			write(src, "Part 01.mp3")
			unreadable := write(src, "Part 02.mp3")
			Expect(os.Chmod(unreadable, 0o000)).To(Succeed())
			DeferCleanup(func() {
				Expect(os.Chmod(unreadable, 0o644)).To(Succeed())
			})

			_, err := svc.ImportAudiobook(ctx, src, author, book, false)
			Expect(err).To(HaveOccurred())
			dir := filepath.Join(audio, "Brandon Sanderson", "Elantris (2005)")
			Expect(dir).NotTo(BeAnExistingFile())

			Expect(os.Chmod(unreadable, 0o644)).To(Succeed())
			_, err = svc.ImportAudiobook(ctx, src, author, book, false)
			Expect(err).NotTo(HaveOccurred())
		})

		It("fails with ErrNoMedia when there is no audio", func() {
			write(src, "notes.txt")
			_, err := svc.ImportAudiobook(ctx, src, author, book, false)
			Expect(err).To(MatchError(ErrNoMedia))
		})
	})
})
