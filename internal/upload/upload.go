// Package upload receives media pushed over HTTP into a staging folder under
// the download path, where the importer picks it up like a finished download.
package upload

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/library/ebookmeta"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/sysinfo"
)

type Kind string

const (
	KindAlbum     Kind = "album"
	KindTrack     Kind = "track"
	KindArtist    Kind = "artist"
	KindEbook     Kind = "ebook"
	KindAudiobook Kind = "audiobook"
)

const (
	rootName      = ".uploads"
	partialSuffix = ".partial"
	cueExt        = ".cue"
	wavExt        = ".wav"
	gib           = int64(1) << 30
	maxNameBytes  = 255
	maxNamedFiles = 5
)

var (
	ErrRejected = errors.New("upload rejected")
	ErrConflict = errors.New("upload target busy")
	ErrNoSpace  = errors.New("not enough free space for the upload")
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/upload")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/upload")

	uploads metric.Int64Counter

	liveMu sync.Mutex
	live   = map[string]struct{}{}
)

func init() {
	uploads = otelx.Must(meter.Int64Counter(
		"streamline.uploads",
		metric.WithDescription("Media uploads by kind and outcome"),
	))
	uploads.Add(context.Background(), 0)
}

// File is one staged file; Path is relative to Staged.Dir and slash-separated.
type File struct {
	Path string
	Size int64
}

type Staged struct {
	// Dir is absolute and becomes the record's save_path.
	Dir   string
	Files []File

	name string
}

// Root is <download_path>/.uploads.
func Root() string {
	return filepath.Join(config.Get().Library.DownloadPath, rootName)
}

func markLive(names ...string) {
	liveMu.Lock()
	defer liveMu.Unlock()
	for _, n := range names {
		live[n] = struct{}{}
	}
}

func unmarkLive(names ...string) {
	liveMu.Lock()
	defer liveMu.Unlock()
	for _, n := range names {
		delete(live, n)
	}
}

func isLive(name string) bool {
	liveMu.Lock()
	defer liveMu.Unlock()
	_, ok := live[name]
	return ok
}

// Done removes Dir from the live set.
func (s *Staged) Done() {
	unmarkLive(s.name, s.name+partialSuffix)
}

// Discard removes Dir from disk too.
func (s *Staged) Discard() {
	if err := os.RemoveAll(s.Dir); err != nil {
		slog.WarnContext(
			context.Background(),
			"remove staged upload",
			"dir",
			s.Dir,
			"error",
			err,
		)
	}
	s.Done()
}

// Drop deletes the named files (paths as in Files) and rewrites Files.
func (s *Staged) Drop(paths []string) error {
	drop := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		drop[p] = struct{}{}
	}
	var firstErr error
	kept := make([]File, 0, len(s.Files))
	for _, f := range s.Files {
		if _, ok := drop[f.Path]; !ok {
			kept = append(kept, f)
			continue
		}
		full := filepath.Join(s.Dir, filepath.FromSlash(f.Path))
		if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			kept = append(kept, f)
			firstErr = cmpErr(firstErr, fmt.Errorf("drop %s: %w", f.Path, err))
			continue
		}
		removeEmptyParents(s.Dir, filepath.Dir(full))
	}
	s.Files = kept
	return firstErr
}

func cmpErr(first, next error) error {
	if first != nil {
		return first
	}
	return next
}

