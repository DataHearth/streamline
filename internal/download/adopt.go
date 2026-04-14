package download

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
)

// adoptDecision is what to do with one untracked managed torrent.
type adoptDecision struct {
	movieID, episodeID uint32
	// autoImport true → an importing record the caller enqueues; false → a
	// pending proposal carrying reason.
	autoImport bool
	reason     string
	quality    string
}

// untrackedTorrent pairs a torrent with the client it came from, so the
// adoption record records the originating download client.
type untrackedTorrent struct {
	t          Torrent
	clientName string
}

// classifyMovieAdoption decides the outcome for a parsed release against the
// candidate movies. hasFile reports whether a matched movie already has a file.
// Pure: no I/O, fully unit-tested. The bool return is false when nothing
// matched (skip — create no row).
func classifyMovieAdoption(
	parsed library.ParseResult,
	candidates []*ent.Movie,
	hasFile func(*ent.Movie) bool,
) (adoptDecision, bool) {
	var matches []*ent.Movie
	for _, m := range candidates {
		if library.TitleMatches(parsed.Title, m.Title) && parsed.Year == m.Year {
			matches = append(matches, m)
		}
	}
	if len(matches) == 0 {
		return adoptDecision{}, false
	}
	m := matches[0]
	d := adoptDecision{movieID: m.ID, quality: parsed.Resolution}
	switch {
	case len(matches) > 1:
		d.reason = "ambiguous match"
	case hasFile(m):
		d.reason = "already have a file"
	case resolutionOK(parsed.Resolution, profileMin(m.QualityProfile)):
		d.autoImport = true
	default:
		d.reason = fmt.Sprintf("resolution %q below minimum %q",
			parsed.Resolution, profileMin(m.QualityProfile))
	}
	return d, true
}

// classifyEpisodeAdoption decides the outcome for a parsed release against the
// candidate shows. A single-episode release (SxxExx, or an anime absolute
// number) matches an episode and applies the has-file/quality rules using the
// show's profile; a season pack or otherwise-unresolvable multi is proposed
// "review manually" linked to the first episode of the parsed season. Returns
// false when no show matches (skip — create no row). Pure: no I/O.
func classifyEpisodeAdoption(
	parsed library.ParseResult,
	shows []*ent.TVShow,
	hasFile func(*ent.Episode) bool,
) (adoptDecision, bool) {
	var show *ent.TVShow
	for _, s := range shows {
		if library.TitleMatches(parsed.Title, s.Title) {
			show = s
			break
		}
	}
	if show == nil {
		return adoptDecision{}, false
	}

	singleEpisode := !parsed.SeasonPack &&
		(parsed.Episode > 0 || parsed.AbsoluteNumber > 0)
	if !singleEpisode {
		// Season pack / multi-file: propose, never auto-fan. Link the first
		// episode of the parsed season so the operator can act on it (the
		// importer fans the directory out on import).
		ep := firstEpisodeOfSeason(show, parsed.Season)
		if ep == nil {
			return adoptDecision{}, false
		}
		return adoptDecision{
			episodeID: ep.ID,
			quality:   parsed.Resolution,
			reason:    "season pack, review manually",
		}, true
	}

	ep := library.MatchEpisode(
		parsed,
		show.Edges.Seasons,
		show.Type == tvshow.TypeAnime,
	)
	if ep == nil {
		return adoptDecision{}, false
	}
	d := adoptDecision{episodeID: ep.ID, quality: parsed.Resolution}
	switch {
	case hasFile(ep):
		d.reason = "already have a file"
	case resolutionOK(parsed.Resolution, profileMin(show.QualityProfile)):
		d.autoImport = true
	default:
		d.reason = fmt.Sprintf("resolution %q below minimum %q",
			parsed.Resolution, profileMin(show.QualityProfile))
	}
	return d, true
}

// firstEpisodeOfSeason returns the first episode of the show's season numbered
// `season`, or nil when the season is absent or empty.
func firstEpisodeOfSeason(show *ent.TVShow, season uint16) *ent.Episode {
	for _, se := range show.Edges.Seasons {
		if se.Number == season {
			for _, e := range se.Edges.Episodes {
				return e
			}
		}
	}
	return nil
}

// episodeHasFile reports whether an episode (with MediaFiles eager-loaded)
// already has a file on disk.
func episodeHasFile(ep *ent.Episode) bool {
	return len(ep.Edges.MediaFiles) > 0
}

func profileMin(name string) string {
	p, _ := config.ResolveQualityProfile(name)
	return p.MinResolution
}

// resolutionOK reports parsed >= min on the fixed 720p<1080p<2160p ladder
// (4K == 2160p). An empty/unparseable parsed resolution → false (propose,
// never auto-import blind); an empty/unparseable min → accept.
func resolutionOK(parsed, min string) bool {
	rank := map[string]int{"720p": 1, "1080p": 2, "2160p": 3, "4k": 3}
	p, okP := rank[strings.ToLower(parsed)]
	if !okP {
		return false
	}
	m, okM := rank[strings.ToLower(min)]
	if !okM {
		return true
	}
	return p >= m
}

