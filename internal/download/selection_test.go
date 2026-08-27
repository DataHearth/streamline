package download

import (
	"bytes"
	"fmt"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
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
			mfs, seasons, false, []uint32{wantedID},
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
			mfs, seasons, false, []uint32{105, 110},
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

		keep, keptBytes, matched := computeKeepSet(mfs, seasons, false, []uint32{11})

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

		keep, keptBytes, matched := computeKeepSet(mfs, seasons, true, []uint32{52})

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
				mfs,
				seasons,
				false,
				[]uint32{22},
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
		keep, keptBytes, matched := computeKeepSet(mfs, seasons, false, nil)

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
				mfs,
				seasons,
				false,
				[]uint32{41, 42},
			)

			Expect(matched).To(Equal(0))
			Expect(keep).To(BeEmpty())
			Expect(keptBytes).To(Equal(int64(0)))
		},
	)
})

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