func removeEmptyParents(root, dir string) {
	for dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)) {
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// freeBudget is the bytes an upload may use: free space minus the grab
// reserve. ok is false when the check is off or the volume cannot be probed. Measured once per
// upload, so concurrent uploads can together overrun it: the write then fails
// on a full disk and its staging is removed. Tally live bytes here if that
// starts to matter.
func freeBudget() (int64, bool) {
	if !config.Get().Download.SpaceCheck {
		return 0, false
	}
	usage := sysinfo.DiskUsageFor(config.Get().Library.DownloadPath)
	if usage == nil {
		return 0, false
	}
	return usage.FreeBytes - int64(config.Get().Download.SpaceReserveGB)*gib, true
}

// CheckSpace is the refusal that needs no body: an instance whose staging lies
// outside the allowed roots, or a declared length that does not fit; length < 0
// means undeclared.
func CheckSpace(length int64) error {
	if err := checkRoots(); err != nil {
		return err
	}
	if length < 0 {
		return nil
	}
	if budget, ok := freeBudget(); ok && length > budget {
		return ErrNoSpace
	}
	return nil
}

// classifyFailure sorts a failed receive by whose fault it is: what the client
// sent or stopped sending is a refusal naming it, not a server error, and a disk
// that filled up since the budget was read is the no-space answer.
func classifyFailure(rel string, err error) error {
	switch {
	case errors.Is(err, ErrRejected), errors.Is(err, ErrNoSpace):
		return err
	case errors.Is(err, syscall.ENOSPC):
		return ErrNoSpace
	case errors.Is(err, syscall.ENAMETOOLONG),
		errors.Is(err, syscall.ENOTDIR),
		errors.Is(err, syscall.EISDIR),
		errors.Is(err, os.ErrExist):
		return rejected("%s clashes with another file of the upload", rel)
	case errors.Is(err, io.EOF),
		errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, os.ErrDeadlineExceeded),
		errors.Is(err, context.Canceled):
		return rejected("the upload was cut off")
	}
	return err
}

func rejected(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrRejected, fmt.Sprintf(format, a...))
}

