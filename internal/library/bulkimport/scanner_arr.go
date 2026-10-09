package bulkimport

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/arr"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

// runScanArr fetches an identified library instead of walking a directory.
// Nothing here searches a metadata provider: every title already carries the
// id the provider would have been asked for. sourceURL is the URL as the
// caller gave it; the scan row keeps only its address (storedSourceURL).
func (s *Service) runScanArr(
	ctx context.Context, scan *ent.ImportScan, sourceURL, apiKey string,
) {
	ctx, span := tracer.Start(ctx, "bulkimport.fetch_arr",
		trace.WithAttributes(
			attribute.Int64("scan.id", int64(scan.ID)),
			attribute.String("arr.app", string(scan.Source)),
		))
	defer span.End()

	fail := func(err error) {
		s.markScanFailed(ctx, scan.ID, err.Error())
		slog.WarnContext(ctx, "migration fetch failed",
			"scan.id", scan.ID, "scan.source", scan.Source, "error", err)
		otelx.RecordSpanError(span, err)
	}

	app := arr.Radarr
	if scan.Source == entimportscan.SourceSonarr {
		app = arr.Sonarr
	}
	client, err := s.arrClients.Client(app, sourceURL, apiKey)
	if err != nil {
		fail(err)
		return
	}
	if err := client.TestConnection(ctx); err != nil {
		fail(err)
		return
	}

	var (
		tally  map[string]int
		active bool
	)
	if scan.Source == entimportscan.SourceSonarr {
		tally, active, err = s.fetchSonarr(ctx, scan, client)
	} else {
		tally, active, err = s.fetchRadarr(ctx, scan, client)
	}
	if err != nil {
		fail(err)
		return
	}
	if !active {
		return
	}

	scannedAt := time.Now()
	if err := s.store.UpdateImportScanStatus(
		ctx, scan.ID, entimportscan.StatusAwaitingReview,
		db.UpdateScanStatusOpts{ScannedAt: &scannedAt},
	); err != nil {
		slog.ErrorContext(ctx, "migration: failed to flip scan to awaiting_review",
			"scan.id", scan.ID, "error", err)
	}
	slog.InfoContext(ctx, "migration fetch finished",
		"scan.id", scan.ID,
		"scan.source", scan.Source,
		"scan.confirmed", tally[string(entimportscanfile.ClassificationConfirmed)],
		"scan.existing", tally[string(entimportscanfile.ClassificationExisting)],
		"scan.ambiguous", tally[string(entimportscanfile.ClassificationAmbiguous)])
	kind := "movie"
	if scan.Kind == entimportscan.KindSeries {
		kind = "series"
	}
	for cls, n := range tally {
		scanClassified.Add(ctx, int64(n), metric.WithAttributes(
			attribute.String("classification", cls),
			attribute.String("kind", kind),
			attribute.String("source", string(scan.Source)),
		))
	}
}

// profileFor resolves the source's profile id to a streamline profile name.
// An id the operator never mapped resolves to the empty string, which the
// commit reads as "use the default".
func profileFor(m schema.ScanMappings, id uint32) string {
	for _, p := range m.Profiles {
		if p.SourceID == id {
			return p.Target
		}
	}
	return ""
}

// rootMappings converts the persisted mappings. The two types stay separate
// because internal/arr must not import ent/schema.
func rootMappings(m schema.ScanMappings) []arr.RootMapping {
	out := make([]arr.RootMapping, 0, len(m.Roots))
	for _, r := range m.Roots {
		out = append(out, arr.RootMapping{From: r.From, To: r.To})
	}
	return out
}

// fileUsable reports whether a mapped source path can be committed as-is:
// it exists here, and an in_place scan only records files inside the library
// root. A file failing either is surfaced for review instead of auto-committed
// — the commit adds such a title without its file.
func (s *Service) fileUsable(scan *ent.ImportScan, path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if scan.Mode != entimportscan.ModeInPlace {
		return true
	}
	root, err := s.libraryRoot(scan.Kind)
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	return insideRoot(resolved, root)
}

// stillRunning polls the scan's status so a cancel from the API stops the
// fetch between batches.
//
// It is also checked once the whole-library list is in and again once the
// last row is written: the status write that records the total, and the
// caller's flip to awaiting_review, would otherwise put a scan cancelled
// meanwhile back to running — the list alone takes seconds on a large
// library, and the batch poll never runs for one smaller than a batch.
func (s *Service) stillRunning(ctx context.Context, scanID uint32) bool {
	cur, err := s.store.FindImportScan(ctx, scanID)
	return err != nil || cur.Status == entimportscan.StatusRunning
}

