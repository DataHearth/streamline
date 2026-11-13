package db

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/ent/season"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

// withEpisodeContext eager-loads the Episode edge of an importing record along
// with its Season and TVShow, plus all of the show's seasons and their episodes
// — giving the importer the full episode set needed to match season-pack files
// (and anime absolute numbers) back to episodes.
func withEpisodeContext(q *ent.EpisodeQuery) {
	q.WithSeason(func(sq *ent.SeasonQuery) {
		sq.WithTvShow(func(tq *ent.TVShowQuery) {
			tq.WithSeasons(func(ssq *ent.SeasonQuery) { ssq.WithEpisodes() })
		})
	})
}

// withBookContext gives the importer what it names and places a book by: the
// makers, the slot editions and the series.
func withBookContext(q *ent.BookQuery) {
	q.WithSeries().
		WithEbookEdition().
		WithAudiobookEdition().
		WithContributions(func(cq *ent.BookContributionQuery) { cq.WithAuthor() })
}

// withAlbumContext eager-loads an album record's artist and its tracks in disc
// order, each with the files it already holds. Mirrors FindAlbumByID: the
// album importer matches downloaded files to tracks and skips covered ones.
func withAlbumContext(q *ent.AlbumQuery) {
	withAlbumArtists(q)
	q.WithTracks(func(tq *ent.TrackQuery) {
		tq.Order(ent.Asc(track.FieldDisc), ent.Asc(track.FieldPosition)).
			WithMediaFiles()
	})
}

type CreateDownloadRecordParams struct {
	Title       string
	Size        int64
	TorrentHash string
	Status      downloadrecord.Status
	MovieID     uint32
	EpisodeID   uint32
	AlbumID     uint32
	// TrackID files a single-track upload under its track; AlbumID is set too.
	TrackID  uint32
	Uploaded bool
	// ArtistID files a discography pack under its artist; AlbumIDs are the
	// albums it is expected to cover. A record has an album or an artist.
	ArtistID           uint32
	AlbumIDs           []uint32
	BookID             uint32
	BookKind           downloadrecord.BookKind
	DownloadClientName string
	IndexerName        string
	// Adoption proposals persist these so the pending queue and a later
	// import have the parsed quality, on-disk path, and human reason.
	SavePath      string
	Quality       string
	FailureReason string
	// EpisodeID is the anchor; EpisodeIDs the rest of what the download is
	// for. The anchor is linked whether or not it is listed here.
	EpisodeIDs []uint32
	// Zero value resolves to the schema default (skipped).
	SelectionState downloadrecord.SelectionState
	// The keep-set behind an "applied" SelectionState, stored at create so a
	// failed post-add confirmation can't leave the row claiming a selection
	// it can't name — the SPA renders selected_bytes against size, and an
	// empty pair there reads as "0 B of 24 GB selected".
	SelectedFiles []int
	SelectedBytes int64
	// Zero value resolves to the schema default. An upload is born importing, so
	// its mode has to land with the insert or a scan could import it without.
	ReplaceMode downloadrecord.ReplaceMode
	// Set when the record is born completed — an adoption of a torrent whose
	// file is already the library's. Nil otherwise; the importer stamps it.
	ImportedAt *time.Time
}

func (db *DB) CreateDownloadRecord(
	ctx context.Context,
	p CreateDownloadRecordParams,
) (*ent.DownloadRecord, error) {
	b := db.client.DownloadRecord.Create().
		SetNillableImportedAt(p.ImportedAt).
		SetTitle(p.Title).
		SetSize(p.Size).
		SetTorrentHash(p.TorrentHash).
		SetStatus(p.Status).
		SetDownloadClientName(p.DownloadClientName).
		SetIndexerName(p.IndexerName)
	if p.SavePath != "" {
		b = b.SetSavePath(p.SavePath)
	}
	if p.Quality != "" {
		b = b.SetQuality(p.Quality)
	}
	if p.FailureReason != "" {
		b = b.SetFailureReason(p.FailureReason)
	}
	if p.MovieID != 0 {
		b = b.SetMovieID(p.MovieID)
	}
	if p.AlbumID != 0 {
		b = b.SetAlbumID(p.AlbumID)
	}
	if p.TrackID != 0 {
		b = b.SetTrackID(p.TrackID)
	}
	if p.Uploaded {
		b = b.SetUploaded(true)
	}
	if p.ArtistID != 0 {
		b = b.SetArtistID(p.ArtistID)
	}
	b = b.AddAlbumIDs(p.AlbumIDs...)
	if p.BookID != 0 {
		b = b.SetBookID(p.BookID).SetBookKind(p.BookKind)
	}
	if p.EpisodeID != 0 {
		b = b.SetAnchorEpisodeID(p.EpisodeID).AddEpisodeIDs(p.EpisodeID)
	}
	b = b.AddEpisodeIDs(p.EpisodeIDs...)
	if p.SelectionState != "" {
		b = b.SetSelectionState(p.SelectionState)
	}
	if p.ReplaceMode != "" {
		b = b.SetReplaceMode(p.ReplaceMode)
	}
	if len(p.SelectedFiles) > 0 {
		b = b.SetSelectedFiles(p.SelectedFiles)
	}
	if p.SelectedBytes != 0 {
		b = b.SetSelectedBytes(p.SelectedBytes)
	}
	return b.Save(ctx)
}

// AllDownloadRecordHashes returns the set of non-empty torrent hashes across
// every download_record (any status). The adoption pass uses it to skip
// torrents streamline already tracks.
func (db *DB) AllDownloadRecordHashes(
	ctx context.Context,
) (map[string]struct{}, error) {
	hashes, err := db.client.DownloadRecord.Query().
		Where(downloadrecord.TorrentHashNEQ("")).
		Select(downloadrecord.FieldTorrentHash).
		Strings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list download record hashes: %w", err)
	}
	set := make(map[string]struct{}, len(hashes))
	for _, h := range hashes {
		set[h] = struct{}{}
	}
	return set, nil
}

// TorrentRecord is what the builtin torrent list shows of the newest download
// record holding a torrent: the release it was grabbed as, and the library
// title it is for, so the list can link to that title's page. At most one of
// the ids is set (AlbumArtistID beside AlbumID); none on an orphan record.
// RecordID is the record itself while Activity shows it (ShownRecordStatuses),
// so the torrent can link to its detail; 0 for an adoption proposal.
type TorrentRecord struct {
	Title         string
	RecordID      uint32
	MovieID       uint32
	SeriesID      uint32
	AlbumID       uint32
	AlbumArtistID uint32
	ArtistID      uint32
	BookID        uint32
}

// DownloadRecordsByHash maps each of hashes that some download record holds to
// the newest such record. A torrent's own name is its uploader's choice and
// can say nothing of the release — a RuTracker Omens LP is named "Lamb Of
// God" — so the builtin torrent list shows the record's title beside it.
func (db *DB) DownloadRecordsByHash(
	ctx context.Context,
	hashes []string,
) (map[string]TorrentRecord, error) {
	out := make(map[string]TorrentRecord, len(hashes))
	if len(hashes) == 0 {
		return out, nil
	}
	recs, err := db.client.DownloadRecord.Query().
		Where(downloadrecord.TorrentHashIn(hashes...)).
		Order(ent.Asc(downloadrecord.FieldID)).
		WithMovie().
		WithAnchorEpisode(func(q *ent.EpisodeQuery) {
			q.WithSeason(func(sq *ent.SeasonQuery) { sq.WithTvShow() })
		}).
		WithAlbum(func(q *ent.AlbumQuery) { q.WithArtist() }).
		WithArtist().
		WithBook().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list download records by hash: %w", err)
	}
	for _, r := range recs {
		out[r.TorrentHash] = torrentRecord(r)
	}
	return out, nil
}

func torrentRecord(r *ent.DownloadRecord) TorrentRecord {
	t := TorrentRecord{Title: r.Title}
	if slices.Contains(ShownRecordStatuses, r.Status) {
		t.RecordID = r.ID
	}
	switch e := r.Edges; {
	case e.Album != nil:
		t.AlbumID = e.Album.ID
		if e.Album.Edges.Artist != nil {
			t.AlbumArtistID = e.Album.Edges.Artist.ID
		}
	case e.Artist != nil:
		t.ArtistID = e.Artist.ID
	case e.Book != nil:
		t.BookID = e.Book.ID
	case e.AnchorEpisode != nil:
		if se := e.AnchorEpisode.Edges.Season; se != nil && se.Edges.TvShow != nil {
			t.SeriesID = se.Edges.TvShow.ID
		}
	case e.Movie != nil:
		t.MovieID = e.Movie.ID
	}
	return t
}

// ListPendingDownloadRecords returns one page of status=pending records with
// Movie and Episode (+ its season and show) edges eager-loaded for the
// needs-attention queue, newest first, and the count of all of them.
func (db *DB) ListPendingDownloadRecords(
	ctx context.Context,
	limit, offset uint32,
) ([]*ent.DownloadRecord, uint32, error) {
	pending := db.client.DownloadRecord.Query().
		Where(downloadrecord.StatusEQ(downloadrecord.StatusPending))
	total, err := pending.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := pending.
		WithMovie(func(mq *ent.MovieQuery) { mq.WithMediaFiles() }).
		WithAnchorEpisode(func(q *ent.EpisodeQuery) {
			q.WithMediaFiles()
			q.WithSeason(func(sq *ent.SeasonQuery) { sq.WithTvShow() })
		}).
		WithAlbum().
		WithBook().
		Order(ent.Desc(downloadrecord.FieldCreateTime)).
		Limit(int(limit)).
		Offset(int(offset)).
		All(ctx)
	return rows, numeric.SaturateU32(total), err
}

