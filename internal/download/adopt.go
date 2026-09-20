package download

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// adoptDecision is what to do with one untracked managed torrent.
type adoptDecision struct {
	movieID, episodeID uint32
	// episodeIDs is every episode the torrent is for — the record's claim.
	// Holds episodeID for a single-episode release and the pack's whole scope
	// otherwise; empty for a movie.
	episodeIDs []uint32
	// autoImport true → an importing record the caller enqueues; false → a
	// pending proposal carrying reason.
	autoImport bool
	// completed marks a torrent whose payload is the file the library already
	// holds: the record is born completed, restoring the bookkeeping a lost
	// row left behind, and nobody is asked anything.
	completed bool
	reason    string
	quality   string
}

// sameFile reports whether a torrent — its parsed name and size — is the
// media file the owner already holds. A byte-identical size for the same
// title is one release, and that is the case a wiped records table leaves
// behind: the library file *is* this torrent's payload, copied out at import.
// The release facts both sides state are a veto on top: a stored resolution
// or source that contradicts the torrent's name says two different encodes
// happened to land on one byte count. A fact either side leaves blank is not
// evidence. Codec and group are deliberately not compared: a row without a
// stored parse reports the probe's "hevc" where the name says "x265", and a
// row the bulk import recreated from a template-named file carries whatever
// followed the last hyphen of the episode title as its group ("Why-man").
//
// The torrent may be a folder: an .nfo or a subtitle beside the video adds
// kilobytes to the total, so the torrent may exceed the file by up to
// sidecarSlack. A sample clip is megabytes and falls through to the "already
// have a file" proposal; compare against the torrent's largest file if that
// ever matters.
func sameFile(parsed library.ParseResult, size int64, files []*ent.MediaFile) bool {
	const sidecarSlack = 1 << 20
	if len(files) == 0 || size < files[0].Size || size-files[0].Size > sidecarSlack {
		return false
	}
	have := library.ParsedFromMediaFile(files[0])
	for _, pair := range [][2]string{
		{parsed.Resolution, have.Resolution},
		{parsed.Source, have.Source},
	} {
		if pair[0] != "" && pair[1] != "" && !strings.EqualFold(pair[0], pair[1]) {
			return false
		}
	}
	return true
}

// reasonUnidentified marks a proposal whose release matched nothing in the
// library. It is the one proposal reason carrying no media edge, so the SPA
// keys the Identify action off the absent media rather than off this string.
const reasonUnidentified = "unidentified — pick a title"

// missingFilesReason labels a proposal whose payload is at neither path
// adoptionPath tried. Naming the client's own save path is the whole value of
// the message: the usual cause is a torrent tagged with the managed category
// rather than moved into it, and the two paths side by side are what says so.
func missingFilesReason(clientPath string) string {
	if clientPath == "" {
		return "files not found under the download path"
	}
	return fmt.Sprintf("files not found — client reports %s", clientPath)
}

// untrackedTorrent pairs a torrent with the client it came from, so the
// adoption record records the originating download client.
type untrackedTorrent struct {
	t          Torrent
	clientName string
}

