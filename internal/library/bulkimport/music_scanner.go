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
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/metadata"
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
	}
	if h.ReleaseDate != nil {
		c.Year = numeric.SaturateU16(h.ReleaseDate.Year())
	}
	return c
}

// albumFolder is one directory that directly holds audio files.
type albumFolder struct {
	path  string
	files []string
}

func walkAlbumFolders(
	ctx context.Context, root string, skip map[string]struct{},
) ([]albumFolder, int) {
	byDir := map[string][]string{}
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
			byDir[dir] = append(byDir[dir], path)
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
		out = append(out, albumFolder{path: dir, files: byDir[dir]})
	}
	return out, walkErrors
}

// majorityTags returns the most common non-empty (artist, album) pair across
// the folder's files, preferring album artist over track artist. Empty when no
// file carries an album tag.
func majorityTags(ctx context.Context, files []string) (string, string, int) {
	type pair struct{ artist, album string }
	counts := map[pair]int{}
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
	}
	var best pair
	bestN := 0
	for p, n := range counts {
		if n > bestN || (n == bestN && p.album < best.album) {
			best, bestN = p, n
		}
	}
	return best.artist, best.album, readErrors
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

		artist, album, readErrors := majorityTags(ctx, f.files)
		walkErrors += readErrors
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
	countCommit(ctx, "music", "walk_error", int64(walkErrors))
	countCommit(ctx, "music", "musicbrainz_lookup_error", int64(lookupErrors))
}
