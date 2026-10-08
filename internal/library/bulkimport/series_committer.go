package bulkimport

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	entepisode "github.com/datahearth/streamline/ent/episode"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/schema"
	enttvshow "github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/tvshow"
	"github.com/datahearth/streamline/internal/mediaserver"
)

// runCommitSeries adopts every reviewed show in a series scan: creates (or
// reuses) the TV show and links the episode files already on disk. Runs
// sequentially — a scan holds tens of shows, not thousands of files.
func (s *Service) runCommitSeries(ctx context.Context, scan *ent.ImportScan) {
	ctx, span := tracer.Start(ctx, "bulkimport.run_commit_series",
		trace.WithAttributes(attribute.Int64("scan.id", int64(scan.ID))))
	defer span.End()

	defer func() {
		if r := recover(); r != nil {
			s.markScanFailed(ctx, scan.ID, fmt.Sprintf("panic: %v", r))
		}
	}()

	shows, err := s.store.ListImportScanShowsForCommit(ctx, scan.ID)
	if err != nil {
		s.markScanFailed(ctx, scan.ID, err.Error())
		return
	}

	var success, failed uint32
	for _, sh := range shows {
		outcome, msg, createdID := s.commitShow(ctx, scan, sh)
		if uerr := s.store.UpdateImportScanShowOutcome(
			ctx,
			sh.ID,
			outcome,
			db.UpdateScanShowOutcomeOpts{
				Message:         msg,
				CreatedTvshowID: createdID,
			},
		); uerr != nil {
			slog.ErrorContext(ctx, "series commit: failed to record show outcome",
				"scan.id", scan.ID, "show.id", sh.ID, "error", uerr)
		}
		switch outcome {
		case entimportscanshow.OutcomeCreated, entimportscanshow.OutcomeAttached:
			success++
		case entimportscanshow.OutcomeFailed:
			failed++
		}
	}

	committedAt := time.Now()
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scan.ID,
		entimportscan.StatusCompleted,
		db.UpdateScanStatusOpts{
			CommittedAt:        &committedAt,
			CommitSuccessCount: &success,
			CommitFailedCount:  &failed,
		},
	); err != nil {
		slog.ErrorContext(ctx, "series commit: failed to flip scan to completed",
			"scan.id", scan.ID, "error", err)
	}
	slog.InfoContext(
		ctx,
		"series commit finished",
		"scan.id",
		scan.ID,
		"commit.success_count",
		success,
		"commit.failed_count",
		failed,
	)
	countCommit(ctx, "series", scan.Source, "success", int64(success))
	countCommit(ctx, "series", scan.Source, "failed", int64(failed))
	if success > 0 {
		mediaserver.RefreshInBackground(ctx, s.ms, "series", s.seriesPath)
	}
}

