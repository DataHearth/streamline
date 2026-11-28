package download

import (
	"bytes"
	"fmt"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/library"
)

// buildTorrentBytes bencodes info into a minimal .torrent, mirroring how
// e2e/fakes/torznab.go builds one — no pieces needed since decodeTorrentFiles
// only reads the file list, never validates content against hashes.
func buildTorrentBytes(info metainfo.Info) []byte {
	GinkgoHelper()
	infoBytes, err := bencode.Marshal(info)
	Expect(err).NotTo(HaveOccurred())
	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	var buf bytes.Buffer
	Expect(mi.Write(&buf)).To(Succeed())
	return buf.Bytes()
}

const aboveFloor int64 = library.MinEpisodeSize + 1

// showOf wraps seasons in a show, the shape computeKeepSet reads.
func showOf(seasons []*ent.Season, anime bool) *ent.TVShow {
	show := &ent.TVShow{
		Title: "Show",
		Type:  tvshow.TypeStandard,
		Edges: ent.TVShowEdges{Seasons: seasons},
	}
	if anime {
		show.Type = tvshow.TypeAnime
	}
	return show
}

var _ = Describe("decodeTorrentFiles", Label("unit", "downloads"), func() {
	It("lists multi-file torrents in metainfo order", func() {
		raw := buildTorrentBytes(metainfo.Info{
			Name: "Show",
			Files: []metainfo.FileInfo{
				{Path: []string{"Season 01", "Show.S01E01.mkv"}, Length: aboveFloor},
				{Path: []string{"Season 01", "Show.S01E02.mkv"}, Length: aboveFloor},
			},
		})
		files, err := decodeTorrentFiles(raw)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(Equal([]metaFile{
			{Index: 0, Path: "Season 01/Show.S01E01.mkv", Size: aboveFloor},
			{Index: 1, Path: "Season 01/Show.S01E02.mkv", Size: aboveFloor},
		}))
	})

	It(
		"produces one entry named after the torrent for a single-file torrent",
		func() {
			raw := buildTorrentBytes(metainfo.Info{
				Name:   "Show.S01E01.mkv",
				Length: aboveFloor,
			})
			files, err := decodeTorrentFiles(raw)
			Expect(err).NotTo(HaveOccurred())
			Expect(files).To(Equal([]metaFile{
				{Index: 0, Path: "Show.S01E01.mkv", Size: aboveFloor},
			}))
		},
	)

	It("errors on garbage input", func() {
		_, err := decodeTorrentFiles([]byte("not a torrent"))
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("computeKeepSet", Label("unit", "downloads"), func() {
	// episodeFile is a shorthand for a season-pack video FileInfo at a given
	// season/episode, sized just over MinEpisodeSize.
	episodeFile := func(season, episode int, size int64) metainfo.FileInfo {
		return metainfo.FileInfo{
			Path: []string{
				"Show",
				sprintfSxxEyy(season, episode),
			},
			Length: size,
		}
	}

	It("keeps 1 of 220 files in a 10-season integral, one episode wanted", func() {
		// A smaller representative tree: 3 seasons x 4 episodes preserves the
		// "multi-season pack, single wanted episode buried in it" shape.
		var seasons []*ent.Season
		var files []metainfo.FileInfo
		var wantedID uint32
		id := uint32(1)
		for s := 1; s <= 3; s++ {
			var episodes []*ent.Episode
			for e := 1; e <= 4; e++ {
				episodes = append(episodes, &ent.Episode{ID: id, Number: uint16(e)})
				files = append(files, episodeFile(s, e, aboveFloor))
				if s == 2 && e == 2 {
					wantedID = id
				}
				id++
			}
			seasons = append(seasons, &ent.Season{
				Number: uint16(s),
				Edges:  ent.SeasonEdges{Episodes: episodes},
			})
		}
		mfs := toMetaFiles(files)

		keep, keptBytes, matched := computeKeepSet(
			mfs, showOf(seasons, false), []uint32{wantedID},
		)

		Expect(matched).To(Equal(1))
		Expect(keep).To(Equal([]int{5})) // S02E02 is the 6th file (index 5)
		Expect(keptBytes).To(Equal(aboveFloor))
	})

	It("keeps 2 of 22 files in a season pack", func() {
		var episodes []*ent.Episode
		var files []metainfo.FileInfo
		for e := 1; e <= 22; e++ {
			episodes = append(
				episodes,
				&ent.Episode{ID: uint32(100 + e), Number: uint16(e)},
			)
			files = append(files, episodeFile(1, e, aboveFloor))
		}
		seasons := []*ent.Season{
			{Number: 1, Edges: ent.SeasonEdges{Episodes: episodes}},
		}
		mfs := toMetaFiles(files)

		keep, keptBytes, matched := computeKeepSet(
			mfs, showOf(seasons, false), []uint32{105, 110},
		)

		Expect(matched).To(Equal(2))
		Expect(keep).To(Equal([]int{4, 9}))
		Expect(keptBytes).To(Equal(2 * aboveFloor))
	})

	It("keeps 1 of 1 for a single-episode torrent, all matched", func() {
		seasons := []*ent.Season{
			{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
				{ID: 11, Number: 2},
			}}},
		}
		mfs := []metaFile{{Index: 0, Path: "Show.S01E02.mkv", Size: aboveFloor}}

		keep, keptBytes, matched := computeKeepSet(
			mfs, showOf(seasons, false), []uint32{11})

		Expect(matched).To(Equal(1))
		Expect(keep).To(Equal([]int{0}))
		Expect(keptBytes).To(Equal(aboveFloor))
	})

	It("matches on absolute number for anime", func() {
		seasons := []*ent.Season{
			{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
				{ID: 51, AbsoluteNumber: 1},
				{ID: 52, AbsoluteNumber: 2},
			}}},
		}
		mfs := []metaFile{
			{Index: 0, Path: "Show - 01.mkv", Size: aboveFloor},
			{Index: 1, Path: "Show - 02.mkv", Size: aboveFloor},
		}

		keep, keptBytes, matched := computeKeepSet(
			mfs, showOf(seasons, true), []uint32{52})

		Expect(matched).To(Equal(1))
		Expect(keep).To(Equal([]int{1}))
		Expect(keptBytes).To(Equal(aboveFloor))
	})

	It(
		"keeps a Sample/ clip under MinEpisodeSize even though it's unmatched/unwanted",
		func() {
			seasons := []*ent.Season{
				{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
					{ID: 21, Number: 1},
					{ID: 22, Number: 2},
				}}},
			}
			mfs := []metaFile{
				{
					Index: 0,
					Path:  "Show/Sample/Show.S01E01.mkv",
					Size:  library.MinEpisodeSize - 1,
				},
				{Index: 1, Path: "Show/Show.S01E01.mkv", Size: aboveFloor},
				{Index: 2, Path: "Show/Show.S01E02.mkv", Size: aboveFloor},
			}

			// Only episode 2 is wanted; the sample (of episode 1, and sub-floor
			// regardless) must stay in the keep set anyway.
			keep, keptBytes, matched := computeKeepSet(
				mfs, showOf(seasons, false), []uint32{22},
			)

			Expect(keep).To(ConsistOf(0, 2))
			Expect(matched).To(Equal(1))
			Expect(keptBytes).To(Equal(aboveFloor))
		},
	)

	It("always keeps .srt/.nfo sidecars regardless of match", func() {
		seasons := []*ent.Season{
			{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
				{ID: 31, Number: 1},
			}}},
		}
		mfs := []metaFile{
			{Index: 0, Path: "Show/Show.S01E01.mkv", Size: aboveFloor},
			{Index: 1, Path: "Show/Show.S01E01.srt", Size: 4096},
			{Index: 2, Path: "Show/show.nfo", Size: 512},
		}

		// Nothing wanted at all: the episode itself is excluded, but the
		// sidecars are kept unconditionally (spec §6 rule 5).
		keep, keptBytes, matched := computeKeepSet(
			mfs, showOf(seasons, false), nil)

		Expect(keep).To(ConsistOf(1, 2))
		Expect(matched).To(Equal(0))
		Expect(keptBytes).To(Equal(int64(0)))
	})

	It(
		"reports matchedWanted == 0 when the pack's numbering matches no library episode",
		func() {
			seasons := []*ent.Season{
				{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
					{ID: 41, Number: 1},
					{ID: 42, Number: 2},
				}}},
			}
			mfs := []metaFile{
				// S02Exx: season 1 only has 2 episodes, so this never matches.
				{Index: 0, Path: "Show/Show.S02E23.mkv", Size: aboveFloor},
				{Index: 1, Path: "Show/Show.S02E24.mkv", Size: aboveFloor},
			}

			keep, keptBytes, matched := computeKeepSet(
				mfs, showOf(seasons, false), []uint32{41, 42},
			)

			Expect(matched).To(Equal(0))
			Expect(keep).To(BeEmpty())
			Expect(keptBytes).To(Equal(int64(0)))
		},
	)
})

