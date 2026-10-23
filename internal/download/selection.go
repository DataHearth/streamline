package download

import (
	"bytes"
	"fmt"
	"path"

	"github.com/anacrolix/torrent/metainfo"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/library"
)

// metaFile is one file lifted out of a decoded metainfo.
type metaFile struct {
	Index int
	Path  string
	Size  int64
}

// decodeTorrentFiles lists a .torrent's files in metainfo order. A
// single-file torrent has no Files entry, so UpvertedFiles upconverts it into
// one FileInfo named after the torrent itself.
func decodeTorrentFiles(raw []byte) ([]metaFile, error) {
	mi, err := metainfo.Load(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parse torrent file: %w", err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, fmt.Errorf("unmarshal torrent info: %w", err)
	}
	upverted := info.UpvertedFiles()
	files := make([]metaFile, 0, len(upverted))
	for i, fi := range upverted {
		files = append(files, metaFile{
			Index: i,
			Path:  fi.DisplayPath(&info),
			Size:  fi.Length,
		})
	}
	return files, nil
}

// computeKeepSet resolves which file indexes serve wantedEpisodes.
// Only video-extension files at or above library.MinEpisodeSize are
// candidates for skipping — everything else (subtitles, nfo, artwork) is
// always kept (spec §6 rule 5). Returns the kept indexes, the summed size
// of kept video files, and how many video candidates matched a wanted
// episode.
func computeKeepSet(
	files []metaFile,
	seasons []*ent.Season,
	anime bool,
	wantedEpisodes []uint32,
) (keep []int, keptBytes int64, matchedWanted int) {
	wanted := make(map[uint32]bool, len(wantedEpisodes))
	for _, id := range wantedEpisodes {
		wanted[id] = true
	}

	for _, f := range files {
		if !isSkipCandidate(f) {
			keep = append(keep, f.Index)
			continue
		}
		parsed := library.Parse(path.Base(f.Path))
		_, ep := library.MatchEpisodeInSeason(parsed, seasons, anime)
		if ep == nil || !wanted[ep.ID] {
			continue
		}
		keep = append(keep, f.Index)
		keptBytes += f.Size
		matchedWanted++
	}
	return keep, keptBytes, matchedWanted
}

// isSkipCandidate reports whether f is large and video-typed enough to ever
// be excluded from a selective download — the floor that keeps subtitles,
// nfo, artwork and sub-floor samples in regardless of match (spec §6 rule 5).
func isSkipCandidate(f metaFile) bool {
	return library.IsVideoPath(f.Path) && f.Size >= library.MinEpisodeSize
}

// countVideoCandidates is the number of files isSkipCandidate would consider
// for skipping — callers use it against matchedWanted to tell "every
// candidate serves a wanted episode" (not selective) from a partial match.
func countVideoCandidates(files []metaFile) int {
	n := 0
	for _, f := range files {
		if isSkipCandidate(f) {
			n++
		}
	}
	return n
}
