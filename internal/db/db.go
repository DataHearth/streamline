// Package db exposes the database surface as the Store interface. Callers
// hold db.Store (or *DB directly) and invoke methods, rather than calling
// top-level functions that take an *ent.Client.
package db

import (
	"context"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/importscan"
	"github.com/datahearth/streamline/ent/importscanfile"
	"github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/request"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/ent/user"
)

// Tx is a transaction-bound Store. Caller invokes regular Store methods, then
// Commit or Rollback. Either method is terminal — calling both, or calling
// the same method twice, is a programmer error.
type Tx interface {
	Store
	Commit() error
	Rollback() error
}

// Store is the full database surface. Implementations: *DB (prod) and
// generated mocks (tests).
type Store interface {
	// Tx starts a transaction and returns a Tx-bound Store. Caller owns
	// Commit/Rollback.
	Tx(ctx context.Context) (Tx, error)

	// users
	FindUserByEmail(ctx context.Context, email string) (*ent.User, error)
	FindUserByID(ctx context.Context, id uint32) (*ent.User, error)
	CountUsers(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, p CreateUserParams) (*ent.User, error)
	UpdateUserPassword(ctx context.Context, id uint32, hash string) error
	UpdateUser(
		ctx context.Context,
		id uint32,
		p UpdateUserParams,
	) (*ent.User, error)
	ListUsers(
		ctx context.Context,
		p ListUsersParams,
	) ([]*ent.User, int, error)
	CountUsersByRole(ctx context.Context, role user.Role) (int, error)
	DeleteUser(ctx context.Context, id uint32) error

	// sessions
	CreateSession(ctx context.Context, p CreateSessionParams) (*ent.Session, error)
	FindSessionByJTI(ctx context.Context, jti string) (*ent.Session, error)
	TouchSession(ctx context.Context, jti string, when time.Time) error
	RevokeSessionByJTI(ctx context.Context, jti string, when time.Time) error
	RevokeUserSessionByID(
		ctx context.Context,
		userID, sessionID uint32,
		when time.Time,
	) (int, error)
	UserSessionExists(ctx context.Context, userID, sessionID uint32) (bool, error)
	RevokeAllUserSessions(ctx context.Context, userID uint32, when time.Time) error
	RevokeOtherUserSessions(
		ctx context.Context,
		userID uint32,
		keepJTI string,
		when time.Time,
	) error
	ListUserSessions(ctx context.Context, userID uint32) ([]*ent.Session, error)
	PurgeExpiredSessions(ctx context.Context, before time.Time) (int, error)
	TruncateSessions(ctx context.Context) error

	// api keys
	CreateAPIKey(ctx context.Context, p CreateAPIKeyParams) (*ent.ApiKey, error)
	FindAPIKeyByHash(ctx context.Context, hash string) (*ent.ApiKey, error)
	ListAPIKeysByUser(ctx context.Context, userID uint32) ([]*ent.ApiKey, error)
	DeleteAPIKeyByID(ctx context.Context, userID, keyID uint32) (int, error)

	// torrent sessions (builtin BitTorrent engine persistence)
	CreateTorrentSession(
		ctx context.Context,
		p CreateTorrentSessionParams,
	) (*ent.TorrentSession, error)
	ListTorrentSessions(ctx context.Context) ([]*ent.TorrentSession, error)
	DeleteTorrentSessionByHash(ctx context.Context, infoHash string) error
	SetTorrentSessionPaused(
		ctx context.Context,
		infoHash string,
		paused bool,
	) error
	SetTorrentSessionName(ctx context.Context, infoHash, name string) error
	SetTorrentSessionCompleted(
		ctx context.Context,
		infoHash string,
		at time.Time,
	) error
	SetTorrentSessionSeedStopped(ctx context.Context, infoHash string) error

	// invites
	CreateInvite(ctx context.Context, p CreateInviteParams) (*ent.Invite, error)
	FindInviteByTokenHash(ctx context.Context, hash string) (*ent.Invite, error)
	FindUnusedInviteForEmail(
		ctx context.Context,
		email string,
		now time.Time,
	) (*ent.Invite, error)
	ListInvites(ctx context.Context) ([]*ent.Invite, error)
	MarkInviteUsed(
		ctx context.Context,
		id uint32,
		when time.Time,
	) (*ent.Invite, error)
	MarkInviteUsedWithUser(
		ctx context.Context,
		id, userID uint32,
		when time.Time,
	) (*ent.Invite, error)
	RevokeInvite(ctx context.Context, id uint32, now time.Time) error

	// oidc identities
	FindOIDCIdentity(
		ctx context.Context,
		provider, subject string,
	) (*ent.OIDCIdentity, error)
	CreateOIDCIdentity(
		ctx context.Context,
		p CreateOIDCIdentityParams,
	) (*ent.OIDCIdentity, error)

	// movies
	CreateMovie(ctx context.Context, p CreateMovieParams) (*ent.Movie, error)
	FindMovieByID(ctx context.Context, id uint32) (*ent.Movie, error)
	FindMovieByTMDBID(ctx context.Context, tmdbID uint32) (*ent.Movie, error)
	FindMoviesByTMDBIDs(ctx context.Context, tmdbIDs []uint32) ([]*ent.Movie, error)
	CountMovies(ctx context.Context) (int, error)
	CountMoviesByStatus(ctx context.Context, status movie.Status) (int, error)
	MovieCreateTimesSince(ctx context.Context, since time.Time) ([]time.Time, error)
	ListMovies(ctx context.Context, offset, limit uint32) ([]*ent.Movie, error)
	FilterMovies(
		ctx context.Context,
		p FilterMoviesParams,
	) ([]*ent.Movie, int, error)
	ListEligibleMoviesForSync(
		ctx context.Context,
		maxGrabFailures uint8,
		notSearchedSince time.Time,
	) ([]*ent.Movie, error)
	ListWantedMovies(ctx context.Context) ([]*ent.Movie, error)
	ListMoviesStaleSince(
		ctx context.Context,
		cutoff time.Time,
	) ([]*ent.Movie, error)
	DeleteMovie(ctx context.Context, id uint32) error
	UpdateMovie(
		ctx context.Context,
		id uint32,
		p UpdateMovieParams,
	) (*ent.Movie, error)
	UpdateMovieMetadata(
		ctx context.Context,
		id uint32,
		p UpdateMovieMetadataParams,
	) error
	UpdateMovieStatus(ctx context.Context, id uint32, status movie.Status) error
	SetMovieLastSearchAt(ctx context.Context, id uint32, when time.Time) error
	SetMovieDigitalReleaseDate(
		ctx context.Context,
		id uint32,
		date *time.Time,
	) error
	IncrementMovieGrabFailures(ctx context.Context, id uint32) error
	ResetMovieGrabFailures(ctx context.Context, id uint32) error

	// movie events
	RecentActivity(
		ctx context.Context,
		f ActivityFilter,
	) (*ActivityResult, error)
	UpcomingReleases(
		ctx context.Context,
		from, to time.Time,
	) ([]*ent.Movie, error)
	ListUpcomingEpisodes(
		ctx context.Context,
		from, to time.Time,
	) ([]*ent.Episode, error)

	// download records — used by the download manager
	CreateDownloadRecord(
		ctx context.Context,
		p CreateDownloadRecordParams,
	) (*ent.DownloadRecord, error)
	// ListMoviesForAdoption returns all movies as adoption-match candidates.
	ListMoviesForAdoption(ctx context.Context) ([]*ent.Movie, error)
	// ListTvShowsForAdoption returns all shows with seasons → episodes → media
	// files eager-loaded, for episode adoption matching.
	ListTvShowsForAdoption(ctx context.Context) ([]*ent.TVShow, error)
	ListDownloadingRecords(ctx context.Context) ([]*ent.DownloadRecord, error)
	ListDownloadingRecordsWithMovie(
		ctx context.Context,
	) ([]*ent.DownloadRecord, error)
	UpdateDownloadRecordStatus(
		ctx context.Context,
		id uint32,
		status downloadrecord.Status,
	) error
	// MarkDownloadRecordReplaceExisting flags a record so the importer
	// overwrites already-present file(s) instead of skipping them.
	MarkDownloadRecordReplaceExisting(ctx context.Context, id uint32) error
	ListImportingDownloadRecords(ctx context.Context) ([]*ent.DownloadRecord, error)
	FindImportingDownloadRecordByID(
		ctx context.Context,
		id uint32,
	) (*ent.DownloadRecord, error)
	RecordImportSuccess(ctx context.Context, p RecordImportSuccessParams) error
	RecordEpisodeImportSuccess(
		ctx context.Context,
		p RecordEpisodeImportSuccessParams,
	) error
	RecordImportFailure(ctx context.Context, p RecordImportFailureParams) error
	SetDownloadRecordSavePath(ctx context.Context, id uint32, path string) error
	DeleteCompletedDownloadRecordsBefore(
		ctx context.Context,
		cutoff time.Time,
	) (int, error)
	DeleteFailedDownloadRecordsBefore(
		ctx context.Context,
		cutoff time.Time,
	) (int, error)
	ListActiveDownloadRecords(ctx context.Context) ([]*ent.DownloadRecord, error)
	FindActiveDownloadRecordByID(
		ctx context.Context,
		id uint32,
	) (*ent.DownloadRecord, error)
	ListDownloadHistory(
		ctx context.Context,
		limit int,
		cursor string,
	) (*DownloadHistoryResult, error)
	DeleteDownloadRecord(ctx context.Context, id uint32) error
	DeleteAllCompletedDownloadRecords(ctx context.Context) (int, error)
	RevertMovieToWantedIfNoFile(ctx context.Context, movieID uint32) error
	RevertOrphanedDownloadingEpisodes(ctx context.Context) (int, error)
	SyncSeasonDownloadStateForRecord(
		ctx context.Context,
		recordID uint32,
		paused bool,
	) error
	// AllDownloadRecordHashes returns the set of non-empty torrent hashes
	// across every record. The adoption pass uses it to skip already-tracked
	// torrents.
	AllDownloadRecordHashes(ctx context.Context) (map[string]struct{}, error)
	ListPendingDownloadRecords(ctx context.Context) ([]*ent.DownloadRecord, error)
	// DeleteStalePendingAdoptions prunes pending proposals for a client whose
	// torrent_hash is absent from its current managed torrents (liveHashes).
	DeleteStalePendingAdoptions(
		ctx context.Context,
		clientName string,
		liveHashes []string,
	) (int, error)
	FindPendingDownloadRecordByID(
		ctx context.Context,
		id uint32,
	) (*ent.DownloadRecord, error)
	// LatestImportedRecordForMovie returns the newest hash-carrying record for
	// a movie (file-delete uses it to remove the source torrent). NotFound when
	// none. LatestImportedRecordForEpisode is the episode twin.
	LatestImportedRecordForMovie(
		ctx context.Context,
		movieID uint32,
	) (*ent.DownloadRecord, error)
	LatestImportedRecordForEpisode(
		ctx context.Context,
		episodeID uint32,
	) (*ent.DownloadRecord, error)

	// media files — used by the library importer
	CreateMediaFile(
		ctx context.Context,
		p CreateMediaFileParams,
	) (*ent.MediaFile, error)
	// FindMediaFileByID returns one MediaFile by ID, or ent NotFound.
	FindMediaFileByID(ctx context.Context, id uint32) (*ent.MediaFile, error)
	// FindMediaFileByEpisodeID returns the MediaFile owned by an episode (at
	// most one), or ent NotFound when it has none.
	FindMediaFileByEpisodeID(
		ctx context.Context,
		episodeID uint32,
	) (*ent.MediaFile, error)
	// MovieHasMediaFile reports whether the movie identified by tmdbID has at
	// least one MediaFile row. Returns (false, nil) when the movie row itself
	// is absent.
	MovieHasMediaFile(ctx context.Context, tmdbID uint32) (bool, error)
	// ListAllMediaFilesWithMovie returns every MediaFile row joined to its
	// owning movie. Used by drift_check; loads edges eagerly via WithMovie().
	ListAllMediaFilesWithMovie(ctx context.Context) ([]*ent.MediaFile, error)
	// ListMediaFilesByMovieID returns every MediaFile attached to the given
	// movie. Empty slice (no error) when the movie has no files.
	ListMediaFilesByMovieID(
		ctx context.Context,
		movieID uint32,
	) ([]*ent.MediaFile, error)
	// BumpMediaFileLastSeen sets last_seen_at = now for the given row.
	BumpMediaFileLastSeen(ctx context.Context, id uint32) error
	// UpdateMediaFilePath rewrites a MediaFile's path (used by rename).
	UpdateMediaFilePath(ctx context.Context, id uint32, path string) error
	// DeleteMediaFileAndRevertMovie removes the MediaFile row and sets the
	// owning movie's status back to "wanted" in a single transaction.
	DeleteMediaFileAndRevertMovie(
		ctx context.Context,
		mediaFileID, movieID uint32,
	) error
	// DeleteMediaFileAndRevertEpisode is the episode twin of
	// DeleteMediaFileAndRevertMovie.
	DeleteMediaFileAndRevertEpisode(
		ctx context.Context,
		mediaFileID, episodeID uint32,
	) error

	// bulk-import scans
	CreateImportScan(
		ctx context.Context,
		p CreateImportScanParams,
	) (*ent.ImportScan, error)
	FindImportScan(ctx context.Context, id uint32) (*ent.ImportScan, error)
	// FindOpenImportScanForSource returns the oldest awaiting_review scan for
	// sourcePath, or ent NotFound.
	FindOpenImportScanForSource(
		ctx context.Context,
		sourcePath string,
	) (*ent.ImportScan, error)
	ListImportScans(
		ctx context.Context,
		offset, limit uint32,
	) ([]*ent.ImportScan, uint32, error)
	UpdateImportScanStatus(
		ctx context.Context,
		id uint32,
		status importscan.Status,
		opts UpdateScanStatusOpts,
	) error
	IncrementImportScanProgress(
		ctx context.Context,
		id uint32,
		processedDelta uint32,
	) error
	CountActiveImportScans(ctx context.Context) (uint32, error)
	DeleteImportScan(ctx context.Context, id uint32) error
	AbortInflightImportScans(ctx context.Context, reason string) (uint32, error)

	// bulk-import scan files
	BulkCreateImportScanFiles(
		ctx context.Context,
		scanID uint32,
		files []CreateImportScanFileParams,
	) error
	FilterImportScanFiles(
		ctx context.Context,
		p FilterImportScanFilesParams,
	) ([]*ent.ImportScanFile, uint32, error)
	// FindImportScanFile returns the file with fileID scoped to scanID, or an
	// ent NotFound error when no such row exists under that scan.
	FindImportScanFile(
		ctx context.Context,
		scanID, fileID uint32,
	) (*ent.ImportScanFile, error)
	UpdateImportScanFileDecision(
		ctx context.Context,
		id uint32,
		decision importscanfile.Decision,
		tmdbID *uint32,
	) error
	UpdateImportScanFileOutcome(
		ctx context.Context,
		id uint32,
		outcome importscanfile.Outcome,
		opts UpdateScanFileOutcomeOpts,
	) error
	ListImportScanFilesForCommit(
		ctx context.Context,
		scanID uint32,
	) ([]*ent.ImportScanFile, error)
	// ListPendingImportScanFilePaths returns the source_path of every
	// ImportScanFile attached to a scan whose status is still
	// "awaiting_review". Used by the orphan_scan dedup gate.
	ListPendingImportScanFilePaths(ctx context.Context) ([]string, error)

	// series import scans (import_scan_show children)
	ListPendingImportScanShowFolders(ctx context.Context) ([]string, error)
	BulkCreateImportScanShows(
		ctx context.Context,
		scanID uint32,
		shows []CreateImportScanShowParams,
	) error
	ListImportScanShows(
		ctx context.Context,
		p ListImportScanShowsParams,
	) ([]*ent.ImportScanShow, uint32, error)
	FindImportScanShow(
		ctx context.Context,
		scanID, showID uint32,
	) (*ent.ImportScanShow, error)
	UpdateImportScanShowDecision(
		ctx context.Context,
		id uint32,
		decision importscanshow.Decision,
		tvdbID *uint32,
	) error
	ListImportScanShowsForCommit(
		ctx context.Context,
		scanID uint32,
	) ([]*ent.ImportScanShow, error)
	UpdateImportScanShowOutcome(
		ctx context.Context,
		id uint32,
		outcome importscanshow.Outcome,
		opts UpdateScanShowOutcomeOpts,
	) error
	ListAllEpisodeMediaFilePaths(ctx context.Context) ([]string, error)

	// tv shows / seasons / episodes
	CreateTVShow(ctx context.Context, p CreateTVShowParams) (*ent.TVShow, error)
	FindTVShowByID(ctx context.Context, id uint32) (*ent.TVShow, error)
	FindTVShowByTVDBID(ctx context.Context, tvdbID uint32) (*ent.TVShow, error)
	ListTVShows(ctx context.Context, offset, limit uint32) ([]*ent.TVShow, error)
	CountTVShows(ctx context.Context) (int, error)
	CountTVShowsByStatus(
		ctx context.Context,
		status tvshow.SeriesStatus,
	) (int, error)
	UpdateTVShow(
		ctx context.Context,
		id uint32,
		p UpdateTVShowParams,
	) (*ent.TVShow, error)
	UpdateTVShowMetadata(
		ctx context.Context,
		id uint32,
		p UpdateTVShowMetadataParams,
	) error
	SetTVShowRefreshedAt(ctx context.Context, id uint32, when time.Time) error
	ReconcileEpisodes(
		ctx context.Context,
		showID uint32,
		seasons []SeasonSeed,
	) ([]string, error)
	DeleteTVShow(ctx context.Context, id uint32) error
	SetSeasonMonitored(ctx context.Context, id uint32, monitored bool) error
	SetEpisodeMonitored(ctx context.Context, id uint32, monitored bool) error
	CascadeShowMonitored(ctx context.Context, showID uint32, monitored bool) error
	CascadeSeasonMonitored(
		ctx context.Context,
		seasonID uint32,
		monitored bool,
	) error
	SetEpisodeStatus(ctx context.Context, id uint32, status episode.Status) error
	SetEpisodeLastSearchAt(ctx context.Context, id uint32, when time.Time) error
	IncrementEpisodeGrabFailures(ctx context.Context, id uint32) error
	ResetEpisodeGrabFailures(ctx context.Context, id uint32) error
	ListWantedEpisodes(ctx context.Context) ([]*ent.TVShow, error)

	// requests
	CreateRequest(ctx context.Context, p CreateRequestParams) (*ent.Request, error)
	FindActiveRequest(
		ctx context.Context,
		mediaType string,
		mediaID uint32,
	) (*ent.Request, error)
	ListRequests(
		ctx context.Context,
		p ListRequestsParams,
	) ([]*ent.Request, int, error)
	GetRequest(ctx context.Context, id uint32) (*ent.Request, error)
	ApproveRequest(ctx context.Context, id, adminID uint32) error
	DenyRequest(ctx context.Context, id, adminID uint32, reason string) error
	ReopenRequest(ctx context.Context, id uint32) error
	MarkRequestsAvailable(
		ctx context.Context,
		mediaType string,
		mediaID uint32,
	) error
	CountRequestsByStatus(
		ctx context.Context,
		status request.Status,
	) (int, error)
}

// DB is the ent-backed implementation of Store.
type DB struct {
	client *ent.Client
}

// New constructs a *DB from an open ent client.
func New(c *ent.Client) *DB { return &DB{client: c} }

// Tx starts a transaction and returns a Tx-bound Store. Caller owns
// Commit/Rollback.
func (db *DB) Tx(ctx context.Context) (Tx, error) {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	return &dbTx{DB: &DB{client: tx.Client()}, ent: tx}, nil
}

// dbTx is the transactional implementation of the Tx interface.
type dbTx struct {
	*DB
	ent *ent.Tx
}

func (t *dbTx) Commit() error   { return t.ent.Commit() }
func (t *dbTx) Rollback() error { return t.ent.Rollback() }

var (
	_ Store = (*DB)(nil)
	_ Tx    = (*dbTx)(nil)
)
