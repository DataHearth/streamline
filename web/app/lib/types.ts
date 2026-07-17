export type MovieStatus = "wanted" | "downloading" | "available";

export type SeriesStatus = "continuing" | "ended" | "upcoming";
export type SeriesType = "standard" | "anime" | "daily";
export type EpisodeStatus =
	| "wanted"
	| "downloading"
	| "paused"
	| "available"
	| "unaired"
	| "skipped";

// Monitoring presets accepted by POST /series and PATCH /series/{id}.
export type MonitoringPreset =
	| "all"
	| "future"
	| "missing"
	| "existing"
	| "pilot"
	| "none";

export type Episode = {
	id: number;
	number: number;
	absolute_number?: number;
	title?: string;
	air_date?: string | null;
	status: EpisodeStatus;
	monitored: boolean;
	quality?: string;
	size?: number | null;
};

export type Season = {
	id: number;
	number: number;
	name?: string;
	monitored: boolean;
	available?: number;
	missing?: number;
	unaired?: number;
	total?: number;
	episodes?: Episode[];
};

export type TVShow = {
	id: number;
	title: string;
	original_title?: string;
	year: number;
	overview?: string;
	series_status: SeriesStatus;
	type: SeriesType;
	monitored: boolean;
	tvdb_id: number;
	network?: string;
	creator?: string;
	runtime?: number;
	rating?: number | null;
	genres?: string[];
	quality_profile?: string;
	have_episodes?: number;
	total_episodes?: number;
	wanted_episodes?: number;
	// Only populated by GET /series/{id}; absent in list responses.
	seasons?: Season[];
	cast?: CastMember[];
};

export type PaginatedTVShows = {
	items: TVShow[];
	total: number;
	page: number;
	limit: number;
};

export type TVShowCounts = {
	total: number;
	continuing: number;
	ended: number;
	wanted_episodes: number;
};

export type SeriesLookupResult = {
	tvdb_id: number;
	title: string;
	year: number;
	network?: string;
	overview?: string;
	already_added?: boolean;
	poster_url?: string;
};

// GET /series/lookup wraps results in an items envelope.
export type SeriesLookupResultList = {
	items: SeriesLookupResult[];
};

export type AddSeriesRequest = {
	tvdb_id: number;
	quality_profile?: string;
	preset?: MonitoringPreset;
};

export type PatchSeriesRequest = {
	monitored?: boolean;
	quality_profile?: string;
	preset?: MonitoringPreset;
};

export type CastMember = {
	tmdb_id?: number;
	name: string;
	character?: string;
	profile_url?: string;
	// Link to the person's page on the source provider (TMDB or TVDB).
	person_url?: string;
};

export type Movie = {
	id: number;
	title: string;
	original_title: string;
	year: number;
	status: MovieStatus;
	tmdb_id: number;
	overview?: string;
	runtime?: number;
	monitored?: boolean;
	quality_profile?: string;
	media_files?: MediaFile[];
	cast?: CastMember[];
	genres?: string[];
	rating?: number;
};

export type MediaFile = {
	id: number;
	path: string;
	size: number;
	quality?: string;
	format?: string;
	release_group?: string;
	parsed_source?: string;
	parsed_resolution?: string;
	parsed_codec?: string;
};

export type SearchResult = {
	title: string;
	info_url?: string;
	download_url: string;
	size: number;
	seeders: number;
	leechers?: number;
	release_group?: string;
	resolution?: string;
	source?: string;
	codec?: string;
	indexer?: string;
	published_at?: string;
};

export type PlayOnStatus = "resolved" | "fallback" | "unavailable";

export type PlayOnLink = {
	server_id: number;
	name: string;
	server_type: MediaServerType;
	url?: string;
	fallback: boolean;
	status: PlayOnStatus;
};

export type PlayOnLinkList = {
	items: PlayOnLink[];
};

export type RenameOperation = {
	media_file_id: number;
	from: string;
	to: string;
};

export type RenamePlan = {
	movie_id: number;
	operations: RenameOperation[];
};

export type SeriesRenamePlan = {
	series_id: number;
	operations: RenameOperation[];
};

export type PaginatedMovies = {
	items: Movie[];
	total: number;
	page: number;
	limit: number;
};

export type TMDBMovieResult = {
	tmdb_id: number;
	title: string;
	original_title: string;
	year: number;
	overview?: string;
	poster_url?: string;
};

export type MovieRecommendations = {
	items: TMDBMovieResult[];
};

export type AddMovieRequest = {
	tmdb_id: number;
	quality_profile?: string;
};

export type QualityProfile = {
	name: string;
};