// IdentifyDownloadRecord files a record under a movie or under an anchor
// episode plus the episodes it covers. The episode set is replaced, never
// merged: a proposal re-identified to another title must not keep episode
// ids from the one adoption guessed, or every record-scoped write would reach
// the wrong series' rows.
func (db *DB) IdentifyDownloadRecord(
	ctx context.Context,
	id, movieID, episodeID uint32,
	episodeIDs []uint32,
	reason string,
) error {
	q := db.client.DownloadRecord.UpdateOneID(id).
		SetFailureReason(reason).
		ClearEpisodes().
		AddEpisodeIDs(episodeIDs...)
	if movieID != 0 {
		q = q.SetMovieID(movieID)
	}
	if episodeID != 0 {
		q = q.SetAnchorEpisodeID(episodeID).AddEpisodeIDs(episodeID)
	}
	return q.Exec(ctx)
}

func (db *DB) IdentifyAlbumDownloadRecord(
	ctx context.Context,
	id, albumID uint32,
	reason string,
) error {
	return db.client.DownloadRecord.UpdateOneID(id).
		SetFailureReason(reason).
		SetAlbumID(albumID).
		Exec(ctx)
}

func (db *DB) IdentifyBookDownloadRecord(
	ctx context.Context,
	id, bookID uint32,
	kind downloadrecord.BookKind,
	reason string,
) error {
	return db.client.DownloadRecord.UpdateOneID(id).
		SetFailureReason(reason).
		SetBookID(bookID).
		SetBookKind(kind).
		Exec(ctx)
}

// DeleteStalePendingAdoptions removes pending adoption proposals for clientName
// whose torrent_hash is no longer among liveHashes (the client's current
// managed torrents). An empty liveHashes means the client reported zero
// torrents, so every pending proposal for it is stale. Returns the count
// removed. Call only with a client that listed successfully — otherwise a
// transient outage would purge valid proposals.
//
// The live set is diffed in memory rather than bound as NOT IN: it is as long
// as the client's listing, and past SQLite's bind-variable limit (32766) the
// statement would be refused outright — every tick, so the proposals it exists
// to reclaim would never be reclaimed again.
func (db *DB) DeleteStalePendingAdoptions(
	ctx context.Context,
	clientName string,
	liveHashes []string,
) (int, error) {
	pending, err := db.client.DownloadRecord.Query().
		Where(
			downloadrecord.StatusEQ(downloadrecord.StatusPending),
			downloadrecord.DownloadClientNameEQ(clientName),
		).
		Select(downloadrecord.FieldID, downloadrecord.FieldTorrentHash).
		All(ctx)
	if err != nil {
		return 0, err
	}
	live := make(map[string]struct{}, len(liveHashes))
	for _, h := range liveHashes {
		live[h] = struct{}{}
	}
	var stale []uint32
	for _, r := range pending {
		if _, ok := live[r.TorrentHash]; !ok {
			stale = append(stale, r.ID)
		}
	}
	var deleted int
	for chunk := range slices.Chunk(stale, deleteChunk) {
		n, err := db.client.DownloadRecord.Delete().
			Where(downloadrecord.IDIn(chunk...)).
			Exec(ctx)
		deleted += n
		if err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

// DeleteOrphanedPendingAdoptions removes pending adoption proposals whose
// download client is not among clientNames. A proposal is derived from its
// client's listing, and only an enabled client is ever listed, so one whose
// client was disabled or removed is never pruned by the per-client pass — it
// would sit in every user's pending list for good. Disabling a client that comes
// back re-proposes whatever it still holds on the next tick.
func (db *DB) DeleteOrphanedPendingAdoptions(
	ctx context.Context,
	clientNames []string,
) (int, error) {
	q := db.client.DownloadRecord.Delete().Where(
		downloadrecord.StatusEQ(downloadrecord.StatusPending),
	)
	if len(clientNames) > 0 {
		q = q.Where(downloadrecord.DownloadClientNameNotIn(clientNames...))
	}
	return q.Exec(ctx)
}

// deleteChunk keeps each id-list DELETE far below SQLite's bind-variable limit.
const deleteChunk = 500

// FindPendingDownloadRecordByID returns a single status=pending record with its
// media edges, or ent NotFound.
func (db *DB) FindPendingDownloadRecordByID(
	ctx context.Context,
	id uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.ID(id),
			downloadrecord.StatusEQ(downloadrecord.StatusPending),
		).
		WithMovie().
		// Season → show comes along because the pending-proposal preview
		// resolves the pack against the show's whole episode tree, and an
		// episode with no season loaded is indistinguishable from no episode
		// at all.
		WithAnchorEpisode(func(q *ent.EpisodeQuery) {
			q.WithSeason(func(sq *ent.SeasonQuery) { sq.WithTvShow() })
		}).
		WithAlbum().
		WithBook().
		Only(ctx)
}

// DeletePendingDownloadRecord removes one status=pending record, reporting
// whether a row was there to remove.
//
// Deleting rather than dismissing is the point: AllDownloadRecordHashes — what
// the adoption sweep treats as "already tracked" — selects every record with a
// hash and does not look at status, so a dismissed proposal keeps its torrent
// out of every future sweep. The status guard keeps this off records that are
// downloading, importing or done, where the row is the bookkeeping.
func (db *DB) DeletePendingDownloadRecord(
	ctx context.Context,
	id uint32,
) (bool, error) {
	n, err := db.client.DownloadRecord.Delete().
		Where(
			downloadrecord.ID(id),
			downloadrecord.StatusEQ(downloadrecord.StatusPending),
		).
		Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("delete pending download record: %w", err)
	}
	return n > 0, nil
}

// LatestImportedRecordForMovie returns the most recent record for a movie that
// carries a torrent hash (so file-delete can remove the source torrent). ent
// NotFound when none.
func (db *DB) LatestImportedRecordForMovie(
	ctx context.Context,
	movieID uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.HasMovieWith(movie.ID(movieID)),
			downloadrecord.TorrentHashNEQ(""),
		).
		Order(ent.Desc(downloadrecord.FieldCreateTime)).
		First(ctx)
}

// LatestImportedRecordForEpisode is the episode twin of the above. It matches
// every episode a record covers, not only its anchor — a season pack is the
// torrent behind each of its episodes' files — and so only a completed import:
// a pack also links episodes it has not produced yet (an upgrade still
// downloading, a proposal the operator dismissed and left seeding), and those
// torrents are not this file's to remove.
func (db *DB) LatestImportedRecordForEpisode(
	ctx context.Context,
	episodeID uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.HasEpisodesWith(episode.ID(episodeID)),
			downloadrecord.StatusEQ(downloadrecord.StatusCompleted),
			downloadrecord.TorrentHashNEQ(""),
		).
		Order(ent.Desc(downloadrecord.FieldImportedAt)).
		First(ctx)
}

// ListMoviesForAdoption returns every movie as an adoption-match candidate.
// The set is small (in-memory matched against untracked torrent names), so a
// full fetch is fine.
func (db *DB) ListMoviesForAdoption(ctx context.Context) ([]*ent.Movie, error) {
	return db.client.Movie.Query().WithMediaFiles().All(ctx)
}

// ListTvShowsForAdoption returns every show with its seasons → episodes →
// media files eager-loaded, so the adoption pass can match a torrent to an
// episode and check whether that episode already has a file in-memory.
func (db *DB) ListTvShowsForAdoption(ctx context.Context) ([]*ent.TVShow, error) {
	return db.client.TVShow.Query().
		WithSeasons(func(sq *ent.SeasonQuery) {
			sq.WithEpisodes(func(eq *ent.EpisodeQuery) { eq.WithMediaFiles() })
		}).
		All(ctx)
}

// ListDownloadingRecordsWithMovie returns every "downloading"
// download_record with its Movie and anchor Episode edges preloaded. Used by the orphan-torrent
// reconciliation pass.
func (db *DB) ListDownloadingRecordsWithMovie(
	ctx context.Context,
) ([]*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(downloadrecord.StatusEQ(downloadrecord.StatusDownloading)).
		WithMovie().
		WithAnchorEpisode().
		All(ctx)
}

func (db *DB) UpdateDownloadRecordStatus(
	ctx context.Context,
	id uint32,
	status downloadrecord.Status,
) error {
	return db.client.DownloadRecord.UpdateOneID(id).SetStatus(status).Exec(ctx)
}

// FailDownloadRecord finalizes a record that failed after the torrent was
// already added to the client — the §6 magnet zero-match arm, where the
// torrent is removed with its files and there is no held state to route
// through FailHeldDownloadRecord. Episode/movie status is left alone: the
// download-monitor reconcile sweep (RevertOrphanedDownloadingEpisodes) is
// what un-strands it, same as any other record that goes away underneath it.
func (db *DB) FailDownloadRecord(
	ctx context.Context,
	id uint32,
	reason string,
) error {
	return db.client.DownloadRecord.UpdateOneID(id).
		SetStatus(downloadrecord.StatusFailed).
		SetFailureReason(reason).
		Exec(ctx)
}

var replaceModeRank = map[downloadrecord.ReplaceMode]int{
	downloadrecord.ReplaceModeNone:     0,
	downloadrecord.ReplaceModeUpgrades: 1,
	downloadrecord.ReplaceModeAll:      2,
}

// SetDownloadRecordReplaceMode raises a record's replace mode and never
// lowers it (none < upgrades < all). A §4.6 widen hands the caller an
// existing record, and the caller's stamp must not downgrade the intent
// the original grab recorded — a feed widen over a manual replace-grab
// would otherwise quietly revoke the operator's overwrite. Fresh records
// start at none, so every first stamp is a raise; a refused lower is a
// silent no-op by design.
func (db *DB) SetDownloadRecordReplaceMode(
	ctx context.Context,
	id uint32,
	mode downloadrecord.ReplaceMode,
) error {
	lowerOrEqual := make([]downloadrecord.ReplaceMode, 0, 3)
	for m, r := range replaceModeRank {
		if r <= replaceModeRank[mode] {
			lowerOrEqual = append(lowerOrEqual, m)
		}
	}
	_, err := db.client.DownloadRecord.Update().
		Where(
			downloadrecord.IDEQ(id),
			downloadrecord.ReplaceModeIn(lowerOrEqual...),
		).
		SetReplaceMode(mode).
		Save(ctx)
	return err
}

