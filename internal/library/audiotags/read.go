// Package audiotags reads and writes embedded audio metadata through TagLib
// (go.senan.xyz/taglib: TagLib compiled to WebAssembly and run on wazero, so
// the build stays CGO-free). Write is the only mutation.
package audiotags

import (
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.senan.xyz/taglib"
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
	if err := openable(path); err != nil {
		return Info{}, err
	}
	tags, err := taglib.ReadTags(path)
	if err != nil {
		return info, nil
	}
	info.Artist = first(tags, taglib.Artist)
	info.AlbumArtist = first(tags, taglib.AlbumArtist)
	info.Album = first(tags, taglib.Album)
	info.Title = first(tags, taglib.Title)
	if n := leadingNumber(
		first(tags, taglib.TrackNumber),
	); n > 0 &&
		n <= math.MaxUint16 {
		info.Track = uint16(n)
	}
	if d := leadingNumber(
		first(tags, taglib.DiscNumber),
	); d > 0 &&
		d <= math.MaxUint8 {
		info.Disc = uint8(d)
	}
	date := first(tags, taglib.Date)
	if date == "" {
		date = first(tags, taglib.OriginalDate)
	}
	if y := leadingNumber(date); y > 0 && y <= math.MaxUint16 {
		info.Year = uint16(y)
	}
	return info, nil
}

// ReadTags returns every property the file carries, keyed by TagLib's
// (Picard's) names. Unlike Read it fails on a file TagLib cannot parse.
func ReadTags(path string) (map[string][]string, error) {
	if err := openable(path); err != nil {
		return nil, err
	}
	return taglib.ReadTags(path)
}

// Picture returns the artwork embedded in the file and its MIME type: the
// front cover when the file marks one, else its first picture. A file
// carrying none, or whose tags cannot be parsed, yields nil data and a nil
// error, like Read: only a failure to open is an error.
func Picture(path string) ([]byte, string, error) {
	if err := openable(path); err != nil {
		return nil, "", err
	}
	props, err := taglib.ReadProperties(path)
	if err != nil || len(props.Images) == 0 {
		return nil, "", nil
	}
	idx := 0
	for i, img := range props.Images {
		if img.Type == "Front Cover" {
			idx = i
			break
		}
	}
	data, err := taglib.ReadImageOptions(path, idx)
	if err != nil || len(data) == 0 {
		return nil, "", nil
	}
	mime := props.Images[idx].MIMEType
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	return data, mime, nil
}

// openable reports a file that cannot be opened, the one condition Read and
// Picture return as an error; TagLib itself only says a file is invalid.
func openable(path string) error {
	//nolint:gosec // the caller supplies the path; this package reads library files by design
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return f.Close()
}

func first(tags map[string][]string, key string) string {
	for _, v := range tags[key] {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// leadingNumber reads the digits a value starts with: "5" and "5/12" are 5,
// "1991-09-24" is 1991. Anything else is 0.
func leadingNumber(s string) int {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0
	}
	return n
}