// Requests. UI label "Rejected" maps to status "denied".
export type RequestStatus = "pending" | "approved" | "denied" | "available";

export type RequestUser = {
	id: number;
	email: string;
	display_name?: string;
};

export type MediaRequest = {
	id: number;
	media_type: "movie" | "tvshow";
	media_id: number;
	title: string;
	status: RequestStatus;
	reason?: string;
	requester: RequestUser;
	approved_by?: RequestUser;
	created_at: string;
	updated_at: string;
};

export type PaginatedRequests = {
	items: MediaRequest[];
	total: number;
	page: number;
	limit: number;
};

export type RequestCounts = {
	pending: number;
	approved: number;
	denied: number;
	available: number;
};

// Cover/synopsis fetched on demand so reviewers can judge a request.
export type RequestMediaDetails = {
	poster_url?: string;
	overview: string;
	year?: number;
	rating?: number;
	runtime?: number;
	genres?: string[];
};

export type CreateRequestBody = {
	media_type: "movie" | "tvshow";
	media_id: number;
	title: string;
};

export type MovieCounts = {
	total: number;
	wanted: number;
	downloading: number;
	available: number;
	// Cumulative library size per day over the last 30 days, oldest first;
	// the final element equals `total`. All zeros when the library is empty.
	trend: number[];
};

export type UpcomingMovie = {
	id: number;
	title: string;
	year: number;
	tmdb_id: number;
	digital_release_date: string;
};

export type UpcomingEpisode = {
	series_id: number;
	series_title: string;
	season: number;
	episode: number;
	title?: string;
	air_date: string;
	monitored?: boolean;
};

export type UpcomingList = {
	movies: UpcomingMovie[];
	episodes: UpcomingEpisode[];
};

export type ActivityType =
	| "grabbed"
	| "download_completed"
	| "download_failed"
	| "imported"
	| "import_failed"
	| "drift_detected"
	| "drift_confirmed";

export type ActivityEvent = {
	id: number;
	type: ActivityType;
	created_at: string;
	payload?: Record<string, unknown>;
	movie: Movie;
};

export type ActivityList = {
	events: ActivityEvent[];
	next_cursor: string | null;
};

// QueueItem is the shape the cinematic dashboard expects from a future
// /activity/queue endpoint. Until the backend lands the dashboard treats
// an empty list as "no active downloads".
export type QueueItem = {
	id: number;
	movie_id: number;
	title: string;
	release?: string;
	status: "downloading" | "grabbing";
	progress: number;
	speed?: string;
	eta?: string;
	size?: string;
	indexer?: string;
};

// Live download queue (GET /activity/queue) — DownloadRecords still in
// flight, enriched with client telemetry. Distinct from the legacy
// QueueItem the dashboard stubs.
export type EpisodeRef = {
	show_title: string;
	season: number;
	episode: number;
};

export type QueueEntry = {
	id: number;
	status: "downloading" | "importing" | "paused" | "error";
	title: string;
	quality?: string;
	release_group?: string;
	movie: Movie;
	episode?: EpisodeRef;
	indexer?: string;
	download_client?: string;
	size: number;
	progress: number;
	download_speed?: number;
	eta?: number;
	failure_reason?: string;
	created_at: string;
};
export type DownloadQueue = { items: QueueEntry[]; refreshed_at: string };

export type PendingMedia = {
	type: "movie" | "episode";
	id: number;
	title: string;
	year?: number;
	season?: number;
	episode?: number;
};
export type PendingItem = {
	id: number;
	title: string;
	quality: string;
	reason: string;
	has_file: boolean;
	media?: PendingMedia;
};
export type PendingList = { items: PendingItem[] };

export type HistoryEntry = {
	id: number;
	status: "completed" | "failed";
	title: string;
	quality?: string;
	release_group?: string;
	movie: Movie;
	episode?: EpisodeRef;
	indexer?: string;
	download_client?: string;
	size: number;
	failure_reason?: string;
	imported_at?: string | null;
	created_at: string;
	updated_at: string;
};
export type DownloadHistory = {
	items: HistoryEntry[];
	next_cursor: string | null;
};

export type UserRole = "admin" | "member" | "request_only";

export type AuthMethod = "local" | "oidc" | "both";

export type User = {
	id: number;
	email: string;
	role: UserRole;
	auth_method: AuthMethod;
	display_name?: string;
	created_at: string;
	failed_login_count?: number;
	locked_until?: string | null;
};

export type ApiKey = {
	id: number;
	name: string;
	created_at: string;
	last_used_at: string | null;
};

export type Session = {
	id: number;
	ip?: string;
	user_agent?: string;
	created_at: string;
	last_seen_at: string | null;
	expires_at: string;
	is_current: boolean;
};