// SetDownloadRecordSelection writes the resolution of a file selection: the
// state it landed in, the file indices actually selected, and their summed
// size. Called once the client's file listing is known — before that only the
// record's episodes (the intent) are set.
func (db *DB) SetDownloadRecordSelection(
	ctx context.Context,
	id uint32,
	state downloadrecord.SelectionState,
	files []int,
	selectedBytes int64,
) error {
	return db.client.DownloadRecord.UpdateOneID(id).
		SetSelectionState(state).
		SetSelectedFiles(files).
		SetSelectedBytes(selectedBytes).
		Exec(ctx)
}

// AddDownloadRecordEpisodes links more episodes to a record. A season pack can
// grow its selection as more episodes are confirmed wanted between the grab
// and file-listing resolving; ids already linked are left as they are.
func (db *DB) AddDownloadRecordEpisodes(
	ctx context.Context,
	id uint32,
	eps []uint32,
) error {
	return db.client.DownloadRecord.UpdateOneID(id).
		AddEpisodeIDs(eps...).
		Exec(ctx)
}

// RecordEpisodeIDs lists the ids of rec's eager-loaded episodes edge; empty
// when the edge was not loaded or the record covers none.
func RecordEpisodeIDs(rec *ent.DownloadRecord) []uint32 {
	ids := make([]uint32, 0, len(rec.Edges.Episodes))
	for _, e := range rec.Edges.Episodes {
		ids = append(ids, e.ID)
	}
	return ids
}

// ListPendingSelectionRecords returns records still awaiting file-selection
// resolution (selection_state=pending), with episode context eager-loaded for
// the phase-4 pass that maps selected files back to episodes.
func (db *DB) ListPendingSelectionRecords(
	ctx context.Context,
) ([]*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(downloadrecord.SelectionStateEQ(downloadrecord.SelectionStatePending)).
		WithAnchorEpisode(withEpisodeContext).
		WithEpisodes().
		All(ctx)
}

type RecordImportSuccessParams struct {
	RecordID uint32
	MovieID  uint32
	File     MediaFileRow
	// QueueTranscode rides the same tx as the MediaFile create.
	QueueTranscode bool
}

type MediaFileRow struct {
	Path         string
	Size         int64
	Quality      string
	Format       string
	ReleaseGroup string
	// Parsed is the release name's parse, when the caller has it. Nil falls
	// back to parsing Path's basename — see applyParsed.
	Parsed *library.ParseResult
	Probe  *ffmpeg.Info // nil leaves probed_at NULL for the backfill
}

type RecordImportFailureParams struct {
	RecordID uint32
	// Exactly one of MovieID / EpisodeID is set, identifying the media this
	// record imports. On terminal failure the movie flips to failed; the
	// episode flips back to wanted so the next search re-grabs it — unless it
	// already holds a media file, in which case "wanted" would be a lie and
	// the episode is left as-is.
	// An album record sets AlbumID instead: a terminal failure walks a
	// downloading album back to wanted, unless it already holds a file.
	// Requeue sends an incomplete album to wanted rather than failed. An upload's
	// failure sets it: failed exists to keep the searches off a bad release, and
	// a bad upload is no reason for them to stop looking.
	Requeue   bool
	MovieID   uint32
	EpisodeID uint32
	// BookID / BookKind identify the book slot a book record imports. On
	// terminal failure the slot goes back to wanted unless it still holds a
	// file of that kind.
	BookID   uint32
	BookKind mediafile.BookKind
	AlbumID  uint32
	// PackAlbumIDs are the albums a discography pack was linked to; on
	// terminal failure each one still downloading and holding no file goes
	// back to wanted.
	PackAlbumIDs []uint32
	Terminal     bool
	Reason       string
	Attempts     uint8
}

// ListImportingDownloadRecords returns records currently in status=importing.
// Used by the import_scan scheduler job for restart-safety.
func (db *DB) ListImportingDownloadRecords(
	ctx context.Context,
) ([]*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(downloadrecord.StatusEQ(downloadrecord.StatusImporting)).
		WithMovie().
		WithAnchorEpisode(withEpisodeContext).
		WithBook(withBookContext).
		WithAlbum(withAlbumContext).
		WithArtist().
		WithAlbums(withAlbumContext).
		All(ctx)
}

// FindImportingDownloadRecordByID fetches a single importing record by ID with
// its Movie + DownloadClient edges eager-loaded. Returns ent.NotFound when the
// record is absent or no longer in status=importing (both treated as "nothing
// to do" by the worker).
func (db *DB) FindImportingDownloadRecordByID(
	ctx context.Context,
	id uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.ID(id),
			downloadrecord.StatusEQ(downloadrecord.StatusImporting),
		).
		WithMovie().
		WithAnchorEpisode(withEpisodeContext).
		WithBook(withBookContext).
		WithAlbum(withAlbumContext).
		WithArtist().
		WithAlbums(withAlbumContext).
		WithTrack().
		Only(ctx)
}

// FindHeldAlbumRecord returns a held record with what a track plan reads. A
// record in any other status is not found.
func (db *DB) FindHeldAlbumRecord(
	ctx context.Context,
	id uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.ID(id),
			downloadrecord.StatusEQ(downloadrecord.StatusHeld),
		).
		WithAlbum(withAlbumContext).
		WithArtist().
		WithAlbums(withAlbumContext).
		WithTrack().
		Only(ctx)
}

// FindDownloadRecordByID returns one record by ID whatever its status, so a
// caller can tell "no such record" from "wrong state for this action".
func (db *DB) FindDownloadRecordByID(
	ctx context.Context,
	id uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Get(ctx, id)
}

// HoldDownloadRecord flips an importing record to held with the reasons the
// verifier produced, stopping the import until a user resolves it.
func (db *DB) HoldDownloadRecord(
	ctx context.Context,
	id uint32,
	reasons []schema.HoldReason,
) error {
	return db.client.DownloadRecord.UpdateOneID(id).
		SetStatus(downloadrecord.StatusHeld).
		SetHoldReasons(reasons).
		Exec(ctx)
}

// HoldDownloadingRecord holds a downloading record with reasons, filtered on
// downloading so a record that moved on since it was read is left alone.
// Reports whether the record was held.
func (db *DB) HoldDownloadingRecord(
	ctx context.Context,
	id uint32,
	reasons []schema.HoldReason,
) (bool, error) {
	err := db.client.DownloadRecord.UpdateOneID(id).
		Where(downloadrecord.StatusEQ(downloadrecord.StatusDownloading)).
		SetStatus(downloadrecord.StatusHeld).
		SetHoldReasons(reasons).
		Exec(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("hold downloading record: %w", err)
	}
	return true, nil
}

// ListHeldRecordsByCheck returns the held records whose every hold reason is
// check, with the movie and anchor episode the download monitor reads.
func (db *DB) ListHeldRecordsByCheck(
	ctx context.Context,
	check string,
) ([]*ent.DownloadRecord, error) {
	recs, err := db.client.DownloadRecord.Query().
		Where(downloadrecord.StatusEQ(downloadrecord.StatusHeld)).
		WithMovie().
		WithAnchorEpisode().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list held records: %w", err)
	}
	return slices.DeleteFunc(recs, func(r *ent.DownloadRecord) bool {
		return len(r.HoldReasons) == 0 ||
			slices.ContainsFunc(r.HoldReasons, func(h schema.HoldReason) bool {
				return h.Check != check
			})
	}), nil
}

// SetHeldRecordReasons rewrites a held record's reasons and nothing else, so
// the status hook records no second hold. Filtered on held.
func (db *DB) SetHeldRecordReasons(
	ctx context.Context,
	id uint32,
	reasons []schema.HoldReason,
) error {
	err := db.client.DownloadRecord.UpdateOneID(id).
		Where(downloadrecord.StatusEQ(downloadrecord.StatusHeld)).
		SetHoldReasons(reasons).
		Exec(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("set held record reasons: %w", err)
	}
	return nil
}

// ReleaseLocatedRecord moves a record held for its payload's location to
// importing from savePath, the path the payload was found at, clearing its
// reasons in the same write. Filtered on held, so a record resolved by hand in
// the meantime is left alone. Reports whether the record moved.
func (db *DB) ReleaseLocatedRecord(
	ctx context.Context,
	id uint32,
	savePath string,
) (bool, error) {
	err := db.client.DownloadRecord.UpdateOneID(id).
		Where(downloadrecord.StatusEQ(downloadrecord.StatusHeld)).
		SetStatus(downloadrecord.StatusImporting).
		SetSavePath(savePath).
		ClearHoldReasons().
		Exec(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("release located record: %w", err)
	}
	return true, nil
}

// FindHeldDownloadRecordByID fetches a single held record by ID with its Movie
// and Episode context eager-loaded. Returns ent.NotFound when the record is
// absent or no longer held.
func (db *DB) FindHeldDownloadRecordByID(
	ctx context.Context,
	id uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.ID(id),
			downloadrecord.StatusEQ(downloadrecord.StatusHeld),
		).
		WithMovie().
		WithAnchorEpisode(withEpisodeContext).
		WithAlbum(withAlbumContext).
		WithArtist().
		WithAlbums(withAlbumContext).
		Only(ctx)
}

// ReleaseHeldDownloadRecord flips a held record back to importing with
// verification bypassed, so the importer's re-run imports it as-is. Non-nil
// tracks or folders replace the album's or the pack's matching on that run.
func (db *DB) ReleaseHeldDownloadRecord(
	ctx context.Context,
	id uint32,
	tracks, folders map[string]uint32,
) error {
	update := db.client.DownloadRecord.UpdateOneID(id).
		SetStatus(downloadrecord.StatusImporting).
		SetVerificationBypassed(true).
		ClearHoldReasons()
	if tracks != nil {
		update.SetTrackAssignments(tracks)
	}
	if folders != nil {
		update.SetFolderAssignments(folders)
	}
	return update.Exec(ctx)
}

