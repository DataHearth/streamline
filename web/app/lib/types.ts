export type MovieStatus = "wanted" | "downloading" | "available" | "failed";

export type SeriesStatus = "continuing" | "ended" | "upcoming";
export type SeriesType = "standard" | "anime" | "daily";
export type EpisodeStatus =
	| "wanted"
	| "downloading"
	| "importing"
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

// Library-list monitoring filter. Client-side only — both list endpoints
// return every row and the SPA narrows the set.
export type MonitorFilter = "all" | "monitored" | "unmonitored";

export type Episode = {
	id: number;
	number: number;
	absolute_number?: number;
	title?: string;
	overview?: string;
	air_date?: string | null;
	status: EpisodeStatus;
	monitored: boolean;
	quality?: string;
	size?: number | null;
	has_file?: boolean;
	path?: string;
	release_group?: string;
	parsed_source?: string;
	file_score?: number;
	media_info?: MediaInfo | null;
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
	added_at?: string;
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

// Everything the add/request modals show for a highlighted lookup result that
// the search response itself doesn't carry. Fetched per selected title from
// GET /search/movie/:tmdb_id and GET /series/lookup/:tvdb_id. One shape serves
// both: movies fill runtime/release_date, series fill network/season_count.
export type LookupDetail = {
	overview?: string;
	tagline?: string;
	// Theatrical release (movie) or first air date (series), ISO yyyy-mm-dd.
	release_date?: string;
	// Minutes — feature length for a movie, average episode length for a series.
	runtime?: number;
	rating?: number;
	vote_count?: number;
	genres?: string[];
	cast?: CastMember[];
	original_language?: string;
	tmdb_id?: number;
	tvdb_id?: number;
	imdb_id?: string;
	// Series only.
	network?: string;
	season_count?: number;
	episode_count?: number;
	status?: string;
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
	added_at?: string;
};

// What ffprobe read off the file. Null until the file has been probed, and
// still null when probing is disabled — every consumer falls back to the
// parsed_* fields per field. See lib/media-info.ts.
export type MediaInfo = {
	container?: string;
	video_codec?: string;
	width?: number;
	height?: number;
	duration_seconds?: number;
	// The default (or first) audio track, flattened. Kept alongside audio_tracks:
	// it is what the brief's contract carries, and a file whose streams could not
	// be enumerated may still report this much.
	audio_codec?: string;
	audio_channels?: number;
	bitrate?: number;
	probed_at?: string;
	// Every audio and subtitle stream ffprobe found. Languages are ISO 639-2/B
	// as ffprobe reports them ("eng", "fra", "und").
	audio_tracks?: AudioTrack[];
	subtitles?: SubtitleTrack[];
};

export type AudioTrack = {
	language?: string;
	codec?: string;
	channels?: number;
	default?: boolean;
	// ffprobe's stream title, when the muxer set one ("Surround", "Commentary").
	title?: string;
};

export type SubtitleTrack = {
	language?: string;
	codec?: string;
	forced?: boolean;
	hearing_impaired?: boolean;
	default?: boolean;
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
	// Matched-format total against the owning item's quality profile. Detail
	// responses only — list responses omit it, and it is absent altogether when
	// no quality profile resolves. 0 is a real score, not "unknown".
	file_score?: number;
	media_info?: MediaInfo | null;
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
	// Scoring against the queried item's quality profile. Absent when no profile
	// resolves; comparing scores across items on different profiles is
	// meaningless. The API returns rows already sorted score-descending.
	score?: number;
	rejected?: boolean;
	reject_reason?: string;
	matched_formats?: string[];
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

export type TMDBMovieResult = {
	tmdb_id: number;
	title: string;
	original_title: string;
	year: number;
	overview?: string;
	already_added?: boolean;
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
	is_default?: boolean;
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
	// Quality profile the requester asked for; empty means no preference. The
	// reviewer's approve form starts here and can override it.
	quality_profile?: string;
	requester: RequestUser;
	approved_by?: RequestUser;
	created_at: string;
	updated_at: string;
};

export type RequestCounts = {
	pending: number;
	approved: number;
	denied: number;
	available: number;
};

// Cover + full metadata fetched on demand so reviewers can judge a request.
// Extends LookupDetail with the cover, so the same LookupDetailPanel renders
// an expanded request row and a highlighted add-modal result.
export type RequestMediaDetails = LookupDetail & {
	poster_url?: string;
	year?: number;
};

export type MovieCounts = {
	total: number;
	wanted: number;
	downloading: number;
	available: number;
	failed: number;
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
	status: EpisodeStatus;
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
	| "drift_confirmed"
	| "searched";

export type SeriesRef = {
	id: number;
	title: string;
};

// Exactly one of movie / episode / series is set — what the event happened to.
// series carries only what belongs to no single episode (a search issued at
// series or season scope); per-episode outcomes use episode.
export type ActivityEvent = {
	id: number;
	type: ActivityType;
	created_at: string;
	payload?: Record<string, unknown>;
	movie?: Movie;
	episode?: EpisodeRef;
	series?: SeriesRef;
};

export type ActivityList = {
	events: ActivityEvent[];
	next_cursor: string | null;
};

// Live download queue (GET /activity/queue) — DownloadRecords still in
// flight, enriched with client telemetry.
export type EpisodeRef = {
	show_title: string;
	season: number;
	episode: number;
	series_id?: number;
};

export type HoldCheck =
	| "resolution"
	| "corrupt"
	| "duration"
	| "codec"
	// Not a failed check: `library.probe.always_ask` holds an otherwise-clean
	// import so a person signs off on it, so it carries no expected/actual pair.
	| "always_ask";

export type HoldReason = {
	file: string;
	check: HoldCheck;
	expected?: string;
	actual?: string;
};

export type QueueEntry = {
	id: number;
	// "held": the download finished and failed verification, so it is waiting on
	// a decision rather than on the network. The only queue state whose next move
	// belongs to a person.
	status: "downloading" | "importing" | "paused" | "error" | "held";
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
	hold_reasons?: HoldReason[];
	// Omitted entirely when "skipped" — no selection was ever applied, and the
	// whole torrent downloads. selected_files_count/selected_bytes are present
	// only when selection_state is "applied".
	selection_state?: "pending" | "applied" | "unsupported";
	selected_files_count?: number;
	selected_bytes?: number;
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
	// Present only when media is absent: what the parser made of the release
	// name, used to seed the identify search.
	parsed_title?: string;
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
	lockout: LockoutConfig;
};

// Per-account login-failure lockout. Every field is optional on a patch: an
// omitted one keeps its stored value.
export type LockoutConfig = {
	threshold?: number;
	window?: string;
	duration?: string;
};

// The three roots are read-only here — a root only moves through the path
// migration on Settings → Advanced, which rewrites every stored path before it
// repoints the config. They ride along so the page can show where the library
// lives without a second request.
export type LibraryConfig = {
	monitor_specials: boolean;
	movie_naming: string;
	series_naming: string;
	import_mode: ImportTransferMode;
	keep_torrent_seeding: boolean;
	no_match_cooldown: string;
	max_grab_failures: number;
	import_max_attempts: number;
	drift_grace_ticks: number;
	allowed_download_roots?: string[];
	movie_path?: string;
	series_path?: string;
	download_path?: string;
	probe?: ProbeConfig;
};

export type DownloadConfig = {
	selective_files: boolean;
	selection_grace: string;
};

// The api keys are write-only: the response says whether one is configured
// (`*_set`) and whether a `*_file` path owns it (`*_file_managed`, which is
// what makes the field read-only), never the value.
export type MetadataConfig = {
	language: string;
	tmdb_region: string;
	tmdb_api_key_set: boolean;
	tvdb_api_key_set: boolean;
	tmdb_api_key_file_managed?: boolean;
	tvdb_api_key_file_managed?: boolean;
	restart_required: boolean;
};

// The server's own bookkeeping. Only `events_retention` applies without a
// restart — the log handlers and OTLP exporters are built once at boot, which
// is what `restart_required` reports.
export type SystemConfig = {
	log: LogConfig;
	otel_endpoint: string;
	events_retention: string;
	restart_required: boolean;
};

// One shape for the read view and the patch: on a patch an omitted field keeps
// its stored value, which is why everything nests optional.
export type LogConfig = {
	app?: AppLogConfig;
	http?: HTTPLogConfig;
};

export type AppLogConfig = {
	enabled?: boolean;
	level?: "debug" | "info" | "warn" | "error";
	format?: "text" | "json";
	output?: string;
	rotate?: LogRotateConfig;
};

export type HTTPLogConfig = {
	enabled?: boolean;
	format?: "json" | "combined";
	output?: string;
	rotate?: LogRotateConfig;
};

// Only meaningful when the matching output is a file path. 0 disables a bound.
export type LogRotateConfig = {
	max_size_mb?: number;
	max_backups?: number;
	max_age_days?: number;
	compress?: boolean;
};

export type MetadataConfigPatch = {
	language?: string;
	tmdb_region?: string;
	tmdb_api_key?: string;
	tvdb_api_key?: string;
};

export type ProbeConfig = {
	// Hold every import for a decision, even when every check passes.
	always_ask?: boolean;
	// A file shorter than this share of the expected runtime fails the duration
	// check. Stored as a ratio (0.9); the settings field shows it as a percentage.
	min_duration_ratio?: number;
};

// The ffmpeg suite, not just ffprobe — the same block serves playback later.
// `found` is what the settings page warns on and what the TopBar health pill's
// new reason string comes from.
export type FFmpegConfig = {
	enabled: boolean;
	path: string;
	found?: boolean;
	resolved_path?: string;
	version?: string;
	restart_required?: boolean;
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

export type QualityProfileFormatScore = {
	// A built-in format name or a custom_formats entry name.
	name: string;
	score?: number;
};

export type QualityProfileFull = {
	name: string;
	// True when quality_default_profile names this profile — the one a movie or
	// series with an empty quality_profile resolves to.
	is_default?: boolean;
	preferred_resolution: Resolution;
	min_resolution: Resolution;
	upgrade_allowed: boolean;
	// ffprobe codec names ("hevc", "av1"). Empty means any codec.
	allowed_codecs?: string[];
	formats?: QualityProfileFormatScore[];
	// The API omits both when they are 0, so absent reads as 0: no minimum, and
	// no upgrade ceiling.
	min_score?: number;
	upgrade_until_score?: number;
};

export type CustomFormatConditionType =
	| "release_title"
	| "resolution"
	| "source"
	| "release_group"
	| "codec"
	| "size"
	| "seeders";

// Only the fields its `type` reads are meaningful; the API omits the rest and
// the editor keeps them around so switching a row's type back restores what
// was typed. min_gb/max_gb of 0 mean "unbounded", not "0 GB".
export type CustomFormatCondition = {
	type: CustomFormatConditionType;
	pattern?: string;
	value?: string;
	min_gb?: number;
	max_gb?: number;
	min?: number;
	required?: boolean;
	negate?: boolean;
};

export type CustomFormat = {
	name: string;
	// Set only on the shipped library, which cannot be edited or deleted.
	builtin?: boolean;
	// Human-readable explanation. Fixed on the shipped library; optional and
	// author-set on your own formats.
	description?: string;
	conditions: CustomFormatCondition[];
};

export type CustomFormatTestSample = {
	title: string;
	// Bytes, as the API takes them — the editor asks for GB.
	size?: number;
	seeders?: number;
};

export type CustomFormatTestResult = {
	matched: boolean;
	conditions: { index: number; passed: boolean }[];
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
	// Present on the builtin entry only, and only when the top-level
	// torrent_listen_port is set (normally by STREAMLINE_TORRENT_LISTEN_PORT).
	// It wins over listen_port, so a form showing this must present listen_port
	// as having no effect rather than as the port in force.
	listen_port_override?: number;
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
	known_peers?: number;
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
	library_section_tv?: string | null;
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
	free_bytes: number;
	pct: number;
	kind: "ok" | "warn" | "err";
};

export type SystemInfo = {
	app_name: string;
	public_url: string;
	https_warn: boolean;
	ffmpeg_warn?: boolean;
	auth_mode: string;
	data_dir: string;
	data_usage?: DiskUsage;
	db_path: string;
	db_size?: string;
	db_usage?: DiskUsage;
	library_dir?: string;
	library_usage?: DiskUsage;
	series_dir?: string;
	series_usage?: DiskUsage;
	version: string;
	commit?: string;
	built_at?: string;
	go_version: string;
	go_os_arch: string;
	// File-only by design — the trust boundary, the bootstrap block, and what
	// the process generated for itself. Reported so Settings → General can say
	// what is in force; secrets appear as a source, never a value.
	server_host?: string;
	server_port?: number;
	read_only?: boolean;
	trusted_proxies?: string[];
	trusted_networks?: string[];
	trusted_role?: string;
	seed_admin_email?: string;
	seed_admin_secret?: "file" | "config" | "unset";
	session_secret_file?: string;
	plex_client_id?: string;
	torrent_listen_port?: number;
	tmdb_api_key_file?: string;
	tvdb_api_key_file?: string;
};

export type PlexPinBegin = {
	flow_id: string;
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

export type ImportCounts = {
	running: number;
	awaiting_review: number;
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

// Series import scans carry per-show rows instead of per-file rows. Shows reuse
// the file classification/decision enums; outcomes are a shorter set.
export type ImportShowOutcome = "pending" | "created" | "attached" | "failed";

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

export type ImportStartRequest = {
	source_path: string;
	kind?: ImportScanKind;
	mode: ImportMode;
	import_mode?: ImportTransferMode | "";
};

export type MigrationRoot = "movies" | "series" | "downloads";

export type PathMigrationRequest = {
	root: MigrationRoot;
	from?: string;
	to: string;
	move_files?: boolean;
};

export type PathRewrite = {
	from: string;
	to: string;
};

export type PathMigrationRoot = {
	root: MigrationRoot;
	path: string;
	tracked: number;
	total: number;
};

export type PathMigrationRootList = {
	items: PathMigrationRoot[];
};

export type PathMigrationPreview = {
	root: MigrationRoot;
	from: string;
	to: string;
	total: number;
	skipped: number;
	can_move: boolean;
	samples: PathRewrite[];
};

export type PathMigration = {
	running: boolean;
	root: string;
	from: string;
	to: string;
	move_files: boolean;
	total: number;
	done: number;
	skipped: number;
	current: string;
	error?: string;
	started_at?: string;
	finished_at?: string;
};

// POST /{movies,series}/{id}/reidentify — the repaired entry plus what moved.
export type ReidentifyResult = {
	id: number;
	title: string;
	renamed: number;
	// Series only: paths still on disk whose season/episode number has no
	// counterpart in the new show.
	unmatched?: string[];
};
