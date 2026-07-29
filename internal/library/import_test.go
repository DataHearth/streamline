package library

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

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

	Describe("ImportEpisode", func() {
		var (
			ctx         context.Context
			seriesDir   string
			downloadDir string
			show        *ent.TVShow
			ep          *ent.Episode
		)

		BeforeEach(func() {
			ctx = context.Background()
			tmpDir := GinkgoT().TempDir()
			seriesDir = filepath.Join(tmpDir, "library", "tv")
			downloadDir = filepath.Join(tmpDir, "downloads")
			Expect(os.MkdirAll(downloadDir, 0o755)).To(Succeed())
			show = &ent.TVShow{ID: 3, Title: "Breaking Bad", Year: 2008}
			ep = &ent.Episode{ID: 9, Number: 1, Title: "Pilot"}
		})

		newSvc := func(mode string) *ImportService {
			return NewImportService(&config.LibraryConfig{
				SeriesPath: seriesDir,
				SeriesNaming: "{title}/Season {season:02}/" +
					"{title} - S{season:02}E{episode:02}.{ext}",
				ImportMode: mode,
			})
		}

		It("renders the destination from SeriesNaming", func() {
			src := writeSizedFile(
				downloadDir,
				"Breaking.Bad.S01E01.1080p.WEB-DL.mkv",
				60<<20,
			)

			got, err := newSvc("hardlink").ImportEpisode(ctx, src, show, 1, ep)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Path).To(Equal(filepath.Join(
				seriesDir,
				"Breaking Bad",
				"Season 01",
				"Breaking Bad - S01E01.mkv",
			)))
			Expect(got.Path).To(BeAnExistingFile())
		})

		It("honours an explicit transfer-mode override", func() {
			src := writeSizedFile(
				downloadDir,
				"Breaking.Bad.S01E01.1080p.WEB-DL.mkv",
				60<<20,
			)

			got, err := newSvc("hardlink").
				ImportEpisodeWithMode(ctx, src, show, 1, ep, "copy")
			Expect(err).NotTo(HaveOccurred())

			srcInfo, err := os.Stat(src)
			Expect(err).NotTo(HaveOccurred())
			dstInfo, err := os.Stat(got.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.SameFile(srcInfo, dstInfo)).To(BeFalse())
		})

		It("falls back to the configured mode when no override is given", func() {
			src := writeSizedFile(
				downloadDir,
				"Breaking.Bad.S01E01.1080p.WEB-DL.mkv",
				60<<20,
			)

			got, err := newSvc("hardlink").
				ImportEpisodeWithMode(ctx, src, show, 1, ep, "")
			Expect(err).NotTo(HaveOccurred())

			srcInfo, err := os.Stat(src)
			Expect(err).NotTo(HaveOccurred())
			dstInfo, err := os.Stat(got.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.SameFile(srcInfo, dstInfo)).To(BeTrue())
		})

		It("returns ErrUnsafePath when template output escapes SeriesPath", func() {
			src := writeSizedFile(downloadDir, "Show.S01E01.mkv", 60<<20)
			svc := NewImportService(&config.LibraryConfig{
				SeriesPath:   seriesDir,
				SeriesNaming: "../escape/{title}.{ext}",
				ImportMode:   "copy",
			})

			_, err := svc.ImportEpisode(ctx, src, show, 1, ep)
			Expect(err).To(MatchError(ErrUnsafePath))
		})
	})

	Describe("import metrics", func() {
		It("counts episode imports, not just movie ones", func() {
			ctx := context.Background()
			reader := sdkmetric.NewManualReader()
			otel.SetMeterProvider(
				sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
			)

			tmpDir := GinkgoT().TempDir()
			downloadDir := filepath.Join(tmpDir, "downloads")
			Expect(os.MkdirAll(downloadDir, 0o755)).To(Succeed())
			src := writeSizedFile(downloadDir, "Show.S01E01.1080p.mkv", 60<<20)

			svc := NewImportService(&config.LibraryConfig{
				SeriesPath:   filepath.Join(tmpDir, "tv"),
				SeriesNaming: "{title}/S{season:02}E{episode:02}.{ext}",
				ImportMode:   "copy",
			})
			_, err := svc.ImportEpisode(
				ctx,
				src,
				&ent.TVShow{ID: 1, Title: "Show"},
				1,
				&ent.Episode{ID: 1, Number: 1},
			)
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.ImportEpisode(
				ctx,
				filepath.Join(tmpDir, "nothing-here"),
				&ent.TVShow{ID: 1, Title: "Show"},
				1,
				&ent.Episode{ID: 2, Number: 2},
			)
			Expect(err).To(HaveOccurred())

			var rm metricdata.ResourceMetrics
			Expect(reader.Collect(ctx, &rm)).To(Succeed())
			Expect(importCount(rm, "episode", "success")).To(Equal(int64(1)))
			Expect(importCount(rm, "episode", "no_media")).To(Equal(int64(1)))
		})
	})
})

func importCount(rm metricdata.ResourceMetrics, kind, outcome string) int64 {
	GinkgoHelper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "streamline.library.imports" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			Expect(ok).To(BeTrue(), "imports counter is not an int64 sum")
			for _, dp := range sum.DataPoints {
				k, _ := dp.Attributes.Value(attribute.Key("media.kind"))
				o, _ := dp.Attributes.Value(attribute.Key("outcome"))
				if k.AsString() == kind && o.AsString() == outcome {
					return dp.Value
				}
			}
		}
	}
	return 0
}