// FailHeldDownloadRecord finalizes a held record the user rejected, and marks
// it rejected: RejectedReleases reads it back so the automatic grab paths
// never take the same release for the same item again. requeue
// reverts the movie to wanted so a search finds a replacement; without it the
// movie stays failed, the user having judged the release themselves. Episodes
// revert to wanted either way, mirroring RecordImportFailure, every episode a
// pack left in flight included (settleRecordEpisodes) — unless the
// episode still holds a file: an upgrade grab anchors its record on an
// episode it was replacing, and rejecting the replacement leaves that file in
// place, so "wanted" would claim we have nothing. Albums follow the movie rule,
// except that one holding every track goes back to available.
func (db *DB) FailHeldDownloadRecord(
	ctx context.Context,
	id uint32,
	reason string,
	requeue bool,
) error {
	rec, err := db.FindHeldDownloadRecordByID(ctx, id)
	if err != nil {
		return err
	}
	return db.rejectRecord(ctx, rec, reason, requeue)
}

// RejectDownloadingRecord fails a record still downloading as rejected and
// sends what it was for back to wanted: a release found unimportable before it
// finished, which the automatic searches must not pick again.
func (db *DB) RejectDownloadingRecord(
	ctx context.Context,
	id uint32,
	reason string,
) error {
	rec, err := db.client.DownloadRecord.Query().
		Where(
			downloadrecord.ID(id),
			downloadrecord.StatusEQ(downloadrecord.StatusDownloading),
		).
		WithMovie().
		WithAnchorEpisode(withEpisodeContext).
		WithAlbum(withAlbumContext).
		WithArtist().
		WithAlbums(withAlbumContext).
		Only(ctx)
	if err != nil {
		return err
	}
	return db.rejectRecord(ctx, rec, reason, true)
}

// ListDownloadingAlbumRecords returns the album records still downloading,
// each with its album's id.
func (db *DB) ListDownloadingAlbumRecords(
	ctx context.Context,
) ([]*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.StatusEQ(downloadrecord.StatusDownloading),
			downloadrecord.HasAlbum(),
			downloadrecord.TorrentHashNEQ(""),
		).
		WithAlbum(func(q *ent.AlbumQuery) { q.Select(album.FieldID) }).
		All(ctx)
}

func (db *DB) rejectRecord(
	ctx context.Context,
	rec *ent.DownloadRecord,
	reason string,
	requeue bool,
) error {
	id := rec.ID
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := tx.DownloadRecord.UpdateOneID(id).
		SetStatus(downloadrecord.StatusFailed).
		SetFailureReason(reason).
		SetRejected(true).
		ClearHoldReasons().
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update download record: %w", err)
	}
	if rec.Edges.Movie != nil {
		status := movie.StatusFailed
		if requeue {
			status = movie.StatusWanted
		}
		if err := tx.Movie.UpdateOneID(rec.Edges.Movie.ID).
			SetStatus(status).
			SetFailureReason(reason).
			Exec(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("update movie: %w", err)
		}
	}
	if rec.Edges.AnchorEpisode != nil {
		if _, err := tx.Episode.Update().
			Where(
				episode.ID(rec.Edges.AnchorEpisode.ID),
				episode.Not(episode.HasMediaFiles()),
			).
			SetStatus(episode.StatusWanted).
			Save(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("update episode: %w", err)
		}
	}
	if err := settleRecordEpisodes(ctx, tx, id); err != nil {
		tx.Rollback()
		return err
	}
	var albumIDs []uint32
	if rec.Edges.Album != nil {
		albumIDs = append(albumIDs, rec.Edges.Album.ID)
	}
	for _, a := range rec.Edges.Albums {
		albumIDs = append(albumIDs, a.ID)
	}
	if err := settleFailedAlbums(
		ctx,
		tx,
		albumIDs,
		reason,
		requeue || rec.Uploaded,
	); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// completeAlbum is an album whose every track holds a file. One with no track
// rows yet is not complete.
func completeAlbum() predicate.Album {
	return album.And(
		album.HasTracks(),
		album.Not(album.HasTracksWith(track.Not(track.HasMediaFiles()))),
	)
}

// settleFailedAlbums moves the downloading albums of a record that failed: one
// holding every track back to available, any other to failed with the reason,
// or to wanted when requeue. A partial album goes to failed rather than wanted
// because the missing search would grab the same release again.
func settleFailedAlbums(
	ctx context.Context,
	tx *ent.Tx,
	ids []uint32,
	reason string,
	requeue bool,
) error {
	if len(ids) == 0 {
		return nil
	}
	downloading := album.And(
		album.IDIn(ids...),
		album.StatusEQ(album.StatusDownloading),
	)
	if err := tx.Album.Update().
		Where(downloading, completeAlbum()).
		SetStatus(album.StatusAvailable).
		Exec(ctx); err != nil {
		return fmt.Errorf("return complete albums to available: %w", err)
	}
	// Whatever is still downloading after the update above is incomplete.
	u := tx.Album.Update().Where(downloading)
	if requeue {
		u = u.SetStatus(album.StatusWanted)
	} else {
		u = u.SetStatus(album.StatusFailed).SetFailureReason(reason)
	}
	if err := u.Exec(ctx); err != nil {
		return fmt.Errorf("settle failed albums: %w", err)
	}
	return nil
}

// RecordImportSuccess writes MediaFile row, flips DownloadRecord to completed,
// flips Movie to available — all in one tx. On error, caller retries.
func (db *DB) RecordImportSuccess(
	ctx context.Context,
	p RecordImportSuccessParams,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}

	mc := applyParsed(tx.MediaFile.Create().
		SetPath(p.File.Path).
		SetSize(p.File.Size).
		SetQuality(p.File.Quality).
		SetFormat(p.File.Format).
		SetReleaseGroup(p.File.ReleaseGroup).
		SetMovieID(p.MovieID), p.File.Parsed, p.File.Path)
	if p.File.Probe != nil {
		mc = applyProbe(mc, p.File.Probe)
	}
	mf, err := mc.Save(ctx)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("create media file: %w", err)
	}
	if p.QueueTranscode {
		if _, err := tx.TranscodeJob.Create().
			SetMediaFile(mf).
			Save(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("queue transcode job: %w", err)
		}
	}
	if err := tx.DownloadRecord.UpdateOneID(p.RecordID).
		SetStatus(downloadrecord.StatusCompleted).
		SetImportedAt(time.Now()).
		SetFailureReason("").
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update download record: %w", err)
	}
	if err := tx.Movie.UpdateOneID(p.MovieID).
		SetStatus(movie.StatusAvailable).
		SetFailureReason("").
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update movie: %w", err)
	}
	return tx.Commit()
}

type RecordEpisodeImportSuccessParams struct {
	RecordID  uint32
	EpisodeID uint32
	File      MediaFileRow
	// QueueTranscode rides the same tx as the MediaFile create.
	QueueTranscode bool
}

// RecordEpisodeImportSuccess mirrors RecordImportSuccess for the TV path:
// writes the MediaFile (owned by the episode), flips the DownloadRecord to
// completed, and marks the Episode available — all in one tx. Used per-file so
// a season pack records each matched episode independently.
func (db *DB) RecordEpisodeImportSuccess(
	ctx context.Context,
	p RecordEpisodeImportSuccessParams,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	ec := applyParsed(tx.MediaFile.Create().
		SetPath(p.File.Path).
		SetSize(p.File.Size).
		SetQuality(p.File.Quality).
		SetFormat(p.File.Format).
		SetReleaseGroup(p.File.ReleaseGroup).
		SetEpisodeID(p.EpisodeID), p.File.Parsed, p.File.Path)
	if p.File.Probe != nil {
		ec = applyProbe(ec, p.File.Probe)
	}
	mf, err := ec.Save(ctx)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("create media file: %w", err)
	}
	if p.QueueTranscode {
		if _, err := tx.TranscodeJob.Create().
			SetMediaFile(mf).
			Save(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("queue transcode job: %w", err)
		}
	}
	if err := tx.DownloadRecord.UpdateOneID(p.RecordID).
		SetStatus(downloadrecord.StatusCompleted).
		SetImportedAt(time.Now()).
		SetFailureReason("").
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update download record: %w", err)
	}
	if err := tx.Episode.UpdateOneID(p.EpisodeID).
		SetStatus(episode.StatusAvailable).
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update episode: %w", err)
	}
	return tx.Commit()
}

type RecordAlbumImportSuccessParams struct {
	RecordID uint32
	AlbumID  uint32
	Files    []AdoptAlbumFile
	// KeepRecord leaves the record as it is: one album of a discography pack
	// was imported and the record completes only when every album is done.
	KeepRecord bool
}

// RecordAlbumImportSuccess is the album twin of RecordEpisodeImportSuccess:
// one MediaFile per imported track, the record completed, and the album
// available once every track holds a file. Otherwise it goes back to wanted,
// leaving downloading so the missing tracks are searchable again.
//
// Not adoptAlbumFiles: that writes source=orphan and refuses to move an album
// out of downloading.
func (db *DB) RecordAlbumImportSuccess(
	ctx context.Context,
	p RecordAlbumImportSuccessParams,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	for _, f := range p.Files {
		if err := tx.MediaFile.Create().
			SetPath(f.Path).
			SetSize(f.Size).
			SetQuality(f.Quality).
			SetFormat(f.Format).
			SetSource(mediafile.SourceAuto).
			SetTrackID(f.TrackID).
			SetNillableTagsState(tagsPending(f.TagsPending)).
			Exec(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("create media file %s: %w", f.Path, err)
		}
	}
	if !p.KeepRecord {
		if err := tx.DownloadRecord.UpdateOneID(p.RecordID).
			SetStatus(downloadrecord.StatusCompleted).
			SetImportedAt(time.Now()).
			SetFailureReason("").
			Exec(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("update download record: %w", err)
		}
	}
	bare, err := tx.Track.Query().
		Where(
			track.HasAlbumWith(album.ID(p.AlbumID)),
			track.Not(track.HasMediaFiles()),
		).
		Exist(ctx)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("check album %d coverage: %w", p.AlbumID, err)
	}
	status := album.StatusAvailable
	if bare {
		status = album.StatusWanted
	}
	if err := tx.Album.Update().
		Where(
			album.ID(p.AlbumID),
			album.StatusIn(
				album.StatusDownloading,
				album.StatusWanted,
				album.StatusFailed,
			),
		).
		SetStatus(status).
		ClearFailureReason().
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update album: %w", err)
	}
	return tx.Commit()
}

