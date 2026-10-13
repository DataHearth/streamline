package audiotags

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

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
	_, err := WriteWithArt(path, tags, p, Art{})
	return err
}

// OwnArtDescription is the description Streamline gives the front cover it
// embeds, in the formats that keep one. It is a courtesy to whoever reads the
// file: MP4 keeps no description, so what identifies Streamline's picture is
// the hash the caller records (Art.Ours), never this.
const OwnArtDescription = "Streamline"

// Art is what a write does to the embedded front cover. With Manage unset the
// pictures are left exactly as they are. With it set, Front is embedded as
// the front cover when the file has none or the one it has is Streamline's
// (its SHA-256 is Ours), and a nil Front removes Streamline's; a front cover
// Streamline did not embed — the release's own art — is never replaced or
// removed, and every other picture (a back cover, a booklet page) is kept.
type Art struct {
	Manage bool
	Front  []byte
	Ours   string
}

// WriteWithArt is Write plus the front cover, in the same copy, check and
// rename. It returns the hash of the front cover Streamline now owns in the
// file, empty when there is none: the caller stores it as the next Art.Ours.
// With Manage unset it returns Ours unchanged.
func WriteWithArt(path string, tags Tags, p Policy, art Art) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := AudioExtensions[ext]; !ok {
		return art.Ours, ErrUnsupportedFormat
	}
	ours, err := write(path, ext, p.apply(tags), p, art)
	if err != nil {
		return art.Ours, fmt.Errorf(
			"audiotags: write %s: %w",
			filepath.Base(path),
			err,
		)
	}
	return ours, nil
}

// ArtHash identifies a picture's bytes, as Art.Ours and WriteWithArt's
// result do.
func ArtHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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

func write(path, ext string, tags Tags, p Policy, art Art) (string, error) {
	removeStaleCopies(filepath.Dir(path))
	tmp, st, err := siblingCopy(path, ext)
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmp)
		}
	}()

	current, err := taglib.ReadTags(tmp)
	if err != nil {
		return "", err
	}
	want, update := plan(ext, current, tags, p)
	if err := taglib.WriteTags(tmp, update, 0); err != nil {
		return "", err
	}

	got, err := taglib.ReadTags(tmp)
	if err != nil {
		return "", err
	}
	for k, v := range want {
		if !slices.Equal(got[k], v) {
			return "", fmt.Errorf("%w: %s is %q, wrote %q", ErrVerify, k, got[k], v)
		}
	}

	ours := art.Ours
	if art.Manage {
		if ours, err = writeArt(tmp, ext, art); err != nil {
			return "", err
		}
	}

	// The copy replaces the original under its name, so it takes on what
	// the original carried beyond its bytes: its mode, its extended
	// attributes (a POSIX ACL among them) and, last, its owner — chowning
	// away first would leave the copy one Streamline can no longer change.
	if err := keepAttributes(path, tmp, st); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	committed = true
	return ours, nil
}

// ErrOwnership is a write whose copy could not be given the original's owner,
// group or extended attributes: renaming it over the original would change
// who can read the file (a media server running as another user, a group the
// library is shared through). The original is left untouched, and retrying
// will not help until the permissions change.
var ErrOwnership = errors.New(
	"audiotags: the file's owner or attributes cannot be kept",
)

// keepAttributes gives the copy at tmp the original's permission bits,
// extended attributes and owner.
func keepAttributes(orig, tmp string, st os.FileInfo) error {
	if err := os.Chmod(tmp, st.Mode().Perm()); err != nil {
		return err
	}
	if err := copyXattrs(orig, tmp); err != nil {
		return fmt.Errorf("%w: %w", ErrOwnership, err)
	}
	if err := keepOwner(tmp, st); err != nil {
		return fmt.Errorf("%w: %w", ErrOwnership, err)
	}
	return nil
}

// TempPrefix starts the name of the hidden copy a write works on. A copy is
// removed when the write ends, but one a killed process left behind keeps the
// audio extension (TagLib picks the format from it): IsAudioFile never takes
// it for a track, and the next write in its folder removes it once it is
// older than staleCopyAge.
const TempPrefix = ".streamline-tags-"

// staleCopyAge is how old a leftover copy must be before a write removes it:
// far longer than any write takes, so a copy another write is still working
// on is never touched.
const staleCopyAge = time.Hour

