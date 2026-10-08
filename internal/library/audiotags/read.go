// Package audiotags reads embedded audio metadata; Write is the only mutation.
package audiotags

import (
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
)

type Info struct {
	Artist      string
	AlbumArtist string
	Album       string
	Title       string
	Track       uint16
	Disc        uint8
	Year        uint16
	Format      string
}

var AudioExtensions = map[string]struct{}{
	".flac": {}, ".mp3": {}, ".m4a": {}, ".ogg": {}, ".opus": {},
}

// Read returns an Info with only Format set and a nil error when the file has
// no tags or they cannot be parsed: absence of tags is data, and the caller
// falls back to filename parsing. Only a failure to open is an error.
func Read(path string) (Info, error) {
	info := Info{
		Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
	//nolint:gosec // the caller supplies the path; this package reads library files by design
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return info, nil
	}
	info.Artist = m.Artist()
	info.AlbumArtist = m.AlbumArtist()
	info.Album = m.Album()
	info.Title = m.Title()
	if n, _ := m.Track(); n > 0 && n <= math.MaxUint16 {
		info.Track = uint16(n)
	}
	if d, _ := m.Disc(); d > 0 && d <= math.MaxUint8 {
		info.Disc = uint8(d)
	}
	if y := m.Year(); y > 0 && y <= math.MaxUint16 {
		info.Year = uint16(y)
	}
	return info, nil
}
