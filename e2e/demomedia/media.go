// Package demomedia writes small media files that streamline's importer, its
// probe and its bulk import accept as the real thing: a video whose header
// states a resolution, codecs and a feature-length duration but whose body is
// a sparse hole, a tagged MP3 track, and a minimal EPUB. The fake tracker
// seeds them and the demo screenshot generator adopts them.
//
// Every writer leaves an existing file of the expected size alone, so the
// bytes, and the infohash of a torrent built over them, stay the same from one
// run to the next.
package demomedia

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/datahearth/streamline/internal/library/audiotags"
)

//go:embed testdata/track.mp3
var trackMP3 []byte

// ErrNoFFmpeg is returned by Video when ffmpeg is not on PATH.
var ErrNoFFmpeg = errors.New("demomedia: ffmpeg not found on PATH")

// VideoSpec describes the file Video writes. Zero values take the defaults
// noted on each field.
type VideoSpec struct {
	Height int    // 2160, 1080, 720 or 480; default 1080
	Codec  string // "hevc" or "h264"; default "hevc"
	HDR    bool   // tags the stream BT.2020/PQ, 10-bit
	// Audio is "eac3", "ac3" or "aac", with Channels channels; default eac3 5.1.
	Audio    string
	Channels int
	// Duration is what the container states; default 3h, which passes the
	// importer's runtime check for any film.
	Duration time.Duration
	// Size is the file's apparent size; the header is padded with a sparse
	// hole, so it costs no disk. Default: the header alone.
	Size int64
}

// Video writes an MKV whose header carries spec. The stream holds a frame
// every hour and a second of silence, so the file is a few tens of KB before
// padding, takes well under a second to encode and probes as spec says.
func Video(path string, spec VideoSpec) error {
	if spec.Height == 0 {
		spec.Height = 1080
	}
	if spec.Codec == "" {
		spec.Codec = "hevc"
	}
	if spec.Audio == "" {
		spec.Audio = "eac3"
	}
	if spec.Channels == 0 {
		spec.Channels = 6
	}
	if spec.Duration == 0 {
		spec.Duration = 3 * time.Hour
	}
	if st, err := os.Stat(
		path,
	); err == nil && spec.Size > 0 &&
		st.Size() == spec.Size {
		return nil
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ErrNoFFmpeg
	}
	width := map[int]int{2160: 3840, 1080: 1920, 720: 1280, 480: 854}[spec.Height]
	if width == 0 {
		return fmt.Errorf("demomedia: unsupported height %d", spec.Height)
	}
	layout := map[int]string{1: "mono", 2: "stereo", 6: "5.1", 8: "7.1"}[spec.Channels]
	if layout == "" {
		return fmt.Errorf("demomedia: unsupported channel count %d", spec.Channels)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	secs := strconv.Itoa(int(spec.Duration.Seconds()))
	args := []string{
		"-hide_banner",
		"-loglevel",
		"error",
		"-y",
		"-f",
		"lavfi",
		"-i",
		fmt.Sprintf("color=c=black:s=%dx%d:r=1/3600:d=%s", width, spec.Height, secs),
		"-f",
		"lavfi",
		"-t",
		"1",
		"-i",
		"anullsrc=r=48000:cl=" + layout,
		"-map",
		"0",
		"-map",
		"1",
	}
	switch spec.Codec {
	case "hevc":
		args = append(
			args,
			"-c:v",
			"libx265",
			"-preset",
			"ultrafast",
			"-x265-params",
			"log-level=error",
		)
	case "h264":
		args = append(args, "-c:v", "libx264", "-preset", "ultrafast")
	default:
		return fmt.Errorf("demomedia: unsupported codec %q", spec.Codec)
	}
	if spec.HDR {
		args = append(
			args,
			"-pix_fmt",
			"yuv420p10le",
			"-color_primaries",
			"bt2020",
			"-color_trc",
			"smpte2084",
			"-colorspace",
			"bt2020nc",
		)
	} else {
		args = append(args, "-pix_fmt", "yuv420p")
	}
	args = append(args,
		"-c:a", spec.Audio, "-b:a", "192k",
		"-fflags", "+bitexact", "-flags", "+bitexact", "-map_metadata", "-1",
		"-f", "matroska", path,
	)
	//nolint:gosec // G204: ffmpeg from PATH, with arguments built here from a VideoSpec
	if out, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("demomedia: ffmpeg: %w: %s", err, bytes.TrimSpace(out))
	}
	if spec.Size > 0 {
		if err := os.Truncate(path, spec.Size); err != nil {
			return fmt.Errorf("demomedia: pad: %w", err)
		}
	}
	return nil
}

// Track writes the embedded MP3 template to path and tags it. An existing
// file is kept: the tag writer's frame order is not stable, so a rewrite would
// move the infohash of a torrent built over it.
func Track(path string, tags audiotags.Tags) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, trackMP3, 0o644); err != nil {
		return fmt.Errorf("demomedia: write track: %w", err)
	}
	return audiotags.Write(path, tags, audiotags.Policy{})
}

// EbookMeta is what Ebook writes into the package document: the fields bulk
// import's OPF reader identifies a book by.
type EbookMeta struct {
	Title    string
	Author   string
	ISBN     string // optional; lets bulk import look the book up directly
	Language string // default "en"
	ID       string // dc:identifier when there is no ISBN; default derived from the title
}

// Ebook authors a minimal EPUB: a stored mimetype entry first, then the
// container pointing at an OPF with the title, author and identifiers.
func Ebook(path string, m EbookMeta) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if m.Language == "" {
		m.Language = "en"
	}
	if m.ID == "" {
		m.ID = "urn:demomedia:" + m.Title
	}
	esc := func(s string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	isbn := ""
	if m.ISBN != "" {
		isbn = `<dc:identifier opf:scheme="ISBN">` + esc(m.ISBN) + `</dc:identifier>`
	}
	entries := []struct{ name, body string }{
		{
			"META-INF/container.xml",
			`<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		},
		{
			"content.opf",
			`<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" xmlns:opf="http://www.idpf.org/2007/opf" unique-identifier="id" version="2.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>` + esc(
				m.Title,
			) + `</dc:title><dc:creator opf:role="aut">` + esc(
				m.Author,
			) + `</dc:creator><dc:identifier id="id">` + esc(
				m.ID,
			) + `</dc:identifier>` + isbn + `<dc:language>` + esc(
				m.Language,
			) + `</dc:language></metadata><manifest><item id="c" href="c.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`,
		},
		{
			"c.xhtml",
			`<html xmlns="http://www.w3.org/1999/xhtml"><body><p>` + esc(
				m.Title,
			) + `</p></body></html>`,
		},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mt, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		return err
	}
	if _, err := mt.Write([]byte("application/epub+zip")); err != nil {
		return err
	}
	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Deflate})
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