// classifyMovieAdoption decides the outcome for a parsed release of size bytes
// against the candidate movies, with their media files eager-loaded. Pure: no
// I/O, fully unit-tested. The bool return is false when nothing matched (skip
// — create no row).
func classifyMovieAdoption(
	parsed library.ParseResult,
	size int64,
	candidates []*ent.Movie,
) (adoptDecision, bool) {
	var matches []*ent.Movie
	for _, m := range candidates {
		// Aliases, not just the stored title: with metadata.language set to
		// anything but English the library holds the localized title while the
		// release is named in the original, so "Hellboy II : Les Légions d'or
		// maudites" matched nothing in Hellboy.II.The.Golden.Army.2008 and a
		// film already on disk was proposed as "unidentified — pick a title".
		// Every other matcher in the tree already takes the alias set; this was
		// the one that did not.
		if library.TitleMatchesAny(
			parsed.Title, m.Title, append(m.Aliases, m.OriginalTitle),
		) && parsed.Year == m.Year {
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
	case sameFile(parsed, size, m.Edges.MediaFiles):
		d.completed = true
	case len(m.Edges.MediaFiles) > 0:
		d.reason = "already have a file"
	case resolutionOK(parsed.Resolution, profileMin(m.QualityProfile)):
		d.autoImport = true
	default:
		d.reason = fmt.Sprintf("resolution %q below minimum %q",
			parsed.Resolution, profileMin(m.QualityProfile))
	}
	return d, true
}

// classifyEpisodeAdoption decides the outcome for a parsed release of size
// bytes against the candidate shows. A single-episode release (SxxExx, or an
// anime absolute number) matches an episode and applies the has-file/quality
// rules using the show's profile; a season pack or otherwise-unresolvable
// multi is proposed "review manually" linked to the first episode of the
// parsed season. Returns false when no show matches (skip — create no row).
// Pure: no I/O.
func classifyEpisodeAdoption(
	parsed library.ParseResult,
	size int64,
	shows []*ent.TVShow,
) (adoptDecision, bool) {
	var show *ent.TVShow
	for _, s := range shows {
		if library.TitleMatchesAny(
			parsed.Title, s.Title, append(s.Aliases, s.OriginalTitle),
		) {
			show = s
			break
		}
	}
	if show == nil {
		return adoptDecision{}, false
	}

	ep := AdoptionEpisode(parsed, show)
	if ep == nil {
		return adoptDecision{}, false
	}
	claim := AdoptionEpisodes(parsed, show)
	if !singleEpisodeRelease(parsed) {
		// Season pack / multi-file: propose, never auto-fan.
		return adoptDecision{
			episodeID:  ep.ID,
			episodeIDs: claim,
			quality:    parsed.Resolution,
			reason:     "season pack, review manually",
		}, true
	}
	d := adoptDecision{
		episodeID: ep.ID, episodeIDs: claim, quality: parsed.Resolution,
	}
	switch {
	case sameFile(parsed, size, ep.Edges.MediaFiles):
		d.completed = true
	case episodeHasFile(ep):
		d.reason = "already have a file"
	case resolutionOK(parsed.Resolution, profileMin(show.QualityProfile)):
		d.autoImport = true
	default:
		d.reason = fmt.Sprintf("resolution %q below minimum %q",
			parsed.Resolution, profileMin(show.QualityProfile))
	}
	return d, true
}

// AdoptionEpisode resolves the episode one release should be filed against
// within one show: the matched episode for a single-episode release, and for a
// pack the first episode it can actually fill — the importer fans the directory
// out on import, so the anchor is only a handle, but a handle naming an episode
// that is already on disk says nothing about why the pack was fetched. nil when
// the show has no counterpart for what the name claims, which for a pack means
// the season itself is missing or empty.
//
// Exported because identifying a proposal by hand (a title the library did not
// have when the torrent was adopted) has to land on the same episode the
// automatic path would have chosen.
func AdoptionEpisode(
	parsed library.ParseResult, show *ent.TVShow,
) *ent.Episode {
	if singleEpisodeRelease(parsed) {
		return library.MatchEpisode(
			parsed,
			show.Edges.Seasons,
			show.Type == tvshow.TypeAnime,
		)
	}
	return packAnchor(adoptionScope(parsed, show))
}

// adoptionScope is the seasons a pack covers within one show — the set its
// anchor is picked from, and the set its claim is drawn from, so the two can
// never disagree about what the release is for.
func adoptionScope(
	parsed library.ParseResult, show *ent.TVShow,
) []*ent.Season {
	if parsed.SeasonPack {
		return seasonNumbered(show, parsed.Season)
	}
	// A whole-series pack ("INTEGRALE", "COMPLETE") names no season, so
	// parsed.Season is 0 — which is the *specials* season, not "unknown".
	// Anchoring there filed a six-season Kaamelott integrale against
	// S00E01 and pointed its import at the specials.
	return numberedSeasonsFirst(show)
}

// AdoptionEpisodes is every episode an adopted release is for: the one it
// matches, or a pack's whole scope. It is what the record stores as its claim,
// and the reason a record needs one is that its episode edge holds a single id
// — every state write scoped to "this record's episodes" reads the claim, and
// without one an adopted pack could only ever speak for its anchor.
//
// Episodes already holding a file stay in: the importer decides per file
// whether a pack replaces one, and a claim that dropped them would describe
// the release as smaller than it is. No writer acts on them regardless —
// pause/resume and the importing move both touch in-flight rows only.
//
// Exported alongside AdoptionEpisode because identifying a proposal by hand
// re-resolves both against the show the operator names.
func AdoptionEpisodes(
	parsed library.ParseResult, show *ent.TVShow,
) []uint32 {
	if singleEpisodeRelease(parsed) {
		ep := library.MatchEpisode(
			parsed, show.Edges.Seasons, show.Type == tvshow.TypeAnime,
		)
		if ep == nil {
			return nil
		}
		return []uint32{ep.ID}
	}
	var ids []uint32
	for _, se := range adoptionScope(parsed, show) {
		for _, e := range se.Edges.Episodes {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

// packAnchor picks the episode a pack is filed against: the first one holding
// no file, since that is what the pack was fetched to fill. Falls back to the
// first episode when everything in scope is already on disk — the pack is then
// an upgrade, and one of its episodes is as good a handle as another. nil when
// no season in seasons holds an episode.
func packAnchor(seasons []*ent.Season) *ent.Episode {
	var first *ent.Episode
	for _, se := range seasons {
		for _, e := range se.Edges.Episodes {
			if first == nil {
				first = e
			}
			if !episodeHasFile(e) {
				return e
			}
		}
	}
	return first
}

// seasonNumbered returns the show's season numbered `season`, or nothing.
func seasonNumbered(show *ent.TVShow, season uint16) []*ent.Season {
	for _, se := range show.Edges.Seasons {
		if se.Number == season {
			return []*ent.Season{se}
		}
	}
	return nil
}

// numberedSeasonsFirst orders the show's seasons ascending with the specials
// last, so a whole-series pack anchors in the numbered seasons it is about.
func numberedSeasonsFirst(show *ent.TVShow) []*ent.Season {
	out := slices.Clone(show.Edges.Seasons)
	slices.SortStableFunc(out, func(a, b *ent.Season) int {
		return cmp.Compare(specialsLast(a.Number), specialsLast(b.Number))
	})
	return out
}

func specialsLast(season uint16) uint16 {
	if season == 0 {
		return ^uint16(0)
	}
	return season
}

func singleEpisodeRelease(parsed library.ParseResult) bool {
	return !parsed.SeasonPack &&
		(parsed.Episode > 0 || parsed.AbsoluteNumber > 0)
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

	// With no client enabled there is nothing to adopt from, and the hash
	// query below is a full scan of download_records — paid every 30s on the
	// monitor tick, for a loop that then iterates over nothing.
	if len(config.EnabledDownloadClients()) == 0 {
		return nil, nil
	}
	d.gaugesOnce.Do(func() { d.registerClientGauges(ctx) })

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
			d.setReachable(dc.Name, false)
			continue
		}
		torrents, err := client.ListTorrents(ctx)
		if err != nil {
			slog.DebugContext(ctx, "adopt: list torrents failed",
				"client", dc.Name, "error", err)
			d.setReachable(dc.Name, false)
			continue
		}
		d.setReachable(dc.Name, true)
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
	adoptPruned.Add(ctx, int64(pruned))

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
	var enqueue []uint32
	for _, u := range untracked {
		parsed := library.Parse(u.t.Name)
		dec, ok := classifyMovieAdoption(parsed, u.t.Size, movies)
		if !ok {
			dec, ok = classifyEpisodeAdoption(parsed, u.t.Size, shows)
		}
		if !ok {
			// Nothing in the library matches. Filing it unidentified is what
			// keeps the operator's own add from being a prerequisite: they
			// name it from the proposal and the record links itself. Dropping
			// it silently is how a manually-added torrent for an untracked
			// title sat in the client forever with nothing to point at.
			slog.InfoContext(ctx, "adopting an unidentified torrent",
				"hash", u.t.Hash, "torrent", u.t.Name,
				"parsed_title", parsed.Title)
			dec = adoptDecision{
				reason:  reasonUnidentified,
				quality: parsed.Resolution,
			}
		}
		savePath, found := adoptionPath(u.t)
		if savePath == "" {
			slog.WarnContext(ctx, "refusing to adopt a torrent with an unsafe name",
				"hash", u.t.Hash, "torrent", u.t.Name)
			adoptCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("outcome", "unsafe_name"),
			))
			continue
		}
		if !found {
			// An auto-import here would be a record pointing at nothing: the
			// importer stats this path first thing, and a missing directory is
			// a *retryable* error, so it would burn every import attempt and
			// land terminal with an ENOENT naming a path nobody chose. A
			// proposal says the same thing on the first tick, and says it
			// where both paths can be compared.
			slog.WarnContext(
				ctx,
				"adopted torrent's files are not where streamline expects",
				"hash",
				u.t.Hash,
				"torrent",
				u.t.Name,
				"expected",
				savePath,
				"client_save_path",
				u.t.SavePath,
			)
			dec.autoImport = false
			dec.completed = false
			dec.reason = missingFilesReason(u.t.SavePath)
		}
		id, err := d.persistAdoption(ctx, u, dec, savePath)
		if err != nil {
			slog.WarnContext(ctx, "adopt: persist failed",
				"hash", u.t.Hash, "error", err)
			adoptCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("outcome", "persist_failed"),
			))
			continue
		}
		outcome := "proposal"
		switch {
		case dec.autoImport:
			outcome = "auto_import"
			enqueue = append(enqueue, id)
		case dec.completed:
			outcome = "completed"
			slog.InfoContext(ctx, "adopted a torrent already in the library",
				"hash", u.t.Hash, "torrent", u.t.Name)
		case dec.reason == reasonUnidentified:
			outcome = "unidentified"
		}
		adoptCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("outcome", outcome),
		))
	}
	span.SetAttributes(attribute.Int("adopt.enqueued", len(enqueue)))
	return enqueue, nil
}

