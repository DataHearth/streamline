package bulkimport

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanalbum "github.com/datahearth/streamline/ent/importscanalbum"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

const (
	albumCandidateLimit = 5
	// albumConfirmScore is MusicBrainz's own search score above which a lone
	// hit is trusted without review.
	albumConfirmScore  = 95
	musicProgressEvery = 25
)

// AlbumClassification is the music analogue of ShowClassification.
type AlbumClassification struct {
	Kind             entimportscanalbum.Classification
	ReleaseGroupMBID string
	ArtistMBID       string
	ExistingAlbumID  uint32
	Candidates       []schema.ScannedAlbumCandidate
}

// ClassifyAlbum buckets MusicBrainz release-group hits for one folder. The
// MusicBrainz score is the ranking: exactly one hit at or above
// albumConfirmScore is confirmed (existing when the library already holds that
// release group); several strong hits or only weak ones need a reviewer.
func ClassifyAlbum(
	hits []metadata.ReleaseGroupSearchResult,
	albumByMBID map[string]uint32,
) AlbumClassification {
	if len(hits) == 0 {
		return AlbumClassification{Kind: entimportscanalbum.ClassificationUnmatched}
	}
	cands := make([]schema.ScannedAlbumCandidate, 0, albumCandidateLimit)
	var strong []metadata.ReleaseGroupSearchResult
	for _, h := range hits {
		if h.Score >= albumConfirmScore {
			strong = append(strong, h)
		}
		if len(cands) < albumCandidateLimit {
			cands = append(cands, albumCandidate(h))
		}
	}
	if len(strong) != 1 {
		return AlbumClassification{
			Kind:       entimportscanalbum.ClassificationAmbiguous,
			Candidates: cands,
		}
	}
	only := strong[0]
	c := AlbumClassification{
		Kind:             entimportscanalbum.ClassificationConfirmed,
		ReleaseGroupMBID: only.MBID,
		ArtistMBID:       only.ArtistMBID,
		Candidates:       []schema.ScannedAlbumCandidate{albumCandidate(only)},
	}
	if id, ok := albumByMBID[only.MBID]; ok {
		c.Kind = entimportscanalbum.ClassificationExisting
		c.ExistingAlbumID = id
	}
	return c
}

func albumCandidate(
	h metadata.ReleaseGroupSearchResult,
) schema.ScannedAlbumCandidate {
	c := schema.ScannedAlbumCandidate{
		ReleaseGroupMBID: h.MBID,
		ArtistMBID:       h.ArtistMBID,
		Title:            h.Title,
		Artist:           h.ArtistName,
		Type:             string(h.Type),
	}
	if h.ReleaseDate != nil {
		c.Year = numeric.SaturateU16(h.ReleaseDate.Year())
	}
	return c
}

// albumFolder is one album: a directory that directly holds audio files, with
// the files of its disc subfolders (CD1, Disc 2) folded in.
type albumFolder struct {
	path  string
	files []string
	size  int64
}

func walkAlbumFolders(
	ctx context.Context, root string, skip map[string]struct{},
) ([]albumFolder, int) {
	// WalkDir yields cleaned paths; an unclean root (a trailing slash) would never
	// equal a child's parent and fold the disc folders into the root itself.
	root = filepath.Clean(root)
	byDir := map[string][]string{}
	sizes := map[string]int64{}
	walkErrors := 0
	// WalkDir only fails here through the callback, which swallows per-entry
	// errors so one unreadable directory does not abort the scan.
	if err := filepath.WalkDir(
		root,
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				walkErrors++
				slog.WarnContext(ctx, "music scan: walk error skipped",
					"path", path, "error", err)
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if _, ok := audiotags.AudioExtensions[strings.ToLower(filepath.Ext(path))]; !ok {
				return nil
			}
			dir := filepath.Dir(path)
			if _, isDisc := library.DiscFolderNumber(filepath.Base(dir)); isDisc &&
				filepath.Dir(dir) != root {
				dir = filepath.Dir(dir)
			}
			byDir[dir] = append(byDir[dir], path)
			if info, ierr := d.Info(); ierr == nil {
				sizes[dir] += info.Size()
			}
			return nil
		},
	); err != nil {
		walkErrors++
		slog.WarnContext(ctx, "music scan: walk failed", "root", root, "error", err)
	}

	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		if _, queued := skip[dir]; queued {
			continue
		}
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)
	out := make([]albumFolder, 0, len(dirs))
	for _, dir := range dirs {
		slices.Sort(byDir[dir])
		out = append(
			out,
			albumFolder{path: dir, files: byDir[dir], size: sizes[dir]},
		)
	}
	return out, walkErrors
}