// CompletePackRecord completes a discography pack's record and returns the
// albums the pack was linked to that it did not fill, from downloading back to
// wanted: they stay searchable, with no grab_failures strike, since the pack
// simply did not name them.
func (db *DB) CompletePackRecord(
	ctx context.Context,
	recordID uint32,
	unmatchedAlbumIDs []uint32,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := tx.DownloadRecord.UpdateOneID(recordID).
		SetStatus(downloadrecord.StatusCompleted).
		SetImportedAt(time.Now()).
		SetFailureReason("").
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update download record: %w", err)
	}
	if len(unmatchedAlbumIDs) > 0 {
		if err := tx.Album.Update().
			Where(
				album.IDIn(unmatchedAlbumIDs...),
				album.StatusEQ(album.StatusDownloading),
			).
			SetStatus(album.StatusWanted).
			Exec(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("return unmatched pack albums to wanted: %w", err)
		}
	}
	return tx.Commit()
}

type RecordBookImportSuccessParams struct {
	RecordID uint32
	BookID   uint32
	Kind     mediafile.BookKind
	Files    []MediaFileRow
	// ReplacedFileIDs are the slot's prior MediaFile rows, deleted in the same
	// tx that records their replacements.
	ReplacedFileIDs []uint32
}

// RecordBookImportSuccess deletes the replaced rows, writes one MediaFile per
// placed file, completes the DownloadRecord and marks the book's slot
// available — all in one tx. No probe and no transcode job: books carry
// neither.
func (db *DB) RecordBookImportSuccess(
	ctx context.Context,
	p RecordBookImportSuccessParams,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	if len(p.ReplacedFileIDs) > 0 {
		if _, err := tx.MediaFile.Delete().
			Where(mediafile.IDIn(p.ReplacedFileIDs...)).
			Exec(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("delete replaced media files: %w", err)
		}
	}
	for _, f := range p.Files {
		c := applyParsed(tx.MediaFile.Create().
			SetPath(f.Path).
			SetSize(f.Size).
			SetQuality(f.Quality).
			SetFormat(f.Format).
			SetReleaseGroup(f.ReleaseGroup).
			SetBookID(p.BookID).
			SetBookKind(p.Kind), f.Parsed, f.Path)
		if f.Probe != nil {
			c = applyProbe(c, f.Probe)
		}
		if _, err := c.Save(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("create media file: %w", err)
		}
	}
	if err := tx.DownloadRecord.UpdateOneID(p.RecordID).
		SetStatus(downloadrecord.StatusCompleted).
		SetImportedAt(time.Now()).
		SetFailureReason("").
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update download record: %w", err)
	}
	u := tx.Book.UpdateOneID(p.BookID)
	switch p.Kind {
	case mediafile.BookKindEbook:
		u = u.SetEbookStatus(book.EbookStatusAvailable).
			SetEbookReplacingLanguage("")
	case mediafile.BookKindAudiobook:
		u = u.SetAudiobookStatus(book.AudiobookStatusAvailable).
			SetAudiobookReplacingLanguage("")
	default:
		tx.Rollback()
		return fmt.Errorf("unknown book kind %q", p.Kind)
	}
	if err := u.Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update book: %w", err)
	}
	return tx.Commit()
}

// RecordImportFailure writes attempt counter on retryable; on terminal also
// flips DownloadRecord + Movie to failed with reason, walks the record's
// episodes back (settleRecordEpisodes), and settles the album (or each album
// a pack expected) through settleFailedAlbums.
func (db *DB) RecordImportFailure(
	ctx context.Context,
	p RecordImportFailureParams,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}

	u := tx.DownloadRecord.UpdateOneID(p.RecordID).SetImportAttempts(p.Attempts)
	if p.Terminal {
		u = u.SetStatus(downloadrecord.StatusFailed).SetFailureReason(p.Reason)
	}
	if err := u.Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update download record: %w", err)
	}
	if p.Terminal && p.MovieID != 0 {
		if err := tx.Movie.UpdateOneID(p.MovieID).
			SetStatus(movie.StatusFailed).
			SetFailureReason(p.Reason).
			Exec(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("update movie: %w", err)
		}
	}
	if p.Terminal && p.EpisodeID != 0 {
		// wanted means "we do not have this" — an episode a failed grab was
		// meant to replace still has its prior file, so it stays available
		// instead (this is how ErrEpisodeHasFile reaches here: the importer
		// declined every episode the release selected, but each one already
		// holds a file).
		hasFile, err := tx.MediaFile.Query().
			Where(mediafile.HasEpisodeWith(episode.ID(p.EpisodeID))).
			Exist(ctx)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("check episode media files: %w", err)
		}
		if !hasFile {
			if err := tx.Episode.UpdateOneID(p.EpisodeID).
				SetStatus(episode.StatusWanted).
				Exec(ctx); err != nil {
				tx.Rollback()
				return fmt.Errorf("update episode: %w", err)
			}
		}
	}
	if p.Terminal {
		if err := settleRecordEpisodes(ctx, tx, p.RecordID); err != nil {
			tx.Rollback()
			return err
		}
	}
	if p.Terminal {
		ids := p.PackAlbumIDs
		if p.AlbumID != 0 {
			ids = append(ids, p.AlbumID)
		}
		if err := settleFailedAlbums(ctx, tx, ids, p.Reason, p.Requeue); err != nil {
			tx.Rollback()
			return err
		}
	}
	if p.Terminal && p.BookID != 0 {
		hasFile, err := tx.MediaFile.Query().
			Where(
				mediafile.HasBookWith(book.ID(p.BookID)),
				mediafile.BookKindEQ(p.BookKind),
			).
			Exist(ctx)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("check book media files: %w", err)
		}
		if !hasFile {
			u := tx.Book.Update().Where(book.ID(p.BookID))
			switch p.BookKind {
			case mediafile.BookKindEbook:
				u = u.Where(book.EbookStatusEQ(book.EbookStatusDownloading)).
					SetEbookStatus(book.EbookStatusWanted)
			case mediafile.BookKindAudiobook:
				u = u.Where(book.AudiobookStatusEQ(book.AudiobookStatusDownloading)).
					SetAudiobookStatus(book.AudiobookStatusWanted)
			}
			if err := u.Exec(ctx); err != nil {
				tx.Rollback()
				return fmt.Errorf("update book: %w", err)
			}
		}
	}
	return tx.Commit()
}

// recordEpisodesWanted selects the record's anchor while it is still
// "wanted" — the row an import is about to take over. MarkRecordEpisodesImporting
// skips "wanted" because a grab marked its episodes downloading; both callers
// here are the case where nothing did.
//
// The anchor, not the whole set: an adopted whole-series pack links every
// numbered episode of the show — unaired ones, and seasons the torrent never
// held — and none of the set's wanted rows can be told apart from those. Moved
// to importing they sit out every missing search for as long as a hold lasts.
// The rest of the set stays wanted, which no search acts on while this record
// is in flight: ListEligibleEpisodesForSync excludes every episode it links.
func recordEpisodesWanted(recordID uint32) predicate.Episode {
	return episode.And(
		episode.HasAnchoredDownloadRecordsWith(downloadrecord.ID(recordID)),
		episode.StatusEQ(episode.StatusWanted),
	)
}

// MarkWantedRecordEpisodesImporting moves a record's anchor episode from
// "wanted" to "importing" — see recordEpisodesWanted for why not its whole set.
//
// Adoption needs this and the normal completion sweep does not: a grab
// streamline issued left its episodes "downloading", which is what
// MarkRecordEpisodesImporting moves. An adopted torrent was never grabbed
// here, so its episodes sit at "wanted" and no other path moves them — the
// record would read "importing" while every episode behind it still read
// "wanted", so the series list's importing facet (which counts shows by
// episode status) would report nothing while an import was running.
//
// Episodes that already hold a file are left alone: an adopted upgrade is not
// a gap being filled, and RecordImportFailure would have no way to walk an
// "available" episode back.
func (db *DB) MarkWantedRecordEpisodesImporting(
	ctx context.Context,
	recordID uint32,
) error {
	if _, err := db.client.Episode.Update().
		Where(recordEpisodesWanted(recordID)).
		SetStatus(episode.StatusImporting).
		Save(ctx); err != nil {
		return fmt.Errorf("mark wanted record episodes importing: %w", err)
	}
	return nil
}