func cleanName(raw string) (string, error) {
	if raw == "" {
		return "", rejected("a part has no file name")
	}
	if strings.ContainsRune(raw, 0) {
		return "", rejected("file name %q contains a NUL byte", raw)
	}
	name := strings.ReplaceAll(raw, `\`, "/")
	if strings.HasPrefix(name, "/") {
		return "", rejected("file name %q is absolute", raw)
	}
	if len(name) >= 2 && name[1] == ':' {
		return "", rejected("file name %q starts with a drive letter", raw)
	}
	for seg := range strings.SplitSeq(name, "/") {
		switch seg {
		case "":
			return "", rejected("file name %q has an empty path segment", raw)
		case "..":
			return "", rejected("file name %q climbs out of the upload", raw)
		}
		if len(seg) > maxNameBytes {
			return "", rejected("a file name is over %d bytes", maxNameBytes)
		}
	}
	cleaned := path.Clean(name)
	if cleaned == "." {
		return "", rejected("file name %q names no file", raw)
	}
	return cleaned, nil
}

// partFileName reads the filename parameter itself: Part.FileName reduces it
// to its base name, which would lose the folder structure.
func partFileName(p *multipart.Part) string {
	_, params, err := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
	if err != nil {
		return ""
	}
	return params["filename"]
}

// Verdict is what an upload of a kind does with one file.
type Verdict int

const (
	Ignore Verdict = iota
	Keep
	Refuse
)

// Classify decides by the file's name alone, so a preview and the receiver
// cannot disagree. wav says a music upload's WAV files convert on import
// (library.ConvertsWAV); otherwise one is refused like any media out of place.
func Classify(kind Kind, name string, wav bool) Verdict {
	base := path.Base(name)
	if strings.HasPrefix(base, ".") {
		return Ignore
	}
	ext := strings.ToLower(path.Ext(base))
	// A PDF in an album folder is its booklet, not a book filed in the wrong place.
	if ext == ".pdf" && kind != KindEbook && kind != KindAudiobook {
		return Ignore
	}
	var kept bool
	switch kind {
	case KindEbook:
		_, kept = ebookmeta.EbookExtensions[ext]
	case KindAudiobook:
		_, kept = ebookmeta.AudiobookExtensions[ext]
	default:
		_, kept = audiotags.AudioExtensions[ext]
		kept = kept || ext == cueExt || (wav && ext == wavExt)
	}
	if kept {
		return Keep
	}
	_, audio := audiotags.AudioExtensions[ext]
	_, ebook := ebookmeta.EbookExtensions[ext]
	_, audiobook := ebookmeta.AudiobookExtensions[ext]
	if audio || ebook || audiobook || ext == wavExt || library.MediaExts[ext] {
		return Refuse
	}
	return Ignore
}

// CheckNames applies the receiver's per-file refusals to a list of names, so a
// preview can answer them before any byte is sent.
func CheckNames(kind Kind, names []string, wav bool) error {
	for _, raw := range names {
		rel, err := cleanName(raw)
		if err != nil {
			return err
		}
		if Classify(kind, rel, wav) == Refuse {
			return refusedFile(rel)
		}
	}
	return nil
}

func refusedFile(rel string) error {
	return rejected("%s is not a file this upload takes", rel)
}

func checkRoots() error {
	roots := config.Get().Library.AllowedDownloadRoots
	if len(roots) == 0 {
		return nil
	}
	for _, r := range roots {
		if library.PathUnderRoot(Root(), r) {
			return nil
		}
	}
	return rejected(
		"uploads land under download_path, which no allowed_download_roots entry covers",
	)
}

func newName() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("upload: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// Receive streams every part of mr into a fresh staging dir.
func Receive(
	ctx context.Context,
	mr *multipart.Reader,
	kind Kind,
	wav bool,
) (*Staged, error) {
	ctx, span := tracer.Start(ctx, "upload.receive")
	defer span.End()
	span.SetAttributes(attribute.String("upload.kind", string(kind)))

	staged, err := receive(ctx, mr, kind, wav)
	outcome := "staged"
	switch {
	case err == nil:
		var total int64
		for _, f := range staged.Files {
			total += f.Size
		}
		span.SetAttributes(
			attribute.Int64("upload.bytes", total),
			attribute.Int("upload.files", len(staged.Files)),
		)
	case errors.Is(err, ErrRejected):
		outcome = "rejected"
	case errors.Is(err, ErrNoSpace):
		outcome = "no_space"
	default:
		outcome = "failed"
	}
	uploads.Add(ctx, 1, metric.WithAttributes(
		attribute.String("kind", string(kind)),
		attribute.String("outcome", outcome),
	))
	return staged, otelx.RecordSpanError(span, err)
}

func receive(
	ctx context.Context,
	mr *multipart.Reader,
	kind Kind,
	wav bool,
) (*Staged, error) {
	if err := checkRoots(); err != nil {
		return nil, err
	}
	budget, bounded := freeBudget()

	root := Root()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create upload root: %w", err)
	}
	name := newName()
	partial := filepath.Join(root, name+partialSuffix)
	markLive(name, name+partialSuffix)
	fail := func(err error) (*Staged, error) {
		if rmErr := os.RemoveAll(partial); rmErr != nil {
			slog.WarnContext(
				ctx,
				"remove partial upload",
				"dir",
				partial,
				"error",
				rmErr,
			)
		}
		unmarkLive(name, name+partialSuffix)
		return nil, err
	}
	if err := os.Mkdir(partial, 0o700); err != nil {
		return fail(fmt.Errorf("create staging dir: %w", err))
	}

	var files []File
	seen := map[string]struct{}{}
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		part, err := mr.NextPart()
		// Only the closing boundary answers a bare EOF. A body that stops short of
		// it wraps one, and errors.Is would read that as a complete upload, losing
		// every file still to come.
		if err == io.EOF { //nolint:errorlint // see above
			break
		}

		if malformed, ok := errors.AsType[textproto.ProtocolError](err); ok {
			return fail(rejected("%s", malformed))
		}
		if err != nil {
			return fail(classifyFailure("", fmt.Errorf("read upload: %w", err)))
		}
		raw := partFileName(part)
		if raw == "" {
			if _, err := io.Copy(io.Discard, part); err != nil {
				return fail(classifyFailure("", fmt.Errorf("read upload: %w", err)))
			}
			continue
		}
		rel, err := cleanName(raw)
		if err != nil {
			return fail(err)
		}
		switch Classify(kind, rel, wav) {
		case Refuse:
			return fail(refusedFile(rel))
		case Ignore:
			if _, err := io.Copy(io.Discard, part); err != nil {
				return fail(classifyFailure(rel, fmt.Errorf("read upload: %w", err)))
			}
			continue
		}
		if _, dup := seen[rel]; dup {
			return fail(rejected("%s appears twice", rel))
		}
		seen[rel] = struct{}{}

		n, err := writePart(partial, rel, part, &written, budget, bounded)
		if err != nil {
			return fail(classifyFailure(rel, err))
		}
		files = append(files, File{Path: rel, Size: n})
	}

	if !slices.ContainsFunc(
		files,
		func(f File) bool { return !strings.EqualFold(path.Ext(f.Path), cueExt) },
	) {
		return fail(rejected("the upload holds no media file"))
	}

	if err := checkAudio(partial, kind, files); err != nil {
		return fail(err)
	}

	final := filepath.Join(root, name)
	if err := os.Rename(partial, final); err != nil {
		return fail(fmt.Errorf("finish staging dir: %w", err))
	}
	slices.SortFunc(
		files,
		func(a, b File) int { return strings.Compare(a.Path, b.Path) },
	)
	return &Staged{Dir: final, Files: files, name: name}, nil
}

// checkAudio refuses files named as audio that hold none. It runs once every
// byte is in, not as each file lands: a refusal sent to a client still writing
// reaches a browser as a network error, and naming every bad file at once spares
// sending the rest again for each one.
func checkAudio(dir string, kind Kind, files []File) error {
	if kind == KindEbook {
		return nil
	}
	var bad []string
	for _, f := range files {
		if strings.EqualFold(path.Ext(f.Path), cueExt) {
			continue
		}
		if !audiotags.HoldsAudio(filepath.Join(dir, filepath.FromSlash(f.Path))) {
			bad = append(bad, f.Path)
		}
	}
	switch {
	case len(bad) == 0:
		return nil
	case len(bad) > maxNamedFiles:
		return rejected("%s and %d more are not readable audio files",
			strings.Join(bad[:maxNamedFiles], ", "), len(bad)-maxNamedFiles)
	}
	return rejected("%s: not a readable audio file", strings.Join(bad, ", "))
}

func writePart(
	dir, rel string,
	src io.Reader,
	written *int64,
	budget int64,
	bounded bool,
) (int64, error) {
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return 0, fmt.Errorf("create %s: %w", rel, err)
	}
	//nolint:gosec // full is built from a name cleanName has confined to dir
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", rel, err)
	}
	w := &boundedWriter{w: f, written: written, budget: budget, bounded: bounded}
	n, err := io.Copy(w, src)
	if err != nil {
		if closeErr := f.Close(); closeErr != nil {
			slog.WarnContext(
				context.Background(),
				"close partial file",
				"file",
				rel,
				"error",
				closeErr,
			)
		}
		if errors.Is(err, ErrNoSpace) {
			return 0, err
		}
		return 0, fmt.Errorf("write %s: %w", rel, err)
	}
	if err := f.Sync(); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			slog.WarnContext(
				context.Background(),
				"close partial file",
				"file",
				rel,
				"error",
				closeErr,
			)
		}
		return 0, fmt.Errorf("sync %s: %w", rel, err)
	}
	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("close %s: %w", rel, err)
	}
	return n, nil
}

type boundedWriter struct {
	w       io.Writer
	written *int64
	budget  int64
	bounded bool
}

func (b *boundedWriter) Write(p []byte) (int, error) {
	if b.bounded && *b.written+int64(len(p)) > b.budget {
		return 0, ErrNoSpace
	}
	n, err := b.w.Write(p)
	*b.written += int64(n)
	return n, err
}