// IsAudioFile reports a file the music scanners and importers treat as a
// track: one with an audio extension whose name is not hidden. A hidden one
// is a write's copy or a macOS resource fork (._01.flac), never a track.
func IsAudioFile(path string) bool {
	name := filepath.Base(path)
	if strings.HasPrefix(name, ".") {
		return false
	}
	_, ok := AudioExtensions[strings.ToLower(filepath.Ext(name))]
	return ok
}

// removeStaleCopies deletes the copies a write killed mid-way left in dir.
func removeStaleCopies(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-staleCopyAge)
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), TempPrefix) {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// plan is a write of tags (already filtered by p.apply) over a file holding
// current: want, the keys the write sets as the format stores them, and
// update, what TagLib is handed — want plus a deletion for every existing key
// the policy strips and, when the performers are passed, every PERFORMER:*
// key want does not carry.
func plan(
	ext string,
	current map[string][]string,
	tags Tags,
	p Policy,
) (want, update map[string][]string) {
	want = fold(ext, tags)
	_, performers := tags[taglib.Performer]
	update = map[string][]string{}
	for k := range current {
		if matches(k, p.Preserve) {
			continue
		}
		_, kept := want[k]
		switch {
		case matches(k, p.Strip):
			update[k] = nil
		case performers && !kept && strings.HasPrefix(k, performerPrefix):
			// The performers are owned as a family: an instrument no
			// longer credited, or one an earlier tagger spelled
			// differently, goes.
			update[k] = nil
		}
	}
	maps.Copy(update, want)
	return want, update
}

// KeyStatus is how one key of a file stands against a write.
type KeyStatus string

const (
	// KeySame is a key the write sets to what the file already holds.
	KeySame KeyStatus = "same"
	// KeyDiffers is a key the write would change or remove.
	KeyDiffers KeyStatus = "differs"
	// KeyKept is a key the write leaves alone: not one it is given, or one
	// the policy preserves.
	KeyKept KeyStatus = "kept"
)

// KeyState is one key of a file, as Compare reads it.
type KeyState struct {
	Key     string
	Current []string
	// Desired is what the key holds after the write; Current for a kept key.
	Desired []string
	Status  KeyStatus
}

// Compare reads the file at path and reports, key by key, what a Write of
// tags under p would change, in the format's own keys (an MP3's performers
// as PERFORMER:<INSTRUMENT>). A key neither in the file nor set by the write
// is left out. It never writes.
func Compare(path string, tags Tags, p Policy) ([]KeyState, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := AudioExtensions[ext]; !ok {
		return nil, ErrUnsupportedFormat
	}
	if err := openable(path); err != nil {
		return nil, err
	}
	current, err := taglib.ReadTags(path)
	if err != nil {
		return nil, fmt.Errorf("audiotags: read %s: %w", filepath.Base(path), err)
	}
	_, update := plan(ext, current, p.apply(tags), p)
	keys := map[string]bool{}
	for k := range current {
		keys[k] = true
	}
	for k := range update {
		keys[k] = true
	}
	out := make([]KeyState, 0, len(keys))
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		cur := clean(current[k])
		st := KeyState{Key: k, Current: cur, Desired: cur, Status: KeyKept}
		if want, owned := update[k]; owned {
			if len(cur) == 0 && len(want) == 0 {
				continue
			}
			st.Desired = want
			st.Status = KeySame
			if !slices.Equal(cur, want) {
				st.Status = KeyDiffers
			}
		}
		out = append(out, st)
	}
	return out, nil
}

// Edited reports whether any key Compare returned would change.
func Edited(states []KeyState) bool {
	return slices.ContainsFunc(states, func(s KeyState) bool {
		return s.Status == KeyDiffers
	})
}

// writeArt applies art to the copy at path and checks it read back. It
// returns the hash of the front cover Streamline owns afterwards.
func writeArt(path, ext string, art Art) (string, error) {
	idx, current, err := frontCover(path, ext)
	if err != nil {
		return "", err
	}
	foreign := current != nil && ArtHash(current) != art.Ours
	if foreign {
		// The release's own art: never ours to replace or remove.
		return "", nil
	}
	if art.Front == nil {
		if current == nil {
			return "", nil
		}
		if err := taglib.WriteImageOptions(path, nil, idx, "", "", ""); err != nil {
			return "", err
		}
		if _, left, err := frontCover(path, ext); err != nil || left != nil {
			return "", errors.Join(ErrVerify, err)
		}
		return "", nil
	}
	want := ArtHash(art.Front)
	if current != nil && want == art.Ours {
		return want, nil
	}
	mime := http.DetectContentType(art.Front)
	if err := taglib.WriteImageOptions(
		path, art.Front, idx, frontCoverType, OwnArtDescription, mime,
	); err != nil {
		return "", err
	}
	if _, got, err := frontCover(
		path,
		ext,
	); err != nil ||
		!bytes.Equal(got, art.Front) {
		return "", errors.Join(
			fmt.Errorf("%w: the embedded front cover", ErrVerify), err)
	}
	return want, nil
}