// RetryFailedDownloadRecord puts a terminally-failed record back in front of
// the importer: importing, attempt counter cleared, failure reason dropped.
//
// The media it owns is walked back with it, because the terminal failure moved
// that too — RecordImportFailure flips the movie and the albums to failed and
// the linked episode to wanted. Leaving either behind would have the retry importing into
// rows that claim nothing is coming, and the missing-search would grab a
// second release for an episode already being imported.
//
// Only rows this record owns are touched: its episodes, not the season.
// MarkRecordEpisodesImporting deliberately leaves "wanted" alone because a
// wanted episode is usually none of a running grab's business — here "wanted"
// is precisely what this record's own failure wrote.
//
// A rejected record loses the mark: retrying it is the user taking the
// rejection back, and the release is once again one a search may grab.
func (db *DB) RetryFailedDownloadRecord(ctx context.Context, id uint32) error {
	rec, err := db.client.DownloadRecord.Query().
		Where(
			downloadrecord.ID(id),
			downloadrecord.StatusEQ(downloadrecord.StatusFailed),
		).
		WithMovie().
		WithAlbum().
		WithAlbums().
		Only(ctx)
	if err != nil {
		return err
	}

	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := tx.DownloadRecord.UpdateOneID(id).
		SetStatus(downloadrecord.StatusImporting).
		SetImportAttempts(0).
		SetFailureReason("").
		SetRejected(false).
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update download record: %w", err)
	}
	if rec.Edges.Movie != nil {
		if err := tx.Movie.UpdateOneID(rec.Edges.Movie.ID).
			SetStatus(movie.StatusImporting).
			SetFailureReason("").
			Exec(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("update movie: %w", err)
		}
	}
	if _, err := tx.Episode.Update().
		Where(recordEpisodesWanted(id)).
		SetStatus(episode.StatusImporting).
		Save(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update episodes: %w", err)
	}
	var albumIDs []uint32
	if rec.Edges.Album != nil {
		albumIDs = append(albumIDs, rec.Edges.Album.ID)
	}
	for _, a := range rec.Edges.Albums {
		albumIDs = append(albumIDs, a.ID)
	}
	if len(albumIDs) > 0 {
		from := []album.Status{album.StatusFailed}
		// A failed upload left its album wanted, not failed.
		if rec.Uploaded {
			from = append(from, album.StatusWanted)
		}
		if err := tx.Album.Update().
			Where(album.IDIn(albumIDs...), album.StatusIn(from...)).
			SetStatus(album.StatusDownloading).
			ClearFailureReason().
			Exec(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("update albums: %w", err)
		}
	}
	return tx.Commit()
}

// DeleteCompletedDownloadRecordsBefore deletes records whose status is
// completed and whose imported_at is older than cutoff, sparing any whose
// torrent_hash is in keepHashes. Returns the number of rows deleted.
func (db *DB) DeleteCompletedDownloadRecordsBefore(
	ctx context.Context,
	cutoff time.Time,
	keepHashes []string,
) (int, error) {
	return db.client.DownloadRecord.Delete().Where(sparingHashes(keepHashes,
		downloadrecord.StatusEQ(downloadrecord.StatusCompleted),
		downloadrecord.ImportedAtLT(cutoff),
	)...).Exec(ctx)
}

// DeleteFailedDownloadRecordsBefore deletes records whose status is failed and
// whose update_time is older than cutoff, sparing any whose torrent_hash is in
// keepHashes. Returns the number of rows deleted. A rejected record is kept
// however old it is: it is the blocklist entry, and purging it would hand the
// release back to the next automatic search. The user forgets one by deleting
// it from the history.
func (db *DB) DeleteFailedDownloadRecordsBefore(
	ctx context.Context,
	cutoff time.Time,
	keepHashes []string,
) (int, error) {
	return db.client.DownloadRecord.Delete().Where(sparingHashes(keepHashes,
		downloadrecord.StatusEQ(downloadrecord.StatusFailed),
		downloadrecord.UpdateTimeLT(cutoff),
		downloadrecord.Rejected(false),
	)...).Exec(ctx)
}

// sparingHashes appends to preds the condition that a record's torrent is not
// one of keepHashes. torrent_hash is nullable and NULL NOT IN (...) is NULL, so
// without the IsNil arm a hashless record would never be purged again.
func sparingHashes(
	keepHashes []string,
	preds ...predicate.DownloadRecord,
) []predicate.DownloadRecord {
	if len(keepHashes) == 0 {
		return preds
	}
	return append(preds, downloadrecord.Or(
		downloadrecord.TorrentHashIsNil(),
		downloadrecord.TorrentHashNotIn(keepHashes...),
	))
}

// SetDownloadRecordSavePath persists save_path so import_scan can resume
// after a restart without re-querying the download client.
func (db *DB) SetDownloadRecordSavePath(
	ctx context.Context,
	id uint32,
	path string,
) error {
	return db.client.DownloadRecord.UpdateOneID(id).SetSavePath(path).Exec(ctx)
}

// CountLiveDownloadRecords counts records whose save_path something will
// still read: in-flight downloads and pending adoption proposals. Terminal
// rows (completed, failed, dismissed) keep the path they were created with
// forever, so counting them would hold the boot drift warning on long after
// the download root legitimately moved.
func (db *DB) CountLiveDownloadRecords(ctx context.Context) (int, error) {
	n, err := db.client.DownloadRecord.Query().
		Where(downloadrecord.StatusIn(
			downloadrecord.StatusDownloading,
			downloadrecord.StatusImporting,
			downloadrecord.StatusHeld,
			downloadrecord.StatusPending,
		)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count live download_records: %w", err)
	}
	return n, nil
}

func (db *DB) ListDownloadRecordsByPathPrefix(
	ctx context.Context,
	prefix string,
) ([]*ent.DownloadRecord, error) {
	rows, err := db.client.DownloadRecord.Query().
		Where(downloadrecord.SavePathHasPrefix(prefix)).
		Order(ent.Asc(downloadrecord.FieldSavePath)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list download_records under %s: %w", prefix, err)
	}
	return rows, nil
}

// ListActiveDownloadRecords returns records still in flight (downloading,
// importing or held) with the title they are for eager-loaded — movie, anchor
// episode, album (with its artist), an uploaded track, discography-pack artist
// or book.
// Powers the live queue snapshot.
func (db *DB) ListActiveDownloadRecords(
	ctx context.Context,
) ([]*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(downloadrecord.StatusIn(queueStatuses...)).
		WithMovie().
		WithAnchorEpisode(func(q *ent.EpisodeQuery) {
			q.WithSeason(func(sq *ent.SeasonQuery) { sq.WithTvShow() })
		}).
		WithAlbum(func(q *ent.AlbumQuery) { q.WithArtist() }).
		WithTrack().
		WithArtist().
		WithBook(func(q *ent.BookQuery) { q.WithSeries() }).
		WithAlbums(func(q *ent.AlbumQuery) {
			q.Select(album.FieldID, album.FieldTitle)
		}).
		// Ids only: the queue says which episodes a pack covers, nothing more.
		WithEpisodes(func(q *ent.EpisodeQuery) { q.Select(episode.FieldID) }).
		All(ctx)
}

// FindLiveDownloadRecordByHash fetches the in-flight record tracking hash, with
// its movie edge. Returns a nil row and a nil error when none matches — a
// torrent the operator added out-of-band has no record, which is not a failure.
// Used by PurgeRecordForHash (DELETE /torrents/{hash}) among others: a
// completed record is deliberately NOT "live" here — it is an already-imported
// history row (GET /activity/history serves it), and matching it would let
// deleting a torrent client-side also delete that row, plus permanently block
// re-grabbing a release whose torrent already left the client after import
// (the hash would keep resolving to "duplicate" with nothing left to dedupe
// against). See FindWidenableDownloadRecordByHash for the one caller that
// does need completed records.
func (db *DB) FindLiveDownloadRecordByHash(
	ctx context.Context,
	hash string,
) (*ent.DownloadRecord, error) {
	rec, err := db.client.DownloadRecord.Query().
		Where(
			downloadrecord.TorrentHashEQ(hash),
			downloadrecord.StatusIn(
				downloadrecord.StatusDownloading,
				downloadrecord.StatusImporting,
				downloadrecord.StatusHeld,
			),
		).
		WithMovie().
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find live download record by hash: %w", err)
	}
	return rec, nil
}

// FindImportedDownloadRecordByHash fetches the record tracking hash that the
// importer finished — status completed — or a nil row with a nil error when
// none matches. The seed-complete sweep uses it as its permission to delete a
// torrent's files: held, pending, downloading, importing and failed records
// all still have someone (the resolve flow, the adoption queue, the importer)
// waiting on those exact bytes, and an untracked torrent is not ours to reap.
func (db *DB) FindImportedDownloadRecordByHash(
	ctx context.Context,
	hash string,
) (*ent.DownloadRecord, error) {
	rec, err := db.client.DownloadRecord.Query().
		Where(
			downloadrecord.TorrentHashEQ(hash),
			downloadrecord.StatusEQ(downloadrecord.StatusCompleted),
		).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find imported download record by hash: %w", err)
	}
	return rec, nil
}

// FindWidenableDownloadRecordByHash is FindLiveDownloadRecordByHash plus
// StatusCompleted: a completed record's torrent commonly still sits in the
// client seeding, and a re-grab landing on that hash (spec §4.6) needs to find
// it so grab can widen the selection instead of adding a duplicate torrent.
// Kept separate rather than folded into FindLiveDownloadRecordByHash because a
// completed record is "live" for exactly one purpose — grab's own widen
// decision, which further gates on selection_state before treating the hit as
// anything but an ordinary duplicate. Every other caller (PurgeRecordForHash
// included) must keep seeing completed records as not-live.
func (db *DB) FindWidenableDownloadRecordByHash(
	ctx context.Context,
	hash string,
) (*ent.DownloadRecord, error) {
	rec, err := db.client.DownloadRecord.Query().
		Where(
			downloadrecord.TorrentHashEQ(hash),
			downloadrecord.StatusIn(
				downloadrecord.StatusDownloading,
				downloadrecord.StatusImporting,
				downloadrecord.StatusHeld,
				downloadrecord.StatusCompleted,
			),
		).
		WithMovie().
		WithEpisodes().
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf(
			"find widenable download record by hash: %w", err,
		)
	}
	return rec, nil
}