// A special is released under its title and no number, so a grab made for
// exactly one season 0 episode reads its files by title too.
var _ = Describe(
	"computeKeepSet for one special",
	Label("unit", "downloads"),
	func() {
		const dayID, regularID = uint32(900), uint32(11)

		doctorWho := func() *ent.TVShow {
			return &ent.TVShow{
				Title:   "Doctor Who (2005)",
				Aliases: []string{"Doctor Who"},
				Type:    tvshow.TypeStandard,
				Edges: ent.TVShowEdges{Seasons: []*ent.Season{
					{Number: 0, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
						{ID: dayID, Number: 5, Title: "The Day of the Doctor"},
						{ID: 901, Number: 77, Title: "The Time of the Doctor"},
					}}},
					{Number: 7, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
						{
							ID:     regularID,
							Number: 5,
							Title:  "The Angels Take Manhattan",
						},
					}}},
				}},
			}
		}
		video := func(i int, name string) metaFile {
			return metaFile{Index: i, Path: "Release/" + name, Size: aboveFloor}
		}

		It("keeps the file naming the special by its title", func() {
			keep, keptBytes, matched := computeKeepSet([]metaFile{
				video(0, "Doctor.Who.The.Day.of.the.Doctor.1080p.BluRay.mkv"),
				video(1, "Doctor.Who.The.Day.of.the.Doctor.Deleted.Scenes.mkv"),
				{Index: 2, Path: "Release/release.nfo", Size: 512},
			}, doctorWho(), []uint32{dayID})

			Expect(matched).To(Equal(1))
			Expect(keep).To(Equal([]int{0, 2}))
			Expect(keptBytes).To(Equal(aboveFloor))
		})

		It("reads a title before the number a tracker gave the special", func() {
			// S00E77 is another special in the library; the title says which.
			keep, _, matched := computeKeepSet([]metaFile{
				video(0, "Doctor.Who.S00E77.The.Day.of.the.Doctor.1080p.mkv"),
			}, doctorWho(), []uint32{dayID})

			Expect(matched).To(Equal(1))
			Expect(keep).To(Equal([]int{0}))
		})

		It("keeps a lone video that names no episode at all", func() {
			// The importer files a release's only video under its anchor,
			// whatever it is called.
			keep, keptBytes, matched := computeKeepSet([]metaFile{
				video(0, "feature.mkv"),
				{
					Index: 1,
					Path:  "Release/Sample/sample.mkv",
					Size:  library.MinEpisodeSize - 1,
				},
			}, doctorWho(), []uint32{dayID})

			Expect(matched).To(Equal(1))
			Expect(keep).To(Equal([]int{0, 1}))
			Expect(keptBytes).To(Equal(aboveFloor))
		})

		// The importer files a release's only video under its anchor, so
		// only a name that is another episode of the library is refused.
		DescribeTable(
			"keeps a lone video that is no other episode",
			func(name string) {
				keep, _, matched := computeKeepSet(
					[]metaFile{video(0, name)}, doctorWho(), []uint32{dayID},
				)
				Expect(matched).To(Equal(1))
				Expect(keep).To(Equal([]int{0}))
			},
			Entry("an episode 0 of a season", "grp-dw.s07e00.1080p.mkv"),
			Entry("a special number the library lacks", "Show.S00E99.mkv"),
			Entry(
				"a regular episode the library lacks",
				"Doctor.Who.S09E01.1080p.mkv",
			),
			Entry("the specials season alone", "Show.S00.1080p.mkv"),
			Entry("an absolute number", "Doctor Who - 05.mkv"),
			Entry("a date", "Doctor.Who.2013.11.23.1080p.mkv"),
		)

		DescribeTable("still matches nothing",
			func(wanted []uint32, names ...string) {
				files := make([]metaFile, 0, len(names))
				for i, name := range names {
					files = append(files, video(i, name))
				}
				keep, _, matched := computeKeepSet(files, doctorWho(), wanted)
				Expect(matched).To(BeZero())
				Expect(keep).To(BeEmpty())
			},
			Entry("a lone video of a regular episode",
				[]uint32{dayID}, "Doctor.Who.S07E05.1080p.mkv"),
			Entry("a lone video numbered as another special",
				[]uint32{dayID}, "Doctor.Who.S00E77.1080p.mkv"),
			Entry("a lone video titled as another special",
				[]uint32{dayID}, "Doctor.Who.The.Time.of.the.Doctor.1080p.mkv"),
			Entry("another special's file beside an unnamed one",
				[]uint32{dayID},
				"Doctor.Who.The.Time.of.the.Doctor.1080p.mkv", "feature.mkv"),
			Entry("two videos naming nothing",
				[]uint32{dayID}, "feature.mkv", "extras.mkv"),
			Entry("a lone unnamed video for a regular episode",
				[]uint32{regularID}, "feature.mkv"),
			Entry("a title-named file when more than the special is wanted",
				[]uint32{dayID, regularID}, "Doctor.Who.The.Day.of.the.Doctor.mkv"),
		)
	},
)

