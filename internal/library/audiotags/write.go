package audiotags

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.senan.xyz/taglib"
)

var ErrUnsupportedFormat = errors.New(
	"audiotags: unsupported format for tag writing",
)

// ErrVerify is a write whose result did not read back as written. The
// original file is left exactly as it was.
var ErrVerify = errors.New("audiotags: written tags did not read back")

// Tags maps TagLib property keys (the taglib constants, Picard's names) to
// values. Write replaces each key present with its values and deletes each
// key mapped to no value, so a caller that owns a key and has nothing for it
// passes it empty rather than leaving a stale value behind. Keys the map does
// not name are kept: ReplayGain, lyrics, AcoustID, embedded pictures and
// anything else the file carries survive a write.
//
// TRACKNUMBER/TRACKTOTAL and DISCNUMBER/DISCTOTAL are passed separately;
// Write folds them into "n/total" for ID3 and MP4, which have no separate
// total field, and keeps them apart in Vorbis comments.
type Tags map[string][]string

// Track and disc totals, which TagLib has no constant for: Picard's names.
const (
	TrackTotal = "TRACKTOTAL"
	DiscTotal  = "DISCTOTAL"
)

// DefaultStrip lists the keys deleted on every write: the comment and link
// fields a release carries its tracker or ripper notes in, and the encoder
// credit of whoever made the copy.
var DefaultStrip = []string{
	taglib.Comment,
	taglib.EncodedBy,
	taglib.URL,
	taglib.FileWebpage,
	taglib.AudioSourceWebpage,
	taglib.PaymentWebpage,
	taglib.PublisherWebpage,
}

// Write applies tags to the file at path, then deletes every existing key
// that strip names (an entry ending in * matches as a prefix). Managed keys
// win over strip. It never modifies the file in place: the write goes to a
// copy beside it, which is read back and compared with tags, then renamed
// over the original. A failed write or check leaves the original untouched,
// and a hard link to the original (a torrent still seeding it) keeps the old
// bytes. A format TagLib is not used for here is ErrUnsupportedFormat, and
// the caller imports without writing.
func Write(path string, tags Tags, strip []string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := AudioExtensions[ext]; !ok {
		return ErrUnsupportedFormat
	}
	if err := write(path, ext, tags, strip); err != nil {
		return fmt.Errorf("audiotags: write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func write(path, ext string, tags Tags, strip []string) error {
	tmp, err := siblingCopy(path, ext)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmp)
		}
	}()

	current, err := taglib.ReadTags(tmp)
	if err != nil {
		return err
	}
	want := fold(ext, tags)
	update := map[string][]string{}
	for k := range current {
		if stripped(k, strip) {
			update[k] = nil
		}
	}
	maps.Copy(update, want)
	if err := taglib.WriteTags(tmp, update, 0); err != nil {
		return err
	}

	got, err := taglib.ReadTags(tmp)
	if err != nil {
		return err
	}
	for k, v := range want {
		if !slices.Equal(got[k], v) {
			return fmt.Errorf("%w: %s is %q, wrote %q", ErrVerify, k, got[k], v)
		}
	}

	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	committed = true
	return nil
}

// fold returns tags as the format stores them: values trimmed and emptied
// out, and the totals folded into the number for ID3 and MP4.
func fold(ext string, tags Tags) map[string][]string {
	out := make(map[string][]string, len(tags))
	for k, vs := range tags {
		var keep []string
		for _, v := range vs {
			if v = strings.TrimSpace(v); v != "" {
				keep = append(keep, v)
			}
		}
		out[k] = keep
	}
	if ext != ".mp3" && ext != ".m4a" {
		return out
	}
	for _, pair := range [][2]string{
		{taglib.TrackNumber, TrackTotal},
		{taglib.DiscNumber, DiscTotal},
	} {
		num, total := pair[0], pair[1]
		totals, named := out[total]
		if !named {
			continue
		}
		if len(out[num]) == 1 && len(totals) == 1 {
			out[num] = []string{out[num][0] + "/" + totals[0]}
		}
		// Deleted, not just dropped: an earlier tagger may have left one
		// behind as a free-form frame.
		out[total] = nil
	}
	return out
}

func stripped(key string, patterns []string) bool {
	for _, p := range patterns {
		if prefix, ok := strings.CutSuffix(p, "*"); ok {
			if strings.HasPrefix(key, prefix) {
				return true
			}
		} else if key == p {
			return true
		}
	}
	return false
}

// siblingCopy copies path to a new hidden file in the same directory, so the
// final rename is atomic, with the original's permission bits. The copy keeps
// the extension: TagLib picks the format from it.
func siblingCopy(path, ext string) (string, error) {
	//nolint:gosec // the caller supplies the path; this package rewrites library files by design
	src, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		return "", err
	}
	dst, err := os.CreateTemp(filepath.Dir(path), ".streamline-tags-*"+ext)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(dst, src)
	err = errors.Join(err, dst.Chmod(st.Mode().Perm()), dst.Close())
	if err != nil {
		_ = os.Remove(dst.Name())
		return "", err
	}
	return dst.Name(), nil
}
