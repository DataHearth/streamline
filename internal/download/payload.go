package download

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/schema"
)

// payloadLocation is the two places a torrent's payload can be on this
// process's disk.
//
// conventional, <library.download_path>/<torrent name>, comes first: it is the
// location contract every grab relies on (streamline never sends a save path
// on add) and what the importer's allowed_download_roots fence is configured
// around. reported is the client's own save path for the torrent translated
// through download.path_mappings and joined with the same name, empty when
// the client reports no save path or the two are one path. A client puts a
// torrent somewhere else whenever its download directory is not
// download_path: qBittorrent applies a category's save path only under
// Automatic Torrent Management, and Manual is its default, so a grab tagged
// with the streamline category still lands in the default save path.
type payloadLocation struct {
	conventional string
	reported     string
}

// payloadPaths builds both candidates for t. downloadSavePath refuses any name
// that could climb out of a root, so joining the same name onto the client's
// root is safe; an unsafe name is its ErrUnsafeTorrentName.
func payloadPaths(t Torrent) (payloadLocation, error) {
	conventional, err := downloadSavePath(t.Name)
	if err != nil {
		return payloadLocation{}, err
	}
	loc := payloadLocation{conventional: conventional}
	if t.SavePath != "" {
		if alt := filepath.Join(
			MapClientPath(t.SavePath),
			t.Name,
		); alt != conventional {
			loc.reported = alt
		}
	}
	return loc, nil
}

// locate returns the first candidate that exists.
func (l payloadLocation) locate() (string, bool) {
	for _, p := range []string{l.conventional, l.reported} {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

// holdReasons is the one reason a location hold carries: the torrent, where
// streamline looked, and where the client says it saved it.
func (l payloadLocation) holdReasons(name string) []schema.HoldReason {
	return []schema.HoldReason{{
		File:     name,
		Check:    schema.HoldCheckLocation,
		Expected: l.conventional,
		Actual:   l.reported,
	}}
}

// holdUnlocated holds a completed record whose payload is at neither path,
// before anything moves it to importing. The importer stats save_path first
// and reads ENOENT as retryable, so starting the import would spend every
// library.import_max_attempts on a path nothing was written to and land the
// record failed. A hold counts no attempt, shows both paths, and is released
// by the next monitor tick that finds either one (releaseLocated).
func (d *download) holdUnlocated(
	ctx context.Context,
	rec *ent.DownloadRecord,
	t *Torrent,
	loc payloadLocation,
) {
	held, err := d.db.HoldDownloadingRecord(ctx, rec.ID, loc.holdReasons(t.Name))
	if err != nil {
		slog.WarnContext(ctx, "hold record with missing payload failed",
			"record.id", rec.ID, "error", err)
		return
	}
	if !held {
		return
	}
	slog.WarnContext(ctx,
		"completed download's files are not where streamline looks; holding it",
		"record.id", rec.ID,
		"hash", rec.TorrentHash,
		"torrent", t.Name,
		"expected", loc.conventional,
		"client_save_path", t.SavePath,
		"client_path_mapped", loc.reported,
	)
}

// releaseLocated re-checks a record held for its payload's location against
// the client's current report. Found, the record goes to importing from that
// path; still missing, its reasons follow the client's report, so the paths
// it shows are the ones a fix has to reconcile. A torrent the client no
// longer reports or that is not complete any more stays held: resolving it is
// the operator's call.
func (d *download) releaseLocated(
	ctx context.Context,
	rec *ent.DownloadRecord,
	t *Torrent,
) (CompletedDownload, bool) {
	if t.Status != StatusSeeding && t.Status != StatusCompleted {
		return CompletedDownload{}, false
	}
	loc, err := payloadPaths(*t)
	if err != nil {
		return CompletedDownload{}, false
	}
	path, found := loc.locate()
	if !found {
		if reasons := loc.holdReasons(
			t.Name,
		); !slices.Equal(
			reasons,
			rec.HoldReasons,
		) {
			if err := d.db.SetHeldRecordReasons(ctx, rec.ID, reasons); err != nil {
				slog.WarnContext(ctx, "refresh location hold failed",
					"record.id", rec.ID, "error", err)
			}
		}
		return CompletedDownload{}, false
	}
	moved, err := d.db.ReleaseLocatedRecord(ctx, rec.ID, path)
	if err != nil {
		slog.WarnContext(ctx, "release location hold failed",
			"record.id", rec.ID, "error", err)
		return CompletedDownload{}, false
	}
	if !moved {
		return CompletedDownload{}, false
	}
	slog.InfoContext(ctx, "held download's files found; importing",
		"record.id", rec.ID, "hash", rec.TorrentHash, "save_path", path)
	d.markMediaImporting(ctx, rec)
	return CompletedDownload{Record: rec, SavePath: path}, true
}

// markMediaImporting moves what a record is for to importing alongside it:
// its episodes still downloading or paused, and its movie.
func (d *download) markMediaImporting(ctx context.Context, rec *ent.DownloadRecord) {
	if err := d.db.MarkRecordEpisodesImporting(ctx, rec.ID); err != nil {
		slog.WarnContext(ctx, "mark episodes importing failed",
			"id", rec.ID, "error", err)
	}
	if rec.Edges.Movie != nil {
		if err := d.db.UpdateMovieStatus(
			ctx, rec.Edges.Movie.ID, movie.StatusImporting,
		); err != nil {
			slog.WarnContext(ctx, "mark movie importing failed",
				"id", rec.ID, "error", err)
		}
	}
}

// IsLocationHold reports whether rec is held for its payload's location only.
func IsLocationHold(rec *ent.DownloadRecord) bool {
	if rec.Status != downloadrecord.StatusHeld || len(rec.HoldReasons) == 0 {
		return false
	}
	for _, r := range rec.HoldReasons {
		if r.Check != schema.HoldCheckLocation {
			return false
		}
	}
	return true
}