// FindSeedingDownloadRecord is the transcoding worker's route from a library
// file back to the torrent that produced it. An episode matches every record
// that covers it, not only the one anchored on it: a season pack's files all
// came out of the same torrent.
func (db *DB) FindSeedingDownloadRecord(
	ctx context.Context,
	movieID, episodeID uint32,
) (*ent.DownloadRecord, error) {
	var owner predicate.DownloadRecord
	switch {
	case movieID != 0:
		owner = downloadrecord.HasMovieWith(movie.ID(movieID))
	case episodeID != 0:
		owner = downloadrecord.HasEpisodesWith(episode.ID(episodeID))
	default:
		return nil, nil
	}
	rec, err := db.client.DownloadRecord.Query().
		Where(
			owner,
			downloadrecord.StatusEQ(downloadrecord.StatusCompleted),
			downloadrecord.TorrentHashNEQ(""),
			downloadrecord.DownloadClientNameNEQ(""),
		).
		Order(ent.Desc(downloadrecord.FieldImportedAt)).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find seeding download record: %w", err)
	}
	return rec, nil
}

func (db *DB) ListReleaseGrabs(
	ctx context.Context,
	movieID, showID uint32,
	hashes, titles []string,
) ([]*ent.DownloadRecord, error) {
	var owner predicate.DownloadRecord
	switch {
	case movieID != 0:
		owner = downloadrecord.HasMovieWith(movie.ID(movieID))
	case showID != 0:
		owner = downloadrecord.HasEpisodesWith(
			episode.HasSeasonWith(season.HasTvShowWith(tvshow.ID(showID))),
		)
	default:
		return nil, nil
	}
	if len(hashes) == 0 && len(titles) == 0 {
		return nil, nil
	}
	hashArgs := make([]any, 0, len(hashes))
	for _, h := range hashes {
		hashArgs = append(hashArgs, h)
	}
	titleArgs := make([]any, 0, len(titles))
	for _, t := range titles {
		titleArgs = append(titleArgs, strings.ToLower(t))
	}
	recs, err := db.client.DownloadRecord.Query().
		Where(
			owner,
			downloadrecord.StatusNotIn(
				downloadrecord.StatusPending,
				downloadrecord.StatusDismissed,
			),
			func(s *entsql.Selector) {
				s.Where(entsql.Or(
					entsql.In(s.C(downloadrecord.FieldTorrentHash), hashArgs...),
					entsql.In(
						entsql.Lower(s.C(downloadrecord.FieldTitle)),
						titleArgs...,
					),
				))
			},
		).
		Select(
			downloadrecord.FieldTitle,
			downloadrecord.FieldTorrentHash,
			downloadrecord.FieldCreateTime,
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list release grabs: %w", err)
	}
	return recs, nil
}

// FindActiveDownloadRecordByID fetches an in-flight record by ID with movie +
// download_client edges. Returns ent.NotFound when absent or already terminal.
// Held records match: the queue shows them, so the queue verbs have to be able
// to tell a held row apart from a missing one.
func (db *DB) FindActiveDownloadRecordByID(
	ctx context.Context,
	id uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.ID(id),
			downloadrecord.StatusIn(
				downloadrecord.StatusDownloading,
				downloadrecord.StatusImporting,
				downloadrecord.StatusHeld,
			),
		).
		WithMovie().
		// What a cancellation is filed against when it is not a movie: the
		// activity row needs the owner, and only its id.
		WithAnchorEpisode(func(q *ent.EpisodeQuery) { q.Select(episode.FieldID) }).
		WithAlbum(func(q *ent.AlbumQuery) { q.Select(album.FieldID) }).
		WithBook(func(q *ent.BookQuery) { q.Select(book.FieldID) }).
		WithArtist(func(q *ent.ArtistQuery) { q.Select(artist.FieldID) }).
		Only(ctx)
}

type DownloadHistoryResult struct {
	Records    []*ent.DownloadRecord
	NextCursor string
	// Total is every terminal record, not the page: the view switch badges
	// History with it, and the page length caps at the limit.
	Total int
}

// ListDownloadHistory returns terminal records (completed or failed) newest
// first, keyset-paginated on (update_time, id). Reuses the activity cursor
// scheme.
func (db *DB) ListDownloadHistory(
	ctx context.Context,
	limit int,
	cursor string,
) (*DownloadHistoryResult, error) {
	if limit <= 0 {
		limit = defaultActivityLimit
	}
	terminal := downloadrecord.StatusIn(historyStatuses...)
	total, err := db.client.DownloadRecord.Query().Where(terminal).Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("download history: count: %w", err)
	}
	q := withHistoryEdges(db.client.DownloadRecord.Query().
		Where(terminal).
		Order(
			ent.Desc(downloadrecord.FieldUpdateTime),
			ent.Desc(downloadrecord.FieldID),
		))

	if cursor != "" {
		ts, id, err := decodeActivityCursor(cursor)
		if err != nil {
			return nil, fmt.Errorf("download history: decode cursor: %w", err)
		}
		q = q.Where(downloadrecord.Or(
			downloadrecord.UpdateTimeLT(ts),
			downloadrecord.And(
				downloadrecord.UpdateTimeEQ(ts),
				downloadrecord.IDLT(id),
			),
		))
	}

	rows, err := q.Limit(limit + 1).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("download history: query: %w", err)
	}
	res := &DownloadHistoryResult{Total: total}
	if len(rows) > limit {
		res.Records = rows[:limit]
		last := res.Records[limit-1]
		res.NextCursor = encodeActivityCursor(last.UpdateTime, last.ID)
	} else {
		res.Records = rows
	}
	return res, nil
}

// queueStatuses are the in-flight states the queue lists. Held records are
// finished downloading but not done: the queue is where a user is told one is
// waiting on them.
var queueStatuses = []downloadrecord.Status{
	downloadrecord.StatusDownloading,
	downloadrecord.StatusImporting,
	downloadrecord.StatusHeld,
}

// historyStatuses are the terminal states the history lists.
var historyStatuses = []downloadrecord.Status{
	downloadrecord.StatusCompleted,
	downloadrecord.StatusFailed,
}

// ShownRecordStatuses are the states Activity shows a record in: the queue's
// and the history's, built from the two lists those views query so a status
// added to either is linked to as well. Adoption proposals (pending,
// dismissed) live in the pending sheet and are not linked to.
var ShownRecordStatuses = slices.Concat(queueStatuses, historyStatuses)

// withHistoryEdges loads what a history row names: the title it is for, down
// to the album's artist, the episode's show and the volume's series.
func withHistoryEdges(q *ent.DownloadRecordQuery) *ent.DownloadRecordQuery {
	return q.
		WithMovie().
		WithAnchorEpisode(func(q *ent.EpisodeQuery) {
			q.WithSeason(func(sq *ent.SeasonQuery) { sq.WithTvShow() })
		}).
		WithAlbum(func(q *ent.AlbumQuery) { q.WithArtist() }).
		WithTrack().
		WithArtist().
		WithAlbums(func(q *ent.AlbumQuery) {
			q.Select(album.FieldID, album.FieldTitle)
		}).
		WithBook(func(q *ent.BookQuery) { q.WithSeries() })
}

// FindDownloadHistoryEntry returns one terminal record with what its history
// row names, for a link to a record further back than the history's loaded
// pages. ent.NotFound when it is absent or not terminal.
func (db *DB) FindDownloadHistoryEntry(
	ctx context.Context,
	id uint32,
) (*ent.DownloadRecord, error) {
	return withHistoryEdges(db.client.DownloadRecord.Query().
		Where(
			downloadrecord.ID(id),
			downloadrecord.StatusIn(historyStatuses...),
		)).
		Only(ctx)
}

// ShownDownloadRecordIDs reports which of ids name a record Activity still
// shows (ShownRecordStatuses). The activity feed links an event to its
// record only then: a cleared history row or a proposal has no detail to open.
func (db *DB) ShownDownloadRecordIDs(
	ctx context.Context,
	ids []uint32,
) (map[uint32]struct{}, error) {
	out := make(map[uint32]struct{}, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	found, err := db.client.DownloadRecord.Query().
		Where(
			downloadrecord.IDIn(ids...),
			downloadrecord.StatusIn(ShownRecordStatuses...),
		).
		IDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("shown download records: %w", err)
	}
	for _, id := range found {
		out[id] = struct{}{}
	}
	return out, nil
}

// DeleteDownloadRecord deletes one record by ID. Returns ent.NotFound when
// absent (handler maps to 404).
func (db *DB) DeleteDownloadRecord(ctx context.Context, id uint32) error {
	return db.client.DownloadRecord.DeleteOneID(id).Exec(ctx)
}

// DeleteAllCompletedDownloadRecords removes every completed record (the
// "Clear completed" action) but those whose torrent_hash is in keepHashes.
// Returns the number of rows deleted.
func (db *DB) DeleteAllCompletedDownloadRecords(
	ctx context.Context,
	keepHashes []string,
) (int, error) {
	return db.client.DownloadRecord.Delete().Where(sparingHashes(keepHashes,
		downloadrecord.StatusEQ(downloadrecord.StatusCompleted),
	)...).Exec(ctx)
}

// RevertMovieToWantedIfNoFile flips a movie back to "wanted" only when it has
// no MediaFile, so cancelling a download doesn't clobber an available movie
// that already has a prior file from an upgrade grab.
func (db *DB) RevertMovieToWantedIfNoFile(
	ctx context.Context,
	movieID uint32,
) error {
	has, err := db.client.MediaFile.Query().
		Where(mediafile.HasMovieWith(movie.ID(movieID))).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check media files: %w", err)
	}
	if has {
		return nil
	}
	return db.client.Movie.UpdateOneID(movieID).
		SetStatus(movie.StatusWanted).
		Exec(ctx)
}

// inFlightEpisodeStatuses are the states an episode only holds while a grab is
// still running, so a sweep that finds one with no live record behind it knows
// the row is stranded. "paused" is in flight too: the torrent still exists.
var inFlightEpisodeStatuses = []episode.Status{
	episode.StatusDownloading,
	episode.StatusImporting,
	episode.StatusPaused,
}