// adoptionPath locates an adopted torrent's payload on disk, and reports
// whether it found it.
//
// The convention path — <library.download_path>/<torrent name> — wins, because
// it is what every streamline-grabbed record uses and what the importer's
// allowed_download_roots fence is configured around. The client's own reported
// save path, translated through download.path_mappings, is the fallback: a
// torrent the operator added by hand keeps whatever save path it was added
// with, and qBittorrent only relocates it to its category's path under
// Automatic Torrent Management. Merely tagging such a torrent with the managed
// category makes streamline adopt it while its files stay where they were.
//
// When neither exists the convention path comes back anyway, with false — the
// caller files a proposal rather than a record it cannot import, and the path
// is what its reason names.
func adoptionPath(t Torrent) (string, bool) {
	conventional, err := downloadSavePath(t.Name)
	if err != nil {
		return "", false
	}
	if _, err := os.Stat(conventional); err == nil {
		return conventional, true
	}
	if t.SavePath != "" {
		// downloadSavePath has already rejected any name that could climb out
		// of a root, so joining that same name onto a second root is safe.
		if alt := filepath.Join(
			MapClientPath(t.SavePath), t.Name,
		); alt != conventional {
			if _, err := os.Stat(alt); err == nil {
				return alt, true
			}
		}
	}
	return conventional, false
}