// folderTags is what a folder's embedded tags agree on.
type folderTags struct {
	artist, album string
	year          uint16
	readErrors    int
}

// majorityTags returns the most common non-empty (artist, album) pair across
// the folder's files, preferring album artist over track artist, and the most
// common year among them. Empty when no file carries an album tag.
func majorityTags(ctx context.Context, files []string) folderTags {
	type pair struct{ artist, album string }
	counts := map[pair]int{}
	years := map[uint16]int{}
	readErrors := 0
	for _, f := range files {
		info, err := audiotags.Read(f)
		if err != nil {
			readErrors++
			slog.WarnContext(ctx, "music scan: tag read failed",
				"file", f, "error", err)
			continue
		}
		if info.Album == "" {
			continue
		}
		artist := info.AlbumArtist
		if artist == "" {
			artist = info.Artist
		}
		counts[pair{artist, info.Album}]++
		if info.Year != 0 {
			years[info.Year]++
		}
	}
	var best pair
	bestN := 0
	for p, n := range counts {
		if n > bestN || (n == bestN && p.album < best.album) {
			best, bestN = p, n
		}
	}
	var year uint16
	yearN := 0
	for y, n := range years {
		if n > yearN || (n == yearN && y < year) {
			year, yearN = y, n
		}
	}
	return folderTags{
		artist: best.artist, album: best.album, year: year, readErrors: readErrors,
	}
}

// folderFormat labels a folder by its first audio file: measured when ffprobe
// is available, else by the extension.
func (s *Service) folderFormat(ctx context.Context, files []string) string {
	if len(files) == 0 {
		return ""
	}
	first := files[0]
	ext := filepath.Ext(first)
	if !s.probing() {
		return library.MusicFormatLabel(ext, nil)
	}
	info, err := s.prober.ProbeAudio(ctx, first)
	if err != nil {
		slog.DebugContext(ctx, "music scan: probe failed, format from extension",
			"file", filepath.Base(first), "error", err)
		return library.MusicFormatLabel(ext, nil)
	}
	return library.MusicFormatLabel(ext, info)
}

// folderNames derives (artist, album) from the last two path segments under
// the scan root, for folders whose files carry no tags.
func folderNames(root, folder string) (string, string) {
	rel, err := filepath.Rel(root, folder)
	if err != nil || rel == "." {
		return "", filepath.Base(folder)
	}
	segs := strings.Split(rel, string(filepath.Separator))
	album := segs[len(segs)-1]
	if len(segs) < 2 {
		return "", album
	}
	return segs[len(segs)-2], album
}