// AdoptManualTorrents scans every enabled client for managed-category torrents
// streamline does not yet track, and either auto-imports (returning those
// record IDs for the caller to enqueue) or files a pending proposal. Per-item
// failures are logged and skipped; only listing failures are returned.
func (d *download) AdoptManualTorrents(ctx context.Context) ([]uint32, error) {
	ctx, span := tracer.Start(ctx, "download.adopt_manual_torrents")
	defer span.End()

	known, err := d.db.AllDownloadRecordHashes(ctx)
	if err != nil {
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("list known hashes: %w", err),
		)
	}

	var untracked []untrackedTorrent
	// liveByClient maps each successfully-listed client to the hashes it
	// currently reports, so stale pending proposals can be pruned. A client
	// that fails to list is absent here and never triggers a purge.
	liveByClient := map[string][]string{}
	for _, dc := range config.EnabledDownloadClients() {
		client, err := d.buildClient(dc)
		if err != nil {
			slog.DebugContext(ctx, "adopt: build client failed",
				"client", dc.Name, "error", err)
			continue
		}
		torrents, err := client.ListTorrents(ctx)
		if err != nil {
			slog.DebugContext(ctx, "adopt: list torrents failed",
				"client", dc.Name, "error", err)
			continue
		}
		live := make([]string, 0, len(torrents))
		for _, t := range torrents {
			live = append(live, t.Hash)
			if t.Status != StatusSeeding && t.Status != StatusCompleted {
				continue
			}
			if _, ok := known[t.Hash]; ok {
				continue
			}
			untracked = append(
				untracked,
				untrackedTorrent{t: t, clientName: dc.Name},
			)
		}
		liveByClient[dc.Name] = live
	}

	// Prune proposals whose torrent vanished from its originating client (the
	// operator removed it from the download client). Runs every tick, before
	// the additive early-exit, so an emptied "needs attention" queue self-heals.
	var pruned int
	for clientName, live := range liveByClient {
		n, err := d.db.DeleteStalePendingAdoptions(ctx, clientName, live)
		if err != nil {
			slog.WarnContext(ctx, "adopt: prune stale proposals failed",
				"client", clientName, "error", err)
			continue
		}
		pruned += n
	}
	span.SetAttributes(attribute.Int("adopt.pruned", pruned))

	if len(untracked) == 0 {
		return nil, nil // early-exit: no candidate loads
	}

	movies, err := d.db.ListMoviesForAdoption(ctx)
	if err != nil {
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("list movies: %w", err),
		)
	}
	shows, err := d.db.ListTvShowsForAdoption(ctx)
	if err != nil {
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("list shows: %w", err),
		)
	}
	hasFile := d.movieHasFile(ctx)

	var enqueue []uint32
	for _, u := range untracked {
		parsed := library.Parse(u.t.Name)
		dec, ok := classifyMovieAdoption(parsed, movies, hasFile)
		if !ok {
			dec, ok = classifyEpisodeAdoption(parsed, shows, episodeHasFile)
		}
		if !ok {
			continue // matched neither a movie nor an episode
		}
		id, err := d.persistAdoption(ctx, u, dec)
		if err != nil {
			slog.WarnContext(ctx, "adopt: persist failed",
				"hash", u.t.Hash, "error", err)
			continue
		}
		if dec.autoImport {
			enqueue = append(enqueue, id)
		}
	}
	span.SetAttributes(attribute.Int("adopt.enqueued", len(enqueue)))
	return enqueue, nil
}

// movieHasFile returns a closure over MovieHasMediaFile. A lookup error is
// treated as "has file" so an indeterminate state proposes rather than
// blind-imports.
func (d *download) movieHasFile(ctx context.Context) func(*ent.Movie) bool {
	return func(m *ent.Movie) bool {
		has, err := d.db.MovieHasMediaFile(ctx, m.TmdbID)
		if err != nil {
			slog.WarnContext(ctx, "adopt: media file check failed",
				"movie.id", m.ID, "error", err)
			return true
		}
		return has
	}
}

// persistAdoption writes the adoption record (importing for auto-import,
// pending for a proposal) and returns its ID. SavePath mirrors CheckStatus:
// the download path joined with the torrent name.
func (d *download) persistAdoption(
	ctx context.Context, u untrackedTorrent, dec adoptDecision,
) (uint32, error) {
	status := downloadrecord.StatusPending
	if dec.autoImport {
		status = downloadrecord.StatusImporting
	}
	rec, err := d.db.CreateDownloadRecord(ctx, db.CreateDownloadRecordParams{
		Title:              u.t.Name,
		Size:               u.t.Size,
		TorrentHash:        u.t.Hash,
		Status:             status,
		MovieID:            dec.movieID,
		EpisodeID:          dec.episodeID,
		DownloadClientName: u.clientName,
		SavePath: filepath.Join(
			config.Get().Library.DownloadPath,
			u.t.Name,
		),
		Quality:       dec.quality,
		FailureReason: dec.reason,
	})
	if err != nil {
		return 0, err
	}
	return rec.ID, nil
}