// inFlightRecordStatuses are the states a download record holds while its
// torrent is still ours to finish. "held" is one of them: a held record awaits
// a decision, so reverting its episodes would let the missing-search grab a
// duplicate release while it pends.
var inFlightRecordStatuses = []downloadrecord.Status{
	downloadrecord.StatusDownloading,
	downloadrecord.StatusImporting,
	downloadrecord.StatusHeld,
}

// recordEpisodes selects every episode a record's download is for.
func recordEpisodes(recordID uint32) predicate.Episode {
	return episode.HasDownloadRecordsWith(downloadrecord.ID(recordID))
}

// settleRecordEpisodes walks a record that just failed back off the episodes
// it left in flight: RevertOrphanedDownloadingEpisodes' rule, scoped to the
// record's own set and run in the failing transaction. A pack marks every
// episode it is for, so walking back only the anchor would leave the rest
// reading "importing" until the next monitor tick's sweep found them stranded.
// An episode another in-flight record still covers is left to it; the caller
// has already moved this record out of flight.
func settleRecordEpisodes(ctx context.Context, tx *ent.Tx, recordID uint32) error {
	stranded := episode.And(
		recordEpisodes(recordID),
		episode.StatusIn(inFlightEpisodeStatuses...),
		episode.Not(episode.HasDownloadRecordsWith(
			downloadrecord.StatusIn(inFlightRecordStatuses...),
		)),
	)
	if _, err := tx.Episode.Update().
		Where(stranded, episode.Not(episode.HasMediaFiles())).
		SetStatus(episode.StatusWanted).
		Save(ctx); err != nil {
		return fmt.Errorf("revert record episodes to wanted: %w", err)
	}
	if _, err := tx.Episode.Update().
		Where(stranded, episode.HasMediaFiles()).
		SetStatus(episode.StatusAvailable).
		Save(ctx); err != nil {
		return fmt.Errorf("revert record episodes to available: %w", err)
	}
	return nil
}

// RevertOrphanedDownloadingEpisodes reconciles episodes stuck in flight with
// no in-flight download record covering them — a cancelled or lost record
// leaves every episode it marked behind.
//
// Covering means linked through the record's episodes, never sharing a season
// with it: a pack spanning seasons anchors in only one, so a season rule
// would declare every other season stranded 20 seconds after the grab, and the
// missing-search would grab duplicates of episodes already downloading (Narcos
// INTEGRALE, S02 and S03).
//
// Two arms, not one query: an episode with no media file has never had
// anything, so it goes back to "wanted"; an episode that already has a file
// (an upgrade target left stranded downloading) goes back to "available"
// instead — "wanted" would claim we don't have it. Returns rows reverted
// across both.
func (db *DB) RevertOrphanedDownloadingEpisodes(
	ctx context.Context,
) (int, error) {
	stranded := episode.And(
		episode.StatusIn(inFlightEpisodeStatuses...),
		episode.Not(episode.HasDownloadRecordsWith(
			downloadrecord.StatusIn(inFlightRecordStatuses...),
		)),
	)
	toWanted, err := db.client.Episode.Update().
		Where(
			stranded,
			episode.Not(episode.HasMediaFiles()),
		).
		SetStatus(episode.StatusWanted).
		Save(ctx)
	if err != nil {
		return toWanted, err
	}
	toAvailable, err := db.client.Episode.Update().
		Where(
			stranded,
			episode.HasMediaFiles(),
		).
		SetStatus(episode.StatusAvailable).
		Save(ctx)
	return toWanted + toAvailable, err
}

// RevertOrphanedDownloadingAlbumsAndBooks is RevertOrphanedDownloadingEpisodes
// for albums and book slots: one left "downloading" with no in-flight record
// covering it — the client refused the torrent, it was removed from the
// Torrents page, or its record was purged — goes back to "wanted", or to
// "available" when it holds every track. Only the import-failure path moved
// them before, so every other way a record ended stranded the title, and the
// searches skip anything downloading. Returns rows reverted.
func (db *DB) RevertOrphanedDownloadingAlbumsAndBooks(
	ctx context.Context,
) (int, error) {
	inFlight := downloadrecord.StatusIn(inFlightRecordStatuses...)
	strandedAlbum := album.And(
		album.StatusEQ(album.StatusDownloading),
		album.Not(album.HasDownloadRecordsWith(inFlight)),
		album.Not(album.HasPackRecordsWith(inFlight)),
	)
	total := 0
	for _, arm := range []struct {
		complete bool
		to       album.Status
	}{{false, album.StatusWanted}, {true, album.StatusAvailable}} {
		held := completeAlbum()
		if !arm.complete {
			held = album.Not(held)
		}
		n, err := db.client.Album.Update().
			Where(strandedAlbum, held).
			SetStatus(arm.to).
			Save(ctx)
		total += n
		if err != nil {
			return total, fmt.Errorf("revert stranded albums: %w", err)
		}
	}

	for _, kind := range []downloadrecord.BookKind{
		downloadrecord.BookKindEbook, downloadrecord.BookKindAudiobook,
	} {
		noRecord := book.Not(book.HasDownloadRecordsWith(
			inFlight, downloadrecord.BookKindEQ(kind),
		))
		held := book.HasMediaFilesWith(
			mediafile.BookKindEQ(mediafile.BookKind(kind)),
		)
		for _, files := range []bool{false, true} {
			pred := held
			if !files {
				pred = book.Not(held)
			}
			u := db.client.Book.Update().Where(noRecord, pred)
			switch {
			case kind == downloadrecord.BookKindEbook && files:
				u = u.Where(book.EbookStatusEQ(book.EbookStatusDownloading)).
					SetEbookStatus(book.EbookStatusAvailable)
			case kind == downloadrecord.BookKindEbook:
				u = u.Where(book.EbookStatusEQ(book.EbookStatusDownloading)).
					SetEbookStatus(book.EbookStatusWanted)
			case files:
				u = u.Where(book.AudiobookStatusEQ(book.AudiobookStatusDownloading)).
					SetAudiobookStatus(book.AudiobookStatusAvailable)
			default:
				u = u.Where(book.AudiobookStatusEQ(book.AudiobookStatusDownloading)).
					SetAudiobookStatus(book.AudiobookStatusWanted)
			}
			n, err := u.Save(ctx)
			total += n
			if err != nil {
				return total, fmt.Errorf("revert stranded %s slots: %w", kind, err)
			}
		}
	}
	return total, nil
}

// MarkEpisodeDownloading flips one episode to "downloading" after a grab, and
// only from "wanted": an episode that already has a file is a replace target,
// and claiming it is downloading would strand it as "wanted" — file and all —
// if the grab is later reverted. Reports whether the row moved.
func (db *DB) MarkEpisodeDownloading(
	ctx context.Context,
	id uint32,
) (bool, error) {
	n, err := db.client.Episode.Update().
		Where(
			episode.ID(id),
			episode.StatusEQ(episode.StatusWanted),
		).
		SetStatus(episode.StatusDownloading).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("mark episode %d downloading: %w", id, err)
	}
	return n > 0, nil
}

// MarkRecordEpisodesImporting moves a record's in-flight episodes to
// "importing" as the download hands off to the importer. Record-scoped like
// SyncDownloadStateForRecord, and it only moves rows already in
// downloading/paused — an episode that is available (a replace target) or
// wanted (never part of this grab) is none of this record's business.
//
// Not the anchor's season: for a pack spanning seasons that is one of
// several, and the other seasons' episodes would be left "downloading" with
// the import already past them, where no later path moves a row the importer
// is no longer working on.
func (db *DB) MarkRecordEpisodesImporting(
	ctx context.Context,
	recordID uint32,
) error {
	if _, err := db.client.Episode.Update().
		Where(
			recordEpisodes(recordID),
			episode.StatusIn(
				episode.StatusDownloading,
				episode.StatusPaused,
			),
		).
		SetStatus(episode.StatusImporting).
		Save(ctx); err != nil {
		return fmt.Errorf("mark record episodes importing: %w", err)
	}
	return nil
}

// SyncDownloadStateForRecord reflects a download's live torrent state onto its
// episode badges: when paused, the episodes this record is for that are still
// "downloading" flip to "paused"; when active again they flip back. A no-op
// for movie records, which cover no episode.
//
// Scoped to the record's own episodes, not to the season its anchor sits in.
// Season scope crosses records: pausing a duplicate grab of season 2 would also
// pause the eight episodes of that season a whole-series pack is fetching, and
// the resume would never reach them — the pack's own anchor is in season 1, so
// its every-tick resume sweeps a season those episodes are not in. They would
// sit "paused" against a torrent that was never paused, with the orphan sweep
// rightly declining to touch episodes an in-flight record still claims
// (Narcos INTEGRALE). Record scope makes the resume reach them on the next
// tick.
func (db *DB) SyncDownloadStateForRecord(
	ctx context.Context,
	recordID uint32,
	paused bool,
) error {
	from, to := episode.StatusDownloading, episode.StatusPaused
	if !paused {
		from, to = episode.StatusPaused, episode.StatusDownloading
	}
	if _, err := db.client.Episode.Update().
		Where(
			recordEpisodes(recordID),
			episode.StatusEQ(from),
		).
		SetStatus(to).
		Save(ctx); err != nil {
		return fmt.Errorf("sync download state: %w", err)
	}
	return nil
}

// ListKeptUploadPaths returns the staging folders of uploaded records the
// importer or a person may still need: importing, held, or failed and not
// rejected (a retry reads them).
func (db *DB) ListKeptUploadPaths(ctx context.Context) ([]string, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.Uploaded(true),
			downloadrecord.SavePathNEQ(""),
			downloadrecord.Or(
				downloadrecord.StatusIn(
					downloadrecord.StatusImporting,
					downloadrecord.StatusHeld,
				),
				downloadrecord.And(
					downloadrecord.StatusEQ(downloadrecord.StatusFailed),
					downloadrecord.Rejected(false),
				),
			),
		).
		Select(downloadrecord.FieldSavePath).
		Strings(ctx)
}
