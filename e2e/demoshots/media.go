package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/datahearth/streamline/internal/library/audiotags"
)

// fixtures makes the files a completed download leaves on disk. Every video
// file is a hardlink of one of a few clips encoded once into the cache: a
// still frame at a tenth of a frame per second and silent audio, long enough
// to pass the importer's runtime check, then extended sparse to the size a
// real release would have, so the library reads gigabytes while the disk
// holds a few megabytes. The download path and the library are on one
// filesystem and the instance imports by hardlink, so nothing is ever copied.
type fixtures struct {
	cache string
}

type clip struct {
	name     string
	width    int
	height   int
	seconds  int
	codec    string // libx264 or libx265
	padBytes int64
}

var (
	clipMovie1080 = clip{"movie-1080p", 1920, 1080, 7260, "libx264", 4_600_000_000}
	clipMovie2160 = clip{"movie-2160p", 3840, 2160, 7260, "libx265", 14_200_000_000}
	clipEpisode   = clip{"episode-1080p", 1920, 1080, 1620, "libx264", 690_000_000}
)

func (f *fixtures) clip(ctx context.Context, c clip) (string, error) {
	out := filepath.Join(f.cache, c.name+".mkv")
	if st, err := os.Stat(out); err == nil && st.Size() == c.padBytes {
		return out, nil
	}
	tmp := out + ".partial.mkv"
	args := []string{
		"-v",
		"error",
		"-y",
		"-f",
		"lavfi",
		"-i",
		fmt.Sprintf("color=c=0x101018:s=%dx%d:r=0.1", c.width, c.height),
		"-f",
		"lavfi",
		"-i",
		"anullsrc=r=48000:cl=stereo",
		"-t",
		strconv.Itoa(c.seconds),
		"-c:v",
		c.codec,
		"-preset",
		"ultrafast",
		"-pix_fmt",
		"yuv420p",
		"-c:a",
		"aac",
		"-b:a",
		"24k",
		tmp,
	}
	if c.codec == "libx264" {
		args = append(args[:len(args)-1], "-tune", "stillimage", tmp)
	} else {
		args = append(args[:len(args)-1], "-x265-params", "log-level=error", tmp)
	}
	if err := run(ctx, "ffmpeg", args...); err != nil {
		return "", err
	}
	if err := os.Truncate(tmp, c.padBytes); err != nil {
		return "", err
	}
	return out, os.Rename(tmp, out)
}

// link places a hardlink of src at dst, creating dst's directory.
func link(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	_ = os.Remove(dst)
	return os.Link(src, dst)
}

// flacTrack is a silent track of the given length, cached per length.
func (f *fixtures) flacTrack(ctx context.Context, seconds int) (string, error) {
	out := filepath.Join(f.cache, fmt.Sprintf("silence-%ds.flac", seconds))
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	tmp := out + ".partial.flac"
	if err := run(
		ctx,
		"ffmpeg",
		"-v",
		"error",
		"-y",
		"-f",
		"lavfi",
		"-i",
		"anullsrc=r=44100:cl=stereo",
		"-t",
		strconv.Itoa(seconds),
		"-sample_fmt",
		"s16",
		"-c:a",
		"flac",
		tmp,
	); err != nil {
		return "", err
	}
	return out, os.Rename(tmp, out)
}

// mp3Part is a silent audiobook part, cached.
func (f *fixtures) mp3Part(ctx context.Context) (string, error) {
	out := filepath.Join(f.cache, "audiobook-part.mp3")
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	tmp := out + ".partial.mp3"
	if err := run(ctx, "ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "anullsrc=r=22050:cl=mono", "-t", "1800",
		"-c:a", "libmp3lame", "-b:a", "32k", tmp); err != nil {
		return "", err
	}
	return out, os.Rename(tmp, out)
}

// track copies a cached silent file to dst and tags it.
func writeTagged(src, dst string, tags audiotags.Tags) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return audiotags.Write(dst, tags, audiotags.Policy{})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// writeEPUB authors a minimal EPUB 2 whose OPF names the title and author.
func writeEPUB(path, title, author string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
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
	esc := func(s string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	entries := []struct{ name, body string }{
		{
			"META-INF/container.xml",
			`<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		},
		{
			"content.opf",
			`<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" xmlns:opf="http://www.idpf.org/2007/opf" unique-identifier="id" version="2.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>` +
				esc(
					title,
				) + `</dc:title><dc:creator opf:role="aut">` + esc(
				author,
			) + `</dc:creator><dc:identifier id="id">urn:demo:` + esc(
				title,
			) +
				`</dc:identifier><dc:language>en</dc:language></metadata><manifest><item id="c" href="c.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`,
		},
		{
			"c.xhtml",
			`<html xmlns="http://www.w3.org/1999/xhtml"><body><h1>` + esc(
				title,
			) + `</h1></body></html>`,
		},
	}
	for _, e := range entries {
		w, err := zw.Create(e.name)
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
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func run(ctx context.Context, name string, args ...string) error {
	//nolint:gosec // ffmpeg with arguments this program builds
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, stderr.String())
	}
	return nil
}