func (s *Service) fetchRadarr(
	ctx context.Context, scan *ent.ImportScan, client arr.Library,
) (map[string]int, bool, error) {
	movies, err := client.Movies(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("list movies: %w", err)
	}
	if !s.stillRunning(ctx, scan.ID) {
		return nil, false, nil
	}
	total := len(movies)
	if err := s.store.UpdateImportScanStatus(
		ctx, scan.ID, entimportscan.StatusRunning,
		db.UpdateScanStatusOpts{TotalCount: &total},
	); err != nil {
		return nil, false, fmt.Errorf("record total: %w", err)
	}

	tracked, err := s.store.MovieTMDBIndex(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("index tracked movies: %w", err)
	}

	roots := rootMappings(scan.Mappings)
	tally := map[string]int{}
	batch := make([]db.CreateImportScanFileParams, 0, bulkInsertBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.store.BulkCreateImportScanFiles(
			ctx,
			scan.ID,
			batch,
		); err != nil {
			return fmt.Errorf("insert rows: %w", err)
		}
		if err := s.store.IncrementImportScanProgress(
			ctx, scan.ID, len(batch),
		); err != nil {
			slog.WarnContext(ctx, "migration: progress update failed",
				"scan.id", scan.ID, "error", err)
		}
		batch = batch[:0]
		return nil
	}

	for _, m := range movies {
		row := s.radarrRow(scan, m, roots, tracked)
		tally[string(row.Classification)]++
		batch = append(batch, row)
		if len(batch) < bulkInsertBatchSize {
			continue
		}
		if err := flush(); err != nil {
			return nil, false, err
		}
		if !s.stillRunning(ctx, scan.ID) {
			return tally, false, nil
		}
	}
	if err := flush(); err != nil {
		return nil, false, err
	}
	return tally, s.stillRunning(ctx, scan.ID), nil
}

func (s *Service) radarrRow(
	scan *ent.ImportScan,
	m arr.Movie,
	roots []arr.RootMapping,
	tracked map[uint32]uint32,
) db.CreateImportScanFileParams {
	monitored := m.Monitored
	row := db.CreateImportScanFileParams{
		TMDBID:         m.TMDBID,
		ParsedTitle:    m.Title,
		Monitored:      &monitored,
		QualityProfile: profileFor(scan.Mappings, m.QualityProfileID),
		Classification: entimportscanfile.ClassificationConfirmed,
	}
	if m.Year > 0 {
		year := m.Year
		row.ParsedYear = &year
	}
	if m.MovieFile != nil && m.MovieFile.Path != "" {
		path, _ := arr.MapRoot(m.MovieFile.Path, roots)
		parsed := library.Parse(filepath.Base(path))
		row.SourcePath = path
		row.Size = m.MovieFile.Size
		row.ParsedQuality = parsed.Resolution
		row.ParsedReleaseGroup = m.MovieFile.ReleaseGroup
		if row.ParsedReleaseGroup == "" {
			row.ParsedReleaseGroup = parsed.Group
		}
		if !s.fileUsable(scan, path) {
			row.Classification = entimportscanfile.ClassificationAmbiguous
			row.Candidates = []schema.ScannedCandidate{{
				TMDBID: m.TMDBID, Title: m.Title, Year: m.Year,
			}}
		}
	}
	if id, ok := tracked[m.TMDBID]; ok &&
		row.Classification == entimportscanfile.ClassificationConfirmed {
		row.Classification = entimportscanfile.ClassificationExisting
		row.ExistingMovieID = id
	}
	return row
}

// buildMonitoring keeps every season flag and only the episodes that disagree
// with their season. A show monitored uniformly stores nothing per episode,
// which is the overwhelming majority.
func buildMonitoring(
	seasons []arr.Season, eps []arr.Episode,
) schema.ShowMonitoring {
	out := schema.ShowMonitoring{Seasons: make(map[uint16]bool, len(seasons))}
	for _, s := range seasons {
		out.Seasons[s.SeasonNumber] = s.Monitored
	}
	for _, e := range eps {
		seasonFlag, known := out.Seasons[e.SeasonNumber]
		if known && seasonFlag == e.Monitored {
			continue
		}
		out.Episodes = append(out.Episodes, schema.EpisodeFlag{
			Season:    e.SeasonNumber,
			Episode:   e.EpisodeNumber,
			Monitored: e.Monitored,
		})
	}
	return out
}

