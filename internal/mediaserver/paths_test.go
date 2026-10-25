package mediaserver

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
)

var _ = Describe("refresh paths", Label("unit", "mediaserver"), func() {
	Describe("scanFolders", func() {
		var root string

		BeforeEach(func() {
			root = GinkgoT().TempDir()
			Expect(os.MkdirAll(filepath.Join(root, "Show", "Season 01"), 0o755)).
				To(Succeed())
			Expect(os.WriteFile(
				filepath.Join(root, "Show", "Season 01", "e01.mkv"), nil, 0o644,
			)).To(Succeed())
		})

		It("rescans a file's own folder", func() {
			folders, whole := scanFolders(root, []string{
				filepath.Join(root, "Show", "Season 01", "e01.mkv"),
			})
			Expect(whole).To(BeFalse())
			Expect(
				folders,
			).To(Equal([]string{filepath.Join(root, "Show", "Season 01")}))
		})

		It("climbs from a deleted path to the nearest folder still there", func() {
			folders, whole := scanFolders(root, []string{
				filepath.Join(root, "Show", "Season 02", "e01.mkv"),
			})
			Expect(whole).To(BeFalse())
			Expect(folders).To(Equal([]string{filepath.Join(root, "Show")}))
		})

		It("drops a folder another one in the set already covers", func() {
			folders, _ := scanFolders(root, []string{
				filepath.Join(root, "Show", "Season 01", "e01.mkv"),
				filepath.Join(root, "Show"),
			})
			Expect(folders).To(Equal([]string{filepath.Join(root, "Show")}))
		})

		It(
			"is the whole library for the root, a title deleted with its folder, or nothing",
			func() {
				for _, paths := range [][]string{
					nil,
					{root},
					{filepath.Join(root, "Gone (2020)")},
					{"/elsewhere/file.mkv"},
					{"relative/file.mkv"},
				} {
					folders, whole := scanFolders(root, paths)
					Expect(whole).To(BeTrue(), "%v", paths)
					Expect(folders).To(BeEmpty())
				}
			},
		)
	})

	Describe("mapServerPath", func() {
		mappings := []config.MediaServerPathMapping{
			{From: "/srv/movies", To: "/data/movies"},
			{From: "/srv/tv", To: `D:\TV`},
		}

		It("rewrites the first matching prefix", func() {
			Expect(mapServerPath(mappings, "/srv/movies/Heat (1995)")).
				To(Equal("/data/movies/Heat (1995)"))
			Expect(mapServerPath(mappings, "/srv/movies")).To(Equal("/data/movies"))
		})

		It("joins a Windows prefix with backslashes", func() {
			Expect(mapServerPath(mappings, "/srv/tv/Show/Season 01")).
				To(Equal(`D:\TV\Show\Season 01`))
		})

		It("leaves an unmatched path and a sibling prefix alone", func() {
			Expect(mapServerPath(mappings, "/srv/music/a")).To(Equal("/srv/music/a"))
			Expect(
				mapServerPath(mappings, "/srv/movies2/a"),
			).To(Equal("/srv/movies2/a"))
		})
	})

	Describe("serverPathUnder", func() {
		It("matches on separator boundaries", func() {
			Expect(serverPathUnder("/data/movies/A", "/data/movies")).To(BeTrue())
			Expect(serverPathUnder("/data/movies", "/data/movies/")).To(BeTrue())
			Expect(serverPathUnder("/data/movies2/A", "/data/movies")).To(BeFalse())
		})

		It(
			"compares Windows paths case-insensitively with either separator",
			func() {
				Expect(serverPathUnder(`d:\tv\Show`, `D:\TV`)).To(BeTrue())
				Expect(serverPathUnder(`D:/TV/Show`, `D:\TV\`)).To(BeTrue())
				Expect(serverPathUnder(`D:\TV2\Show`, `D:\TV`)).To(BeFalse())
			},
		)
	})
})
