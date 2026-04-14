package library

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
)

var _ = Describe("ImportService", Label("unit", "library"), func() {
	Describe("ImportMovie", func() {
		var (
			ctx         context.Context
			tmpDir      string
			libraryDir  string
			downloadDir string
		)

		BeforeEach(func() {
			ctx = context.Background()
			tmpDir = GinkgoT().TempDir()
			libraryDir = filepath.Join(tmpDir, "library", "movies")
			downloadDir = filepath.Join(tmpDir, "downloads")
			Expect(os.MkdirAll(downloadDir, 0o755)).To(Succeed())
		})

		Context("with hardlink mode", func() {
			It("hardlinks a single-file srcDir into the library", func() {
				srcFile := filepath.Join(
					downloadDir,
					"Interstellar.2014.1080p.BluRay.x264-SPARKS.mkv",
				)
				writeSizedFile(downloadDir, filepath.Base(srcFile), 60<<20)

				cfg := &config.LibraryConfig{
					MoviePath:   libraryDir,
					MovieNaming: "{title} ({year})/{title} ({year}) - {quality}.{ext}",
					ImportMode:  "hardlink",
				}
				svc := NewImportService(cfg)
				m := &ent.Movie{
					ID:     1,
					Title:  "Interstellar",
					Year:   2014,
					TmdbID: 157336,
				}

				got, err := svc.ImportMovie(ctx, srcFile, m, "")
				Expect(err).NotTo(HaveOccurred())

				expectedPath := filepath.Join(
					libraryDir,
					"Interstellar (2014)",
					"Interstellar (2014) - 1080p.mkv",
				)
				Expect(got.Path).To(Equal(expectedPath))
				Expect(got.Size).To(BeNumerically(">", 0))
				Expect(got.Parsed.Group).To(Equal("SPARKS"))
				Expect(expectedPath).To(BeAnExistingFile())

				srcInfo, err := os.Stat(srcFile)
				Expect(err).NotTo(HaveOccurred())
				dstInfo, err := os.Stat(expectedPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(os.SameFile(srcInfo, dstInfo)).To(BeTrue())
			})
		})

		Context("with copy mode", func() {
			It("copies media file from srcDir to library path", func() {
				writeSizedFile(
					downloadDir,
					"Movie.2020.720p.WEB-DL.x264.mkv",
					60<<20,
				)

				cfg := &config.LibraryConfig{
					MoviePath:   libraryDir,
					MovieNaming: "{title} ({year})/{title} ({year}) - {quality}.{ext}",
					ImportMode:  "copy",
				}
				svc := NewImportService(cfg)
				m := &ent.Movie{ID: 1, Title: "Movie", Year: 2020, TmdbID: 999}

				got, err := svc.ImportMovie(ctx, downloadDir, m, "")
				Expect(err).NotTo(HaveOccurred())

				expectedPath := filepath.Join(
					libraryDir,
					"Movie (2020)",
					"Movie (2020) - 720p.mkv",
				)
				Expect(got.Path).To(Equal(expectedPath))
				Expect(expectedPath).To(BeAnExistingFile())

				srcInfo, err := os.Stat(
					filepath.Join(downloadDir, "Movie.2020.720p.WEB-DL.x264.mkv"),
				)
				Expect(err).NotTo(HaveOccurred())
				dstInfo, err := os.Stat(expectedPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(os.SameFile(srcInfo, dstInfo)).To(BeFalse())
			})
		})

		It(
			"returns ErrDestExists when a differing file blocks the destination",
			func() {
				writeSizedFile(downloadDir, "Flick.2024.1080p.mkv", 60<<20)
				cfg := &config.LibraryConfig{
					MoviePath:   libraryDir,
					MovieNaming: "{title} ({year})/{title}.{ext}",
					ImportMode:  "copy",
				}
				destDir := filepath.Join(libraryDir, "Flick (2024)")
				Expect(os.MkdirAll(destDir, 0o755)).To(Succeed())
				Expect(
					os.WriteFile(
						filepath.Join(destDir, "Flick.mkv"),
						[]byte("pre-existing"),
						0o644,
					),
				).To(Succeed())

				svc := NewImportService(cfg)
				m := &ent.Movie{ID: 1, Title: "Flick", Year: 2024, TmdbID: 1}
				_, err := svc.ImportMovie(ctx, downloadDir, m, "")
				Expect(err).To(MatchError(ErrDestExists))
			},
		)

		It(
			"is idempotent when dest already points at the same inode (hardlink mode)",
			func() {
				src := writeSizedFile(downloadDir, "Flick.2024.1080p.mkv", 60<<20)
				cfg := &config.LibraryConfig{
					MoviePath:   libraryDir,
					MovieNaming: "{title} ({year})/{title}.{ext}",
					ImportMode:  "hardlink",
				}
				destDir := filepath.Join(libraryDir, "Flick (2024)")
				Expect(os.MkdirAll(destDir, 0o755)).To(Succeed())
				Expect(
					os.Link(src, filepath.Join(destDir, "Flick.mkv")),
				).To(Succeed())

				svc := NewImportService(cfg)
				m := &ent.Movie{ID: 1, Title: "Flick", Year: 2024, TmdbID: 1}
				got, err := svc.ImportMovie(ctx, downloadDir, m, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(got.Path).To(Equal(filepath.Join(destDir, "Flick.mkv")))
			},
		)

		It("returns ErrUnsafePath when template output escapes MoviePath", func() {
			writeSizedFile(downloadDir, "Flick.2024.1080p.mkv", 60<<20)
			cfg := &config.LibraryConfig{
				MoviePath:   libraryDir,
				MovieNaming: "../escape/{title}.{ext}",
				ImportMode:  "copy",
			}
			svc := NewImportService(cfg)
			m := &ent.Movie{ID: 1, Title: "Flick", Year: 2024, TmdbID: 1}
			_, err := svc.ImportMovie(ctx, downloadDir, m, "")
			Expect(err).To(MatchError(ErrUnsafePath))
		})
	})
})
