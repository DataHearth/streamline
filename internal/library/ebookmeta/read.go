// Package ebookmeta extracts identifying metadata from ebooks and Calibre
// sidecar files. Read-only: files are never mutated.
package ebookmeta

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"os"
	"strings"
	"time"
)

type Info struct {
	Title  string
	Author string
	ISBN   string
	Year   uint16
}

var EbookExtensions = map[string]struct{}{
	".epub": {}, ".mobi": {}, ".azw3": {}, ".pdf": {},
}

var AudiobookExtensions = map[string]struct{}{
	".m4b": {}, ".mp3": {}, ".m4a": {}, ".flac": {}, ".ogg": {}, ".opus": {},
}

type opfPackage struct {
	Metadata struct {
		Titles      []string `xml:"title"`
		Creators    []string `xml:"creator"`
		Identifiers []struct {
			Scheme string `xml:"scheme,attr"`
			Value  string `xml:",chardata"`
		} `xml:"identifier"`
		Dates []string `xml:"date"`
	} `xml:"metadata"`
}

type epubContainer struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

// ReadEpub returns zero-valued Info with a nil error for an epub that carries
// no metadata: absence is data, and the caller falls back to the filename.
// mobi/azw3/pdf have no embedded OPF; those callers go straight to
// sidecar/filename.
func ReadEpub(path string) (Info, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return Info{}, err
	}
	defer zr.Close()
	var container epubContainer
	err = decodeZipXML(&zr.Reader, "META-INF/container.xml", &container)
	if err != nil {
		return Info{}, err
	}
	if len(container.Rootfiles) == 0 {
		return Info{}, nil
	}
	var pkg opfPackage
	opfPath := container.Rootfiles[0].FullPath
	if err := decodeZipXML(&zr.Reader, opfPath, &pkg); err != nil {
		return Info{}, err
	}
	return pkg.toInfo(), nil
}

func ReadSidecar(path string) (Info, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from the library scan
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	var pkg opfPackage
	if err := xml.NewDecoder(f).Decode(&pkg); err != nil {
		return Info{}, err
	}
	return pkg.toInfo(), nil
}

func decodeZipXML(zr *zip.Reader, name string, out any) error {
	f, err := zr.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return err
	}
	return xml.Unmarshal(raw, out)
}

func (p opfPackage) toInfo() Info {
	info := Info{}
	if len(p.Metadata.Titles) > 0 {
		info.Title = strings.TrimSpace(p.Metadata.Titles[0])
	}
	if len(p.Metadata.Creators) > 0 {
		info.Author = strings.TrimSpace(p.Metadata.Creators[0])
	}
	for _, id := range p.Metadata.Identifiers {
		v := strings.TrimSpace(id.Value)
		switch {
		case strings.EqualFold(id.Scheme, "ISBN"):
			info.ISBN = v
		case strings.HasPrefix(strings.ToLower(v), "urn:isbn:"):
			info.ISBN = v[len("urn:isbn:"):]
		}
		if info.ISBN != "" {
			break
		}
	}
	for _, d := range p.Metadata.Dates {
		for _, layout := range []string{"2006-01-02", "2006-01-02T15:04:05Z07:00", "2006"} {
			if t, err := time.Parse(layout, strings.TrimSpace(d)); err == nil {
				//nolint:gosec // time.Parse yields 0..9999
				info.Year = uint16(t.Year())
				break
			}
		}
		if info.Year > 0 {
			break
		}
	}
	return info
}