// persistAdoption writes the adoption record (importing for auto-import,
// completed for a file the library already holds, pending for a proposal) and
// returns its ID. savePath comes from adoptionPath.
func (d *download) persistAdoption(
	ctx context.Context,
	u untrackedTorrent,
	dec adoptDecision,
	savePath string,
) (uint32, error) {
	status := downloadrecord.StatusPending
	var importedAt *time.Time
	switch {
	case dec.autoImport:
		status = downloadrecord.StatusImporting
	case dec.completed:
		// imported_at is what the completed-record sweep ages on; without it
		// the row would outlive every real import.
		status = downloadrecord.StatusCompleted
		now := time.Now()
		importedAt = &now
	}
	rec, err := d.db.CreateDownloadRecord(ctx, db.CreateDownloadRecordParams{
		Title:              u.t.Name,
		Size:               u.t.Size,
		TorrentHash:        u.t.Hash,
		Status:             status,
		MovieID:            dec.movieID,
		EpisodeID:          dec.episodeID,
		WantedEpisodes:     dec.episodeIDs,
		DownloadClientName: u.clientName,
		SavePath:           savePath,
		Quality:            dec.quality,
		FailureReason:      dec.reason,
		ImportedAt:         importedAt,
	})
	if err != nil {
		return 0, err
	}
	// The record says "importing"; without this its episodes still say
	// "wanted", because nothing grabbed them here and the completion sweep's
	// MarkRecordEpisodesImporting only moves rows out of downloading/paused.
	// The series list counts a show as importing off its *episodes*, so an
	// adopted import was invisible there for its whole run. Logged rather than
	// returned: the record exists and the import will run either way, and a
	// badge is not worth failing an adoption over.
	if status == downloadrecord.StatusImporting {
		if err := d.db.MarkWantedRecordEpisodesImporting(ctx, rec.ID); err != nil {
			slog.WarnContext(ctx, "adopt: mark episodes importing failed",
				"record.id", rec.ID, "error", err)
		}
	}
	return rec.ID, nil
}