// commitShow adopts one show folder: resolve/create the show, then link each
// on-disk file to its episode. An in-place scan records the file where it lies;
// a rename scan transfers it into the series library first. Missing episodes
// stay wanted.
func (s *Service) commitShow(
	ctx context.Context, scan *ent.ImportScan, sc *ent.ImportScanShow,
) (entimportscanshow.Outcome, string, uint32) {
	ctx, span := tracer.Start(ctx, "bulkimport.commit_show",
		trace.WithAttributes(
			attribute.Int64("show.id", int64(sc.ID)),
			attribute.String("show.folder", sc.FolderPath)))
	defer span.End()

	migration := isMigration(scan)
	profile, profileNote := "", ""
	if migration {
		profile, profileNote = migratedProfile(sc.QualityProfile)
	}
	show, reused, outcome, msg, id := s.resolveShow(ctx, sc, profile)
	if show == nil {
		return outcome, msg, id
	}

	var (
		plan    []episodeMatch
		skipped int
		total   int
	)
	if migration {
		// The series type decides absolute-number matching, so it is applied
		// before anything resolves an episode.
		show = s.applySeriesType(ctx, show, sc.SeriesType)
		// The half-match guard below exists because a parsed folder name is
		// weak evidence for which show a folder belongs to; here the source
		// states the tvdb id and the episode of every file.
		files := s.usableSourceFiles(scan, sc.SourceFiles)
		plan, skipped = planFromSource(show, files)
		skipped += len(sc.SourceFiles) - len(files)
		total = len(sc.SourceFiles)
		if total > 0 && len(plan) == 0 {
			return entimportscanshow.OutcomeFailed, fmt.Sprintf(
				"none of the %d files could be adopted (missing, outside the library, or naming an episode this library lacks)",
				total,
			), show.ID
		}
	} else {
		files, err := library.ListVideoFilesRecursive(sc.FolderPath)
		if err != nil {
			return commitShowFail("list folder", err, show.ID)
		}
		// Match the whole folder before anything moves. A folder whose files
		// mostly fail to match is far more likely bound to the wrong show than
		// to be a show with missing metadata, and adopting it anyway wrote 7 of
		// 76 files into a same-named series and counted it a success.
		var unmatched int
		plan, unmatched = planEpisodes(show, files)
		total = len(files)
		if len(plan) <= unmatched {
			return entimportscanshow.OutcomeFailed, fmt.Sprintf(
				"only %d of %d files matched an episode — folder likely belongs to another show",
				len(plan),
				total,
			), show.ID
		}
	}

	success := entimportscanshow.OutcomeCreated
	if reused {
		success = entimportscanshow.OutcomeAttached
	}
	matched := 0
	touched := map[uint16]struct{}{}
	// One imported event stands for the whole show, recorded below. Without
	// this the per-file create hook wrote an activity row per episode, and a
	// commit of a few hundred files buried every other event in the feed.
	ctx = events.SuppressImported(ctx)
	for _, m := range plan {
		f, season, target := m.path, m.season, m.episode
		parsed := library.Parse(filepath.Base(f))
		// Stat before the replace check: a path this loop already removed while
		// replacing (old copy + repack in one folder) must not tear down the
		// record its replacement just created.
		info, err := os.Stat(f)
		if err != nil {
			slog.WarnContext(
				ctx,
				"series adopt: stat failed",
				"file",
				f,
				"error",
				err,
			)
			continue
		}
		// An episode holds at most one media file: committing an accepted show
		// replaces whatever file a matched episode already has. The same path
		// being re-scanned is already adopted, so it needs no rewrite.
		if mf, err := s.store.FindMediaFileByEpisodeID(ctx, target.ID); err == nil {
			if mf.Path == f {
				continue
			}
			if rmErr := os.Remove(mf.Path); rmErr != nil && !os.IsNotExist(rmErr) {
				slog.WarnContext(ctx, "series adopt: remove old file failed",
					"path", mf.Path, "error", rmErr)
			} else if rmErr == nil {
				// The failure path was logged and the success path was not, so
				// the case that actually deleted a file left no trace — and
				// "where did my old copy go" is asked about exactly that one.
				slog.InfoContext(ctx, "series adopt: replaced an existing file",
					"episode.id", target.ID,
					"old_path", mf.Path,
					"new_path", f)
			}
			if dErr := s.store.DeleteMediaFile(ctx, mf.ID); dErr != nil {
				slog.WarnContext(ctx, "series adopt: delete replaced file failed",
					"episode.id", target.ID, "error", dErr)
				continue
			}
		} else if !ent.IsNotFound(err) {
			slog.WarnContext(ctx, "series adopt: media file lookup failed",
				"episode.id", target.ID, "error", err)
			continue
		}
		path, size := f, info.Size()
		if scan.Mode == entimportscan.ModeRename {
			imported, err := s.importSvc.ImportEpisodeWithMode(
				ctx, f, show, season, target, string(scan.ImportMode),
			)
			if err != nil {
				slog.WarnContext(ctx, "series adopt: import episode failed",
					"file", f, "episode.id", target.ID, "error", err)
				continue
			}
			path, size, parsed = imported.Path, imported.Size, imported.Parsed
		}
		if _, err := s.store.CreateMediaFile(ctx, db.CreateMediaFileParams{
			EpisodeID:    target.ID,
			Path:         path,
			Size:         size,
			Quality:      parsed.Resolution,
			Format:       parsed.Extension,
			ReleaseGroup: parsed.Group,
			Parsed:       &parsed,
			Source:       entmediafile.SourceWizard,
			QueueTranscode: config.TranscodeEligible(
				config.MediaSeries,
				show.QualityProfile,
			),
		}); err != nil {
			slog.WarnContext(ctx, "series adopt: create media file failed",
				"episode.id", target.ID, "error", err)
			continue
		}
		if err := s.store.SetEpisodeStatus(
			ctx,
			target.ID,
			entepisode.StatusAvailable,
		); err != nil {
			slog.WarnContext(ctx, "series adopt: flip episode status failed",
				"episode.id", target.ID, "error", err)
		}
		touched[season] = struct{}{}
		matched++
	}
	slog.InfoContext(ctx, "series adopted",
		"tvshow.id", show.ID, "matched", matched, "files", total)
	if matched > 0 {
		seasons := make([]uint16, 0, len(touched))
		for n := range touched {
			seasons = append(seasons, n)
		}
		slices.Sort(seasons)
		if err := events.Record(
			ctx, nil, events.TypeImported, events.ScopeSeries, show.ID,
			map[string]any{
				"seasons":  seasons,
				"episodes": matched,
				"source":   importSource(scan),
			},
		); err != nil {
			slog.WarnContext(ctx, "series adopt: record imported event failed",
				"tvshow.id", show.ID, "error", err)
		}
	}
	if !migration {
		return success, "", show.ID
	}
	var skippedNote string
	if skipped > 0 {
		skippedNote = fmt.Sprintf(
			"%d of %d files were missing, outside the library, or named an episode this library lacks",
			skipped,
			total,
		)
	}
	return success, joinNotes(
		profileNote, s.applyShowState(ctx, show, sc, profile), skippedNote,
	), show.ID
}

