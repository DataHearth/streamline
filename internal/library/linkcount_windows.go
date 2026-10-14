//go:build windows

package library

import "os"

// LinkCount is the number of hard links to the file at path. Windows reports
// none through os.Stat, so a file reads as its only link: the music tag
// writer's seeding guard then rests on the torrent listing alone.
func LinkCount(path string) (uint64, error) {
	if _, err := os.Stat(path); err != nil {
		return 0, err
	}
	return 1, nil
}

// LinksOf is LinkCount for a file already stat-ed: always one on Windows.
func LinksOf(os.FileInfo) uint64 { return 1 }
