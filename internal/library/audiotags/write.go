package audiotags

import (
	"crypto/sha256"
	"encoding/hex"
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

// Policy is what a write does beyond the keys it is given. Strip names keys
// deleted from the file; Preserve names keys never written or deleted, and
// wins over both Strip and the keys passed. An entry ending in * matches as a
// prefix.
type Policy struct {
	Strip    []string
	Preserve []string
}

// Write applies tags to the file at path, then deletes every existing key
// the policy strips. It never modifies the file in place: the write goes to
// a copy beside it, which is read back and compared with tags, then renamed
// over the original. A failed write or check leaves the original untouched,
// and a hard link to the original (a torrent still seeding it) keeps the old
// bytes. A format TagLib is not used for here is ErrUnsupportedFormat, and
// the caller imports without writing.
func Write(path string, tags Tags, p Policy) error {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := AudioExtensions[ext]; !ok {
		return ErrUnsupportedFormat
	}
	if err := write(path, ext, p.apply(tags), p); err != nil {
		return fmt.Errorf("audiotags: write %s: %w", filepath.Base(path), err)
	}
	return nil
}

// apply drops the keys the policy preserves.
func (p Policy) apply(tags Tags) Tags {
	out := make(Tags, len(tags))
	for k, v := range tags {
		if !matches(k, p.Preserve) {
			out[k] = v
		}
	}
	return out
}

// Hash identifies what a Write of tags under p would leave in a file: the
// kept keys and their values in a canonical order, the policy, and extra
// (the embedded cover's identity). Two equal hashes mean a second write would
// change nothing Streamline owns.
func Hash(tags Tags, p Policy, extra ...string) string {
	kept := p.apply(tags)
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(kept)) {
		b.WriteString(k + "\x1e" + strings.Join(clean(kept[k]), "\x1e") + "\x1f")
	}
	b.WriteString("strip\x1e" + strings.Join(p.Strip, "\x1e") + "\x1f")
	b.WriteString("preserve\x1e" + strings.Join(p.Preserve, "\x1e") + "\x1f")
	for _, e := range extra {
		b.WriteString(e + "\x1f")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func write(path, ext string, tags Tags, p Policy) error {
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
		if matches(k, p.Strip) && !matches(k, p.Preserve) {
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
		out[k] = clean(vs)
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

// clean trims each value and drops the empty ones.
func clean(vs []string) []string {
	var keep []string
	for _, v := range vs {
		if v = strings.TrimSpace(v); v != "" {
			keep = append(keep, v)
		}
	}
	return keep
}

// matches reports a key named by patterns, an entry ending in * matching as
// a prefix. Keys are compared upper case, as TagLib reports them.
func matches(key string, patterns []string) bool {
	key = strings.ToUpper(key)
	for _, p := range patterns {
		p = strings.ToUpper(strings.TrimSpace(p))
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
