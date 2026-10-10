package bulkimport

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanbook "github.com/datahearth/streamline/ent/importscanbook"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library/ebookmeta"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
)

const (
	bookSidecarName    = "metadata.opf"
	bookProgressLogGap = 25
)

var bracketedSuffix = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)

// scannedBookCandidate is one detected book before identification: an ebook file
// group or an audiobook folder.
type scannedBookCandidate struct {
	slot  entimportscanbook.Slot
	dir   string
	paths []string
}

func (s *Service) runScanBooks(ctx context.Context, scan *ent.ImportScan) {
	ctx, span := tracer.Start(ctx, "bulkimport.scan_books",
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

	candidates, walkErrors, err := walkBookSource(ctx, scan.SourcePath)
	if err != nil {
		otelx.RecordSpanError(span, err)
		s.markScanFailed(ctx, scan.ID, err.Error())
		return
	}
	total := len(candidates)
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scan.ID,
		entimportscan.StatusRunning,
		db.UpdateScanStatusOpts{TotalCount: &total},
	); err != nil {
		slog.WarnContext(ctx, "book scan: failed to set total_count",
			"scan.id", scan.ID, "error", err)
	}

	indexed := map[entimportscanbook.Slot]map[uint32]uint32{}
	for _, slot := range []entimportscanbook.Slot{
		entimportscanbook.SlotEbook, entimportscanbook.SlotAudiobook,
	} {
		idx, err := s.store.BookHardcoverIndex(ctx, string(slot))
		if err != nil {
			slog.WarnContext(ctx, "book scan: tracked-book lookup failed",
				"scan.id", scan.ID, "slot", slot, "error", err)
			idx = map[uint32]uint32{}
		}
		indexed[slot] = idx
	}

	queue := make([]db.CreateImportScanBookParams, 0, total)
	tally := map[entimportscanbook.Classification]int{}
	var lookupErrors int
	budget, _ := s.bookmeta.(metadata.Budgeter)
	lastPoll := time.Now()
	for i, cand := range candidates {
		if budget != nil && budget.Remaining() <= budget.ScanReserve() {
			reason := budgetExhaustedReason(
				ctx,
				time.Until(nextUTCMidnight(time.Now())),
			)
			otelx.RecordSpanError(span, errors.New(reason))
			s.markScanFailed(ctx, scan.ID, reason)
			return
		}
		if time.Since(lastPoll) > cancellationPollEvery {
			lastPoll = time.Now()
			cur, ferr := s.store.FindImportScan(ctx, scan.ID)
			if ferr == nil && cur.Status != entimportscan.StatusRunning {
				return
			}
		}

		info := readBookInfo(ctx, cand)
		c, errs, cerr := s.classifyBookCandidate(ctx, info, indexed[cand.slot])
		if cerr != nil {
			var rl *metadata.RateLimitedError
			errors.As(cerr, &rl)
			reason := rateLimitedReason(ctx, rl)
			otelx.RecordSpanError(span, errors.New(reason))
			s.markScanFailed(ctx, scan.ID, reason)
			return
		}
		lookupErrors += errs
		tally[c.Kind]++
		queue = append(queue, db.CreateImportScanBookParams{
			FilePaths:       cand.paths,
			Slot:            cand.slot,
			ParsedTitle:     info.Title,
			ParsedAuthor:    info.Author,
			ParsedISBN:      info.ISBN,
			Classification:  c.Kind,
			BookHardcoverID: c.BookHardcoverID,
			Candidates:      c.Candidates,
			ExistingBookID:  existingID(c.ExistingBookID),
		})

		if err := s.store.IncrementImportScanProgress(ctx, scan.ID, 1); err != nil {
			slog.WarnContext(ctx, "book scan: failed to increment progress",
				"scan.id", scan.ID, "error", err)
		}
		if (i+1)%bookProgressLogGap == 0 {
			slog.InfoContext(ctx, "book scan progress",
				"scan.id", scan.ID, "scan.done", i+1, "scan.total", total)
		}
	}

	if err := s.store.BulkCreateImportScanBooks(ctx, scan.ID, queue); err != nil {
		otelx.RecordSpanError(span, err)
		s.markScanFailed(ctx, scan.ID, err.Error())
		return
	}

	scannedAt := time.Now()
	queued := len(queue)
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scan.ID,
		entimportscan.StatusAwaitingReview,
		db.UpdateScanStatusOpts{ScannedAt: &scannedAt, TotalCount: &queued},
	); err != nil {
		slog.ErrorContext(ctx, "book scan: failed to flip scan to awaiting_review",
			"scan.id", scan.ID, "error", err)
	}
	slog.InfoContext(ctx, "book scan finished",
		"scan.id", scan.ID,
		"books.queued", queued,
		"scan.confirmed", tally[entimportscanbook.ClassificationConfirmed],
		"scan.existing", tally[entimportscanbook.ClassificationExisting],
		"scan.ambiguous", tally[entimportscanbook.ClassificationAmbiguous],
		"scan.unmatched", tally[entimportscanbook.ClassificationUnmatched],
		"scan.walk_errors", walkErrors,
		"scan.hardcover_lookup_errors", lookupErrors)
	for kind, n := range tally {
		scanClassified.Add(ctx, int64(n), metric.WithAttributes(
			attribute.String("classification", string(kind)),
			attribute.String("kind", "book"),
		))
	}
	countCommit(ctx, "book", "walk_error", int64(walkErrors))
	countCommit(ctx, "book", "hardcover_lookup_error", int64(lookupErrors))
}