const frontCoverType = "Front Cover"

// otherPictureType is ID3's and FLAC's catch-all picture type, which many
// rips give the release's cover.
const otherPictureType = "Other"

// frontCover finds the file's front cover: the picture typed so; else the
// first one typed Other, which a rip that types nothing gives its cover (a
// second, Streamline-typed front cover beside it would be the one players
// show, and Picture would then return Streamline's copy instead of the
// release's own art); else for MP4, which types none, the first one. It
// returns the index to write it at (one past the last picture when there is
// none, which appends) and its bytes, nil when there is none.
func frontCover(path, ext string) (int, []byte, error) {
	props, err := taglib.ReadProperties(path)
	if err != nil {
		return 0, nil, err
	}
	idx := slices.IndexFunc(props.Images, func(d taglib.ImageDesc) bool {
		return d.Type == frontCoverType
	})
	if idx < 0 {
		idx = slices.IndexFunc(props.Images, func(d taglib.ImageDesc) bool {
			return d.Type == otherPictureType || d.Type == ""
		})
	}
	if idx < 0 && ext == ".m4a" && len(props.Images) > 0 {
		idx = 0
	}
	if idx < 0 {
		return len(props.Images), nil, nil
	}
	data, err := taglib.ReadImageOptions(path, idx)
	if err != nil {
		return 0, nil, err
	}
	if len(data) == 0 {
		return idx, nil, nil
	}
	return idx, data, nil
}

const performerPrefix = taglib.Performer + ":"

// performerValue splits Picard's "Name (instrument)".
var performerValue = regexp.MustCompile(`^(.+?) \(([^()]+)\)$`)

// fold returns tags as the format stores them: values trimmed and emptied
// out, the totals folded into the number for ID3 and MP4, and the performers
// in the format's own form — for ID3 one PERFORMER:<INSTRUMENT> key per
// instrument, elsewhere Picard's "Name (instrument)" under PERFORMER.
//
// A PERFORMER:<INSTRUMENT> key given explicitly (a hand-set override) names
// every performer of that instrument: the PERFORMER list's entries for it
// are dropped rather than merged in, so the override replaces them.
func fold(ext string, tags Tags) map[string][]string {
	out := make(map[string][]string, len(tags))
	explicit := map[string]bool{}
	for k, vs := range tags {
		out[k] = clean(vs)
		if strings.HasPrefix(strings.ToUpper(k), performerPrefix) {
			explicit[strings.ToUpper(k)] = true
		}
	}
	if vs, ok := out[taglib.Performer]; ok {
		var plain []string
		for _, v := range vs {
			m := performerValue.FindStringSubmatch(v)
			if m == nil {
				plain = append(plain, v)
				continue
			}
			key := performerPrefix + strings.ToUpper(m[2])
			switch {
			case explicit[key]:
				// The override names this instrument's performers.
			case ext == ".mp3":
				if !slices.Contains(out[key], m[1]) {
					out[key] = append(out[key], m[1])
				}
			default:
				plain = append(plain, v)
			}
		}
		if ext != ".mp3" {
			// No per-instrument key outside ID3: the override's names go
			// into the list in Picard's form, and the key is deleted.
			for _, key := range slices.Sorted(maps.Keys(explicit)) {
				in := strings.ToLower(strings.TrimPrefix(key, performerPrefix))
				for _, name := range out[key] {
					if v := name + " (" + in + ")"; !slices.Contains(plain, v) {
						plain = append(plain, v)
					}
				}
				out[key] = nil
			}
		}
		out[taglib.Performer] = plain
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
// final rename is atomic, and returns it with the original's file info. The
// copy keeps the extension: TagLib picks the format from it.
func siblingCopy(path, ext string) (string, os.FileInfo, error) {
	//nolint:gosec // the caller supplies the path; this package rewrites library files by design
	src, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		return "", nil, err
	}
	dst, err := os.CreateTemp(filepath.Dir(path), TempPrefix+"*"+ext)
	if err != nil {
		return "", nil, err
	}
	_, err = io.Copy(dst, src)
	err = errors.Join(err, dst.Close())
	if err != nil {
		_ = os.Remove(dst.Name())
		return "", nil, err
	}
	return dst.Name(), st, nil
}