// buildSourceFiles carries the source's own file-to-episode mapping across.
// An episode flagged HasFile whose record did not come back embedded is
// skipped rather than guessed at — the commit would have no path to adopt.
func buildSourceFiles(
	eps []arr.Episode, roots []arr.RootMapping,
) []schema.SourceEpisodeFile {
	var out []schema.SourceEpisodeFile
	for _, e := range eps {
		if e.EpisodeFile == nil || e.EpisodeFile.Path == "" {
			continue
		}
		path, _ := arr.MapRoot(e.EpisodeFile.Path, roots)
		out = append(out, schema.SourceEpisodeFile{
			Season:  e.SeasonNumber,
			Episode: e.EpisodeNumber,
			Path:    path,
			Size:    e.EpisodeFile.Size,
		})
	}
	return out
}

func (s *Service) fetchSonarr(
	ctx context.Context, scan *ent.ImportScan, client arr.Library,
) (map[string]int, bool, error) {
	shows, err := client.Series(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("list series: %w", err)
	}
	if !s.stillRunning(ctx, scan.ID) {
		return nil, false, nil
	}
	total := len(shows)
	if err := s.store.UpdateImportScanStatus(
		ctx, scan.ID, entimportscan.StatusRunning,
		db.UpdateScanStatusOpts{TotalCount: &total},
	); err != nil {
		return nil, false, fmt.Errorf("record total: %w", err)
	}

	tracked, err := s.store.TVShowTVDBIndex(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("index tracked shows: %w", err)
	}

	roots := rootMappings(scan.Mappings)
	tally := map[string]int{}
	batch := make([]db.CreateImportScanShowParams, 0, bulkInsertBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.store.BulkCreateImportScanShows(
			ctx,
			scan.ID,
			batch,
		); err != nil {
			return fmt.Errorf("insert rows: %w", err)
		}
		batch = batch[:0]
		return nil
	}

	lastPoll := time.Now()
	for _, sh := range shows {
		// One request per show, so a large library takes long enough that a
		// cancel has to land between shows rather than between batches.
		if time.Since(lastPoll) > cancellationPollEvery {
			lastPoll = time.Now()
			if !s.stillRunning(ctx, scan.ID) {
				return tally, false, nil
			}
		}
		eps, err := client.Episodes(ctx, sh.ID)
		if err != nil {
			return nil, false, fmt.Errorf("list episodes for %q: %w", sh.Title, err)
		}
		row := s.sonarrRow(scan, sh, eps, roots, tracked)
		tally[string(row.Classification)]++
		batch = append(batch, row)
		if len(batch) >= bulkInsertBatchSize {
			if err := flush(); err != nil {
				return nil, false, err
			}
		}
		if err := s.store.IncrementImportScanProgress(ctx, scan.ID, 1); err != nil {
			slog.WarnContext(ctx, "migration: progress update failed",
				"scan.id", scan.ID, "error", err)
		}
	}
	if err := flush(); err != nil {
		return nil, false, err
	}
	return tally, s.stillRunning(ctx, scan.ID), nil
}

func (s *Service) sonarrRow(
	scan *ent.ImportScan,
	sh arr.Series,
	eps []arr.Episode,
	roots []arr.RootMapping,
	tracked map[uint32]uint32,
) db.CreateImportScanShowParams {
	files := buildSourceFiles(eps, roots)
	// A multi-episode file is listed once per episode it covers; the count
	// shown in review is of files.
	paths := make(map[string]struct{}, len(files))
	for _, f := range files {
		paths[f.Path] = struct{}{}
	}
	folder, _ := arr.MapRoot(sh.Path, roots)
	tvdbID := sh.TVDBID
	monitored := sh.Monitored
	monitoring := buildMonitoring(sh.Seasons, eps)

	row := db.CreateImportScanShowParams{
		FolderPath:     folder,
		ParsedTitle:    sh.Title,
		TVDBID:         &tvdbID,
		Classification: entimportscanshow.ClassificationConfirmed,
		Monitored:      &monitored,
		QualityProfile: profileFor(scan.Mappings, sh.QualityProfileID),
		SeriesType:     sh.SeriesType,
		Monitoring:     &monitoring,
		SourceFiles:    files,
		FileCount:      numeric.SaturateU16(len(paths)),
	}
	if sh.Year > 0 {
		year := sh.Year
		row.ParsedYear = &year
	}
	for _, f := range files {
		if !s.fileUsable(scan, f.Path) {
			row.Classification = entimportscanshow.ClassificationAmbiguous
			row.Candidates = []schema.ScannedShowCandidate{{
				TVDBID: sh.TVDBID, Title: sh.Title, Year: sh.Year,
			}}
			break
		}
	}
	if id, ok := tracked[sh.TVDBID]; ok &&
		row.Classification == entimportscanshow.ClassificationConfirmed {
		row.Classification = entimportscanshow.ClassificationExisting
		row.ExistingTvshowID = &id
	}
	return row
}