func nextUTCMidnight(now time.Time) time.Time {
	return now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
}

func budgetExhaustedReason(ctx context.Context, resetsIn time.Duration) string {
	reason := fmt.Sprintf(
		"Hardcover's daily request budget is spent (the rest is kept for adds and refreshes); "+
			"it resets at 00:00 UTC, in about %s. Re-run the scan then.",
		resetsIn.Round(time.Minute),
	)
	slog.WarnContext(ctx, "book scan stopped: hardcover daily budget spent",
		"resets_in", resetsIn.Round(time.Minute))
	return reason
}

func rateLimitedReason(ctx context.Context, rl *metadata.RateLimitedError) string {
	wait := time.Minute
	if rl != nil {
		wait = rl.RetryAfter
	}
	reason := fmt.Sprintf(
		"Hardcover rate limit reached; try again in about %s. Re-run the scan then.",
		wait.Round(time.Second),
	)
	slog.WarnContext(ctx, "book scan stopped: hardcover rate limited",
		"retry_after", wait.Round(time.Second))
	return reason
}

func existingID(id uint32) *uint32 {
	if id == 0 {
		return nil
	}
	return &id
}

// walkBookSource finds ebook groups and audiobook folders in one pass, in a
// stable order so a rescan lists rows the same way.
func walkBookSource(
	ctx context.Context, root string,
) ([]scannedBookCandidate, int, error) {
	type groupKey struct{ dir, stem string }
	ebooks := map[groupKey][]string{}
	audio := map[string][]string{}
	var walkErrors int

	if err := filepath.WalkDir(
		root,
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if path == root {
					return err
				}
				walkErrors++
				slog.WarnContext(ctx, "book scan: walk error",
					"path", path, "error", err)
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			dir := filepath.Dir(path)
			if _, ok := ebookmeta.EbookExtensions[ext]; ok {
				k := groupKey{
					dir,
					strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
				}
				ebooks[k] = append(ebooks[k], path)
			} else if _, ok := ebookmeta.AudiobookExtensions[ext]; ok {
				audio[dir] = append(audio[dir], path)
			}
			return nil
		},
	); err != nil {
		return nil, 0, fmt.Errorf("walk source path: %w", err)
	}

	out := make([]scannedBookCandidate, 0, len(ebooks)+len(audio))
	for k, paths := range ebooks {
		sort.Strings(paths)
		out = append(out, scannedBookCandidate{
			slot: entimportscanbook.SlotEbook, dir: k.dir, paths: paths,
		})
	}
	for dir, paths := range audio {
		sort.Strings(paths)
		out = append(out, scannedBookCandidate{
			slot: entimportscanbook.SlotAudiobook, dir: dir, paths: paths,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].dir != out[j].dir {
			return out[i].dir < out[j].dir
		}
		if out[i].slot != out[j].slot {
			return out[i].slot < out[j].slot
		}
		return out[i].paths[0] < out[j].paths[0]
	})
	return out, walkErrors, nil
}