export type UserDetail = {
	user: User;
	api_keys: ApiKey[];
	sessions: Session[];
};

export type UserList = {
	items: User[];
	total: number;
};

export type Invite = {
	id: number;
	email?: string;
	role: UserRole;
	expires_at: string;
	used_at: string | null;
	created_at: string;
};

export type InviteCreated = Invite & {
	raw_token: string;
	url: string;
};

export type AuthConfig = {
	registration_mode: "disabled" | "open" | "invite";
	session_ttl: string;
	oidc_default_role: UserRole;
};

export type OIDCProvider = {
	name: string;
	issuer: string;
	client_id: string;
	client_secret_set: boolean;
};

export type OIDCProviderList = {
	providers: OIDCProvider[];
	restart_required: boolean;
};

export type Resolution = "720p" | "1080p" | "2160p";

export type QualityProfileFull = {
	name: string;
	preferred_resolution: Resolution;
	min_resolution: Resolution;
	upgrade_allowed: boolean;
};

// "torznab" covers plain Torznab endpoints and Jackett (its /indexers/all
// aggregate feed is standard Torznab). "prowlarr" uses Prowlarr's native JSON
// search API, the only way to query all of Prowlarr's indexers at once.
export type IndexerProtocol = "torznab" | "prowlarr";

export type Indexer = {
	name: string;
	host: string;
	port: number;
	path?: string;
	use_ssl?: boolean;
	api_key_set: boolean;
	protocol: IndexerProtocol;
	priority?: number;
	enabled: boolean;
};

export type DownloadClientType =
	| "qbittorrent"
	| "transmission"
	| "deluge"
	| "builtin";
export type DownloadClientAuth = "password" | "api_key";

export type DownloadClient = {
	name: string;
	client_type: DownloadClientType;
	host: string;
	port: number;
	auth_method: DownloadClientAuth;
	username?: string;
	use_ssl?: boolean;
	priority?: number;
	enabled: boolean;
	api_key_set: boolean;
	password_set: boolean;
	// builtin-only knobs (client_type "builtin"); absent for external clients.
	download_dir?: string;
	listen_port?: number;
	max_upload_kbps?: number;
	max_download_kbps?: number;
	seed_ratio?: number;
	seed_time?: string;
	disable_dht?: boolean;
	// Interface name (e.g. wg0) or IP the engine binds to. Empty = all interfaces.
	bind_interface?: string;
	// Runtime state, populated only for the builtin entry from the live engine.
	running?: boolean;
	port_bound?: number;
	interface_bound?: string;
};

// ── Built-in torrent engine (anacrolix "builtin" download client) ─────────
// The builtin engine's config lives on its DownloadClient entry (client_type
// "builtin"); there is no dedicated /download-clients/builtin endpoint. These
// torrent types mirror the backend TorrentInfo / TorrentDetails schemas.
export type TorrentStatus =
	| "downloading"
	| "seeding"
	| "completed"
	| "paused"
	| "fetching"
	| "stalled";

export type TorrentFilePriority = "skip" | "normal" | "high";

// GET /torrents list item. Light by design — no files/peers/trackers (those
// come from the per-torrent detail query).
export type Torrent = {
	hash: string;
	// Empty while a magnet resolves metadata.
	name: string;
	status: TorrentStatus;
	// Fraction complete, 0..1.
	progress: number;
	// 0 while metadata is unknown.
	size: number;
	// Bytes per second.
	download_speed: number;
	upload_speed: number;
	// Total bytes uploaded so far.
	uploaded: number;
	ratio: number;
	// Seconds to completion; 0 = unknown.
	eta: number;
	// Connected peers holding the complete torrent.
	seeds: number;
	peer_count: number;
	save_path: string;
	added_at: string;
	// True once the ratio/seed-time limit stopped seeding.
	seeding_stopped: boolean;
	// False for arbitrary adds not tied to a library item.
	tracked: boolean;
};

export type TorrentFile = {
	index: number;
	path: string;
	size: number;
	// Bytes downloaded for this file; progress = downloaded / size.
	downloaded: number;
	priority: TorrentFilePriority;
};

export type TorrentPeer = {
	addr: string;
	client?: string;
	// Bytes per second.
	download_rate?: number;
	upload_rate?: number;
};

// GET /torrents/{hash} — the list item plus its file/tracker/peer breakdown.
export type TorrentDetails = Torrent & {
	files: TorrentFile[];
	trackers: string[];
	peers: TorrentPeer[];
};

export type TorrentList = { items: Torrent[]; refreshed_at: string };