// runScanMusic is the music counterpart of runScanSeries: one review entry per
// folder of audio files, identified from embedded tags and matched against
// MusicBrainz release groups.
func (s *Service) runScanMusic(ctx context.Context, scan *ent.ImportScan) {
	ctx, span := tracer.Start(ctx, "bulkimport.scan_music",
		trace.WithAttributes(
			attribute.Int64("scan.id", int64(scan.ID)),
			attribute.String("scan.mode", string(scan.Mode)),
		))
	defer span.End()

	defer func() {
		if r := recover(); r != nil {
			otelx.RecordSpanError(span, fmt.Errorf("panic: %v", r))
			s.markScanFailed(ctx, scan.ID, fmt.Sprintf("panic: %v", r))
		}
	}()

	pending, err := s.store.ListPendingImportScanAlbumFolders(ctx)
	if err != nil {
		slog.WarnContext(ctx, "music scan: pending-folder lookup failed",
			"scan.id", scan.ID, "error", err)
	}
	skip := make(map[string]struct{}, len(pending))
	for _, p := range pending {
		skip[p] = struct{}{}
	}
	folders, walkErrors := walkAlbumFolders(ctx, scan.SourcePath, skip)

	total := len(folders)
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scan.ID,
		entimportscan.StatusRunning,
		db.UpdateScanStatusOpts{TotalCount: &total},
	); err != nil {
		slog.WarnContext(ctx, "music scan: failed to set total_count",
			"scan.id", scan.ID, "error", err)
	}

	albumByMBID, err := s.store.AlbumMBIDIndex(ctx)
	if err != nil {
		slog.WarnContext(ctx, "music scan: tracked-album lookup failed",
			"scan.id", scan.ID, "error", err)
		albumByMBID = map[string]uint32{}
	}

	queue := make([]db.CreateImportScanAlbumParams, 0, len(folders))
	tally := map[entimportscanalbum.Classification]int{}
	var lookupErrors int
	lastPoll := time.Now()
	for i, f := range folders {
		if time.Since(lastPoll) > cancellationPollEvery {
			lastPoll = time.Now()
			cur, ferr := s.store.FindImportScan(ctx, scan.ID)
			if ferr == nil && cur.Status != entimportscan.StatusRunning {
				return
			}
		}

		tags := majorityTags(ctx, f.files)
		artist, album := tags.artist, tags.album
		walkErrors += tags.readErrors
		if album == "" {
			artist, album = folderNames(scan.SourcePath, f.path)
		}

		var hits []metadata.ReleaseGroupSearchResult
		if album != "" {
			var herr error
			hits, herr = s.musicmeta.SearchReleaseGroups(ctx, artist, album)
			if herr != nil {
				lookupErrors++
				slog.WarnContext(ctx, "music scan: musicbrainz lookup failed",
					"folder", f.path, "error", herr)
				hits = nil
			}
		}
		c := ClassifyAlbum(hits, albumByMBID)
		tally[c.Kind]++
		p := db.CreateImportScanAlbumParams{
			FolderPath:       f.path,
			TaggedArtist:     artist,
			TaggedAlbum:      album,
			Classification:   c.Kind,
			ReleaseGroupMBID: c.ReleaseGroupMBID,
			ArtistMBID:       c.ArtistMBID,
			Candidates:       c.Candidates,
			FileCount:        numeric.SaturateU16(len(f.files)),
			TaggedYear:       tags.year,
			Format:           s.folderFormat(ctx, f.files),
			Size:             f.size,
		}
		if c.ExistingAlbumID != 0 {
			id := c.ExistingAlbumID
			p.ExistingAlbumID = &id
		}
		queue = append(queue, p)

		if err := s.store.IncrementImportScanProgress(ctx, scan.ID, 1); err != nil {
			slog.WarnContext(ctx, "music scan: failed to increment progress",
				"scan.id", scan.ID, "error", err)
		}
		if (i+1)%musicProgressEvery == 0 {
			slog.InfoContext(ctx, "music scan progress",
				"scan.id", scan.ID, "folders.done", i+1, "folders.total", total)
		}
	}

	if len(queue) > 0 {
		if err := s.store.BulkCreateImportScanAlbums(
			ctx,
			scan.ID,
			queue,
		); err != nil {
			otelx.RecordSpanError(span, err)
			s.markScanFailed(ctx, scan.ID, err.Error())
			return
		}
	}

	scannedAt := time.Now()
	queued := len(queue)
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scan.ID,
		entimportscan.StatusAwaitingReview,
		db.UpdateScanStatusOpts{ScannedAt: &scannedAt, TotalCount: &queued},
	); err != nil {
		slog.ErrorContext(ctx, "music scan: failed to flip scan to awaiting_review",
			"scan.id", scan.ID, "error", err)
	}
	slog.InfoContext(ctx, "music scan finished",
		"scan.id", scan.ID,
		"albums.queued", len(queue),
		"scan.confirmed", tally[entimportscanalbum.ClassificationConfirmed],
		"scan.existing", tally[entimportscanalbum.ClassificationExisting],
		"scan.ambiguous", tally[entimportscanalbum.ClassificationAmbiguous],
		"scan.unmatched", tally[entimportscanalbum.ClassificationUnmatched],
		"scan.walk_errors", walkErrors,
		"scan.musicbrainz_lookup_errors", lookupErrors)
	for kind, n := range tally {
		scanClassified.Add(ctx, int64(n), metric.WithAttributes(
			attribute.String("classification", string(kind)),
			attribute.String("kind", "music"),
		))
	}
	countCommit(
		ctx,
		"music",
		entimportscan.SourceFilesystem,
		"walk_error",
		int64(walkErrors),
	)
	countCommit(
		ctx,
		"music",
		entimportscan.SourceFilesystem,
		"musicbrainz_lookup_error",
		int64(lookupErrors),
	)
}