// readBookInfo resolves a candidate's title, author and ISBN: a Calibre
// sidecar first, then an embedded epub OPF, then the names on disk. Each
// source only fills what the one before left empty.
func readBookInfo(ctx context.Context, cand scannedBookCandidate) ebookmeta.Info {
	var info ebookmeta.Info

	sidecar := filepath.Join(cand.dir, bookSidecarName)
	if _, err := os.Stat(sidecar); err == nil {
		got, rerr := ebookmeta.ReadSidecar(sidecar)
		if rerr != nil {
			slog.WarnContext(ctx, "book scan: unreadable sidecar",
				"path", sidecar, "error", rerr)
		} else {
			info = mergeBookInfo(info, got)
		}
	}

	if cand.slot == entimportscanbook.SlotEbook && info.Title == "" {
		for _, p := range cand.paths {
			if strings.ToLower(filepath.Ext(p)) != ".epub" {
				continue
			}
			got, rerr := ebookmeta.ReadEpub(p)
			if rerr != nil {
				slog.WarnContext(ctx, "book scan: unreadable epub",
					"path", p, "error", rerr)
				continue
			}
			info = mergeBookInfo(info, got)
			break
		}
	}

	if info.Title == "" || info.Author == "" {
		title, author := guessBookTitleAuthor(
			guessSource(cand), cand.slot == entimportscanbook.SlotAudiobook,
		)
		info = mergeBookInfo(info, ebookmeta.Info{Title: title, Author: author})
	}
	return info
}

func guessSource(cand scannedBookCandidate) string {
	if cand.slot == entimportscanbook.SlotAudiobook {
		return cand.dir
	}
	return cand.paths[0]
}

func mergeBookInfo(base, extra ebookmeta.Info) ebookmeta.Info {
	if base.Title == "" {
		base.Title = extra.Title
	}
	if base.Author == "" {
		base.Author = extra.Author
	}
	if base.ISBN == "" {
		base.ISBN = extra.ISBN
	}
	if base.Year == 0 {
		base.Year = extra.Year
	}
	return base
}

// guessBookTitleAuthor derives names from a path when no metadata exists. An
// ebook stem is read as Calibre writes it, "<Title> - <Author>"; anything
// else, and every audiobook, falls back to <author>/<title> from the last two
// path segments. For an audiobook path is the folder, for an ebook a file.
func guessBookTitleAuthor(path string, audiobook bool) (title, author string) {
	if audiobook {
		return cleanBookName(filepath.Base(path)),
			cleanBookName(filepath.Base(filepath.Dir(path)))
	}
	stem := cleanBookName(
		strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
	)
	if t, a, ok := strings.Cut(stem, " - "); ok {
		return strings.TrimSpace(t), strings.TrimSpace(a)
	}
	return stem, cleanBookName(filepath.Base(filepath.Dir(path)))
}

func cleanBookName(name string) string {
	return strings.TrimSpace(bracketedSuffix.ReplaceAllString(name, ""))
}

// classifyBookCandidate identifies one candidate. The second return is the
// number of failed Hardcover calls, for the scan's hygiene counters; the third
// is non-nil only when Hardcover rate-limited the lookup, which ends the scan.
func (s *Service) classifyBookCandidate(
	ctx context.Context,
	info ebookmeta.Info,
	indexed map[uint32]uint32,
) (BookClassification, int, error) {
	var errs int

	if info.ISBN != "" {
		id, err := s.bookmeta.BookByISBN(ctx, info.ISBN)
		switch {
		case errors.Is(err, metadata.ErrRateLimited):
			return BookClassification{}, errs, err
		case err != nil:
			errs++
			slog.WarnContext(ctx, "book scan: isbn lookup failed",
				"isbn", info.ISBN, "error", err)
		case id != 0:
			return classifyResolvedBook(
				id,
				schema.ScannedBookCandidate{BookHardcoverID: id},
				indexed,
			), errs, nil
		}
	}

	if info.Title == "" {
		return BookClassification{
			Kind: entimportscanbook.ClassificationUnmatched,
		}, errs, nil
	}
	hits, err := s.bookmeta.SearchBooks(
		ctx,
		strings.TrimSpace(info.Title+" "+info.Author),
	)
	if errors.Is(err, metadata.ErrRateLimited) {
		return BookClassification{}, errs, err
	}
	if err != nil {
		errs++
		slog.WarnContext(ctx, "book scan: hardcover search failed",
			"title", info.Title, "error", err)
	}
	c := ClassifyBook(info.Title, info.Author, hits, indexed)
	return c, errs, nil
}