func importSource(scan *ent.ImportScan) string {
	if isMigration(scan) {
		return string(scan.Source)
	}
	return "bulk_import"
}

// planFromSource resolves files by episode number instead of by filename. The
// source already identified every one of them, so re-parsing is strictly worse
// evidence: it re-opens every parser edge case and can fail a folder the
// source knows perfectly. The second return counts entries naming an episode
// this library has no row for, which happens when the source's metadata is
// ahead of TVDB's.
func planFromSource(
	show *ent.TVShow, files []schema.SourceEpisodeFile,
) ([]episodeMatch, int) {
	byNumber := map[[2]uint16]*ent.Episode{}
	for _, season := range show.Edges.Seasons {
		for _, e := range season.Edges.Episodes {
			byNumber[[2]uint16{season.Number, e.Number}] = e
		}
	}
	var plan []episodeMatch
	skipped := 0
	for _, f := range files {
		ep, ok := byNumber[[2]uint16{f.Season, f.Episode}]
		if !ok {
			skipped++
			continue
		}
		plan = append(
			plan,
			episodeMatch{path: f.Path, season: f.Season, episode: ep},
		)
	}
	return plan, skipped
}

func (s *Service) usableSourceFiles(
	scan *ent.ImportScan, files []schema.SourceEpisodeFile,
) []schema.SourceEpisodeFile {
	out := make([]schema.SourceEpisodeFile, 0, len(files))
	for _, f := range files {
		if s.fileUsable(scan, f.Path) {
			out = append(out, f)
		}
	}
	return out
}

func (s *Service) applySeriesType(
	ctx context.Context, show *ent.TVShow, seriesType string,
) *ent.TVShow {
	if seriesType == "" || seriesType == string(show.Type) {
		return show
	}
	if _, err := s.seriesAdder.Update(
		ctx, show.ID, tvshow.UpdateParams{Type: &seriesType},
	); err != nil {
		slog.WarnContext(ctx, "migration: series type not applied",
			"tvshow.id", show.ID, "type", seriesType, "error", err)
		return show
	}
	// Update answers the bare row; the planner needs the eager-loaded tree.
	reloaded, err := s.store.FindTVShowByID(ctx, show.ID)
	if err != nil {
		slog.WarnContext(ctx, "migration: reload after series type failed",
			"tvshow.id", show.ID, "error", err)
		return show
	}
	return reloaded
}

// applyShowState writes the source's monitored state onto the show. The show
// flag goes through the tvshow service, which cascades it and records the one
// series-scope monitoring event; the season and episode exceptions then go
// straight to the db helpers, since the service would record an event per
// call and a 200-episode show would write 200 activity rows.
func (s *Service) applyShowState(
	ctx context.Context,
	show *ent.TVShow,
	sc *ent.ImportScanShow,
	profile string,
) string {
	var problems []string

	monitored := sc.Monitored
	params := tvshow.UpdateParams{Monitored: &monitored}
	if profile != "" {
		params.QualityProfile = &profile
	}
	if _, err := s.seriesAdder.Update(ctx, show.ID, params); err != nil {
		problems = append(problems, fmt.Sprintf("show flags: %v", err))
	}

	seasonIDs := map[uint16]uint32{}
	episodeIDs := map[[2]uint16]uint32{}
	for _, season := range show.Edges.Seasons {
		seasonIDs[season.Number] = season.ID
		for _, e := range season.Edges.Episodes {
			episodeIDs[[2]uint16{season.Number, e.Number}] = e.ID
		}
	}
	numbers := make([]uint16, 0, len(sc.Monitoring.Seasons))
	for n := range sc.Monitoring.Seasons {
		numbers = append(numbers, n)
	}
	slices.Sort(numbers)
	for _, n := range numbers {
		want := sc.Monitoring.Seasons[n]
		id, ok := seasonIDs[n]
		if !ok || want == monitored {
			continue
		}
		if _, err := s.store.CascadeSeasonMonitored(ctx, id, want); err != nil {
			problems = append(problems,
				fmt.Sprintf("season %d monitoring: %v", n, err))
		}
	}
	for _, f := range sc.Monitoring.Episodes {
		id, ok := episodeIDs[[2]uint16{f.Season, f.Episode}]
		if !ok {
			continue
		}
		if err := s.store.SetEpisodeMonitored(ctx, id, f.Monitored); err != nil {
			problems = append(problems, fmt.Sprintf(
				"S%02dE%02d monitoring: %v", f.Season, f.Episode, err))
		}
	}
	if len(problems) > 0 {
		slog.WarnContext(ctx, "migration: show monitoring partly applied",
			"tvshow.id", show.ID, "problems", len(problems))
	}
	return strings.Join(problems, "; ")
}