// POST /torrents — exactly one of magnet / torrent (base64 .torrent) is set.
export type AddTorrentRequest = {
	magnet?: string;
	torrent?: string;
};

export type TorrentAddResult = { hash: string };

export type MediaServerType = "plex" | "jellyfin" | "emby";

export type MediaServer = {
	name: string;
	server_type: MediaServerType;
	host: string;
	library_section?: string | null;
	enabled: boolean;
	api_key_set: boolean;
};

export type MediaServerSection = {
	key: string;
	name: string;
	type: string;
	locations: string[];
};

export type ScheduleStatus = "never" | "success" | "error" | "skipped";

export type Schedule = {
	name: string;
	interval: string;
	paused: boolean;
	system: boolean;
	running: boolean;
	status: ScheduleStatus;
	last_started_at: string | null;
	last_finished_at: string | null;
	next_run_at: string | null;
	last_duration_ms: number;
	last_error: string | null;
};

export type ScheduleList = {
	items: Schedule[];
};

export type DiskUsage = {
	used: string;
	total: string;
	free: string;
	pct: number;
	kind: "ok" | "warn" | "err";
};

export type SystemInfo = {
	app_name: string;
	public_url: string;
	https_warn: boolean;
	auth_mode: string;
	data_dir: string;
	data_usage?: DiskUsage;
	db_path: string;
	db_size?: string;
	db_usage?: DiskUsage;
	version: string;
	commit?: string;
	built_at?: string;
	go_version: string;
	go_os_arch: string;
};

export type PlexPinBegin = {
	pin_id: number;
	auth_url: string;
	client_id: string;
};
export type PlexPinPoll = { auth_token?: string; expired?: boolean };

export type ImportStatus =
	| "running"
	| "awaiting_review"
	| "committing"
	| "completed"
	| "cancelled"
	| "failed";

export type ImportMode = "in_place" | "rename";
export type ImportTransferMode = "hardlink" | "copy" | "move";

export type ImportScanKind = "movie" | "series";

export type ImportScan = {
	id: number;
	source_path: string;
	kind: ImportScanKind;
	mode: ImportMode;
	import_mode?: ImportTransferMode | "";
	status: ImportStatus;
	total_count: number;
	processed_count: number;
	commit_success_count: number;
	commit_failed_count: number;
	failure_reason?: string | null;
	scanned_at?: string | null;
	committed_at?: string | null;
	created_at: string;
	updated_at: string;
};

export type ImportScanList = {
	items: ImportScan[];
	total: number;
};

export type ImportFileClassification =
	| "confirmed"
	| "ambiguous"
	| "unmatched"
	| "existing";

export type ImportFileOutcome =
	| "pending"
	| "created"
	| "attached"
	| "skipped"
	| "failed";

export type ImportFileDecision = "pending" | "accept" | "skip";

export type ImportScanCandidate = {
	tmdb_id: number;
	title: string;
	year: number;
};

export type ImportScanFile = {
	id: number;
	source_path: string;
	size: number;
	parsed_title?: string;
	parsed_year?: number | null;
	parsed_quality?: string;
	parsed_release_group?: string;
	classification: ImportFileClassification;
	candidates?: ImportScanCandidate[];
	tmdb_id?: number;
	existing_movie_id?: number;
	decision: ImportFileDecision;
	decision_tmdb_id?: number;
	outcome: ImportFileOutcome;
	outcome_message?: string;
	created_movie_id?: number;
	created_at: string;
	updated_at: string;
};

export type ImportScanFileList = {
	items: ImportScanFile[];
	total: number;
};

// Series import scans carry per-show rows instead of per-file rows. Shows reuse
// the file classification/decision enums; outcomes are a shorter set.
export type ImportShowOutcome = "pending" | "created" | "failed";

export type ImportScanShowCandidate = {
	tvdb_id: number;
	title: string;
	year?: number;
};

export type ImportScanShow = {
	id: number;
	folder_path: string;
	parsed_title?: string;
	parsed_year?: number | null;
	classification: ImportFileClassification;
	tvdb_id?: number | null;
	candidates?: ImportScanShowCandidate[];
	existing_tvshow_id?: number | null;
	file_count: number;
	decision: ImportFileDecision;
	decision_tvdb_id?: number | null;
	outcome: ImportShowOutcome;
	outcome_message?: string;
	created_tvshow_id?: number | null;
	created_at: string;
	updated_at: string;
};

export type ImportScanShowList = {
	items: ImportScanShow[];
	total: number;
};

export type ImportStartRequest = {
	source_path: string;
	mode: ImportMode;
	import_mode?: ImportTransferMode | "";
};