var _ = Describe("countVideoCandidates", Label("unit", "downloads"), func() {
	It(
		"counts only files that are video-extension and at/above MinEpisodeSize",
		func() {
			mfs := []metaFile{
				{Index: 0, Path: "Show.S01E01.mkv", Size: aboveFloor},
				{Index: 1, Path: "Show.S01E02.mkv", Size: aboveFloor},
				{
					Index: 2,
					Path:  "Sample/Show.S01E01.mkv",
					Size:  library.MinEpisodeSize - 1,
				},
				{Index: 3, Path: "Show.S01E01.srt", Size: 4096},
			}
			Expect(countVideoCandidates(mfs)).To(Equal(2))
		},
	)

	It("returns 0 for an empty file list", func() {
		Expect(countVideoCandidates(nil)).To(Equal(0))
	})
})

func toMetaFiles(files []metainfo.FileInfo) []metaFile {
	info := metainfo.Info{Name: "Show", Files: files}
	out := make([]metaFile, 0, len(files))
	for i, fi := range files {
		out = append(
			out,
			metaFile{Index: i, Path: fi.DisplayPath(&info), Size: fi.Length},
		)
	}
	return out
}

func sprintfSxxEyy(season, episode int) string {
	return fmt.Sprintf("Show.S%02dE%02d.mkv", season, episode)
}