// episodeMatch is one folder file bound to the episode it belongs to.
type episodeMatch struct {
	path    string
	season  uint16
	episode *ent.Episode
}

// planEpisodes resolves every file in a show folder to an episode without
// touching disk, so the caller can judge the folder as a whole before the first
// transfer. Returns the matches plus how many files matched nothing.
func planEpisodes(show *ent.TVShow, files []string) ([]episodeMatch, int) {
	anime := show.Type == enttvshow.TypeAnime
	var plan []episodeMatch
	unmatched := 0
	for _, f := range files {
		season, target := matchEpisodePath(f, show.Edges.Seasons, anime)
		if target == nil {
			unmatched++
			continue
		}
		plan = append(plan, episodeMatch{path: f, season: season, episode: target})
	}
	return plan, unmatched
}

// matchEpisodePath resolves a file to an episode from its basename, then from
// each folder above it up to the season folder.
//
// SanitizePath used to let a slash through, so an episode titled "A / B / C"
// was written as folder "… - S01E17 - A" holding folder "B" holding file
// "C [].mkv". Those files are still on disk, their basename says nothing, and
// the episode number sits one folder up per slash in the title — three on the
// homelab. Folder-per-episode releases have the same shape one level deep.
func matchEpisodePath(
	f string, seasons []*ent.Season, anime bool,
) (uint16, *ent.Episode) {
	const maxDepth = 4
	for range maxDepth {
		season, target := library.MatchEpisodeInSeason(
			library.Parse(filepath.Base(f)), seasons, anime,
		)
		if target != nil {
			return season, target
		}
		f = filepath.Dir(f)
	}
	return 0, nil
}

// resolveShow returns the eager-loaded show to adopt into, creating it from TVDB
// when no row for that tvdb id exists yet. reused reports that the show was
// already in the library. On failure it returns a nil show plus the outcome
// triple to record.
func (s *Service) resolveShow(
	ctx context.Context, sc *ent.ImportScanShow, profile string,
) (*ent.TVShow, bool, entimportscanshow.Outcome, string, uint32) {
	if sc.ExistingTvshowID != nil {
		found, err := s.store.FindTVShowByID(ctx, *sc.ExistingTvshowID)
		if err != nil {
			o, m, id := commitShowFail("load existing show", err, 0)
			return nil, false, o, m, id
		}
		return found, true, "", "", 0
	}

	// Reviewer's pick wins over the classifier's top match.
	tvdbID := uint32(0)
	if sc.DecisionTvdbID != nil {
		tvdbID = *sc.DecisionTvdbID
	} else if sc.TvdbID != nil {
		tvdbID = *sc.TvdbID
	}
	if tvdbID == 0 {
		return nil, false, entimportscanshow.OutcomeFailed, "no tvdb match to adopt", 0
	}

	// Resolve against current state rather than trusting the scan-time
	// classification: a reviewer pointing an unmatched folder at a show that is
	// already in the library — the normal way to fix a bad match — would
	// otherwise collide on tv_shows.tvdb_id and fail the whole entry.
	// FindTVShowByTVDBID reports "no such show" as a nil row with a nil error,
	// so the row has to be checked, not just the error.
	existing, err := s.store.FindTVShowByTVDBID(ctx, tvdbID)
	if err != nil {
		o, m, id := commitShowFail("look up show", err, 0)
		return nil, false, o, m, id
	}
	if existing != nil {
		found, ferr := s.store.FindTVShowByID(ctx, existing.ID)
		if ferr != nil {
			o, m, id := commitShowFail("load existing show", ferr, existing.ID)
			return nil, false, o, m, id
		}
		return found, true, "", "", 0
	}

	created, err := s.seriesAdder.Add(ctx, tvdbID, profile)
	if err != nil {
		o, m, id := commitShowFail("add show", err, 0)
		return nil, false, o, m, id
	}
	found, err := s.store.FindTVShowByID(ctx, created.ID)
	if err != nil {
		o, m, id := commitShowFail("load created show", err, created.ID)
		return nil, false, o, m, id
	}
	return found, false, "", "", 0
}

func commitShowFail(
	label string, err error, tvshowID uint32,
) (entimportscanshow.Outcome, string, uint32) {
	return entimportscanshow.OutcomeFailed, fmt.Sprintf(
		"%s: %v",
		label,
		err,
	), tvshowID
}
