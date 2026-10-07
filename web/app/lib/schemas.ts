import * as v from "valibot";
import { m as i18n } from "./paraglide/messages.js";
import type { MusicFormat } from "./types";

export const password = v.pipe(
	v.string(),
	v.minLength(8, i18n.validation_password_min()),
	v.maxLength(128, i18n.validation_too_long()),
);

export const displayName = v.pipe(
	v.string(),
	v.maxLength(64, i18n.validation_too_long()),
);

export const email = v.pipe(v.string(), v.email(i18n.validation_invalid_email()));

export const userRole = v.picklist(
	["admin", "member", "request_only"] as const,
	i18n.validation_invalid_role(),
);

export const inviteEmail = v.pipe(v.string(), v.email(i18n.validation_invalid_email()));

export const goDuration = v.pipe(
	v.string(),
	v.regex(
		/^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$/,
		i18n.validation_go_duration(),
	),
);

export const registrationMode = v.picklist(
	["disabled", "open", "invite"] as const,
	i18n.validation_invalid_mode(),
);

export const authConfigPatch = v.object({
	registration_mode: registrationMode,
	session_ttl: goDuration,
	default_role: userRole,
	lockout: v.object({
		threshold: v.pipe(
			v.number(),
			v.integer(i18n.validation_whole_number()),
			v.minValue(1, i18n.validation_at_least_one()),
			v.maxValue(255, i18n.validation_at_most_255()),
		),
		window: goDuration,
		duration: goDuration,
	}),
});

// A blank api key means "leave the stored one alone", so the empty string has
// to pass — the field is never seeded with the current value.
const optionalSecret = v.pipe(v.string(), v.maxLength(256, i18n.validation_too_long()));

export const metadataConfigPatch = v.object({
	language: v.pipe(
		v.string(),
		v.regex(/^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$/, i18n.validation_bcp47()),
	),
	tmdb_region: v.pipe(
		v.string(),
		v.regex(/^[A-Z]{2}$/, i18n.validation_region_code()),
	),
	tmdb_api_key: optionalSecret,
	tvdb_api_key: optionalSecret,
});

export const oidcProviderCreate = v.object({
	name: v.pipe(
		v.string(),
		v.minLength(1, i18n.validation_required()),
		v.regex(/^[a-z0-9_-]+$/i, i18n.validation_slug_chars()),
	),
	issuer: v.pipe(v.string(), v.url(i18n.validation_url())),
	client_id: v.pipe(v.string(), v.minLength(1, i18n.validation_required())),
	client_secret: v.pipe(v.string(), v.minLength(1, i18n.validation_required())),
});

export const resolution = v.picklist(
	["720p", "1080p", "2160p"] as const,
	i18n.validation_invalid_resolution(),
);

export const qualityProfileFormatScore = v.object({
	name: v.pipe(v.string(), v.minLength(1, i18n.validation_pick_format())),
	score: v.pipe(v.number(i18n.validation_score_required()), v.integer(i18n.validation_whole_numbers_only())),
});

const score = v.pipe(v.number(i18n.validation_number_required()), v.integer(i18n.validation_whole_numbers_only()));

// ffmpeg-style rate: "8M", "4500k", or a bare bits-per-second count. Empty is
// the valid "no bitrate rule" value, which is why this is not a minLength.
export const transcodeBitrate = v.union([
	v.literal(""),
	v.pipe(
		v.string(),
		v.regex(/^\d+(\.\d+)?[kKmM]?$/, i18n.validation_bitrate()),
	),
]);

export const transcodeTo = v.object({
	container: v.picklist(["mkv", "mp4"]),
	video_codec: v.picklist(["h264", "hevc", "av1"]),
	// 0 means "leave -crf off", not a near-lossless encode, so it is a legal
	// value rather than a missing one.
	crf: v.pipe(
		v.number(i18n.validation_number_required()),
		v.integer(i18n.validation_whole_numbers_only()),
		v.minValue(0, "0–51"),
		v.maxValue(51, "0–51"),
	),
	preset: v.picklist([
		"ultrafast",
		"superfast",
		"veryfast",
		"faster",
		"fast",
		"medium",
		"slow",
		"slower",
		"veryslow",
	]),
	audio_codec: v.picklist(["aac", "opus", "ac3", "flac"]),
	audio_passthrough: v.array(v.string()),
});

export const transcodeIf = v.object({
	video_codecs: v.array(v.string()),
	containers: v.array(v.string()),
	max_video_bitrate: transcodeBitrate,
	min_video_bitrate: transcodeBitrate,
});

export const qualityProfile = v.object({
	name: v.pipe(v.string(), v.minLength(1, i18n.validation_required())),
	preferred_resolution: resolution,
	min_resolution: resolution,
	upgrade_allowed: v.boolean(),
	// Empty means any codec, which is how every profile that predates the media
	// probe behaves — so there is no minLength here on purpose. Not v.optional:
	// QualityProfileForm's defaultValues and openEdit both always populate this
	// as an array, never undefined, so the schema's input type must say the
	// same or it stops matching FormApi's onChange validator type.
	allowed_codecs: v.array(v.string()),
	// A format name is checked for presence only: which names resolve is the
	// server's table (built-ins plus the config's custom formats), and it
	// answers 422 for one that doesn't.
	formats: v.array(qualityProfileFormatScore),
	// Both thresholds are signed: a negative min_score is a profile that still
	// grabs a release the junk formats scored down.
	min_score: score,
	upgrade_until_score: score,
	// The form always carries a policy object plus the enable flag, and the
	// page strips both before the request — the API's `transcode` is absent or
	// whole, never half-filled.
	transcode_enabled: v.boolean(),
	transcode: v.object({ if: transcodeIf, to: transcodeTo }),
});

export const customFormatConditionType = v.picklist(
	[
		"release_title",
		"resolution",
		"source",
		"release_group",
		"codec",
		"size",
		"seeders",
		"audio_tracks",
		"audio_language",
		"subtitle_language",
	] as const,
	i18n.validation_pick_condition_type(),
);

// Which fields a condition type actually reads. The editor keeps every field
// on every row so a type switch is reversible, so the checks below have to be
// scoped by type rather than run over the whole row.
export const PATTERN_CONDITIONS = ["release_title", "release_group"] as const;
export const VALUE_CONDITIONS = [
	"resolution",
	"source",
	"codec",
	"audio_language",
	"subtitle_language",
] as const;

// A pattern's *syntax* is deliberately not validated here. The backend compiles
// Go RE2, which JS RegExp cannot stand in for: `(?i)` — the inline flag this
// app's own help text recommends and every built-in format uses — is a syntax
// error to `new RegExp`, so a local check rejected patterns the server accepts
// and made every case-insensitive format un-editable and un-testable. The
// server is the validator: the tester round-trips POST /custom-formats/test and
// a save surfaces the 422.
export const customFormatCondition = v.pipe(
	v.object({
		type: customFormatConditionType,
		pattern: v.string(),
		value: v.string(),
		min_gb: v.number(),
		max_gb: v.number(),
		min: v.number(),
		required: v.boolean(),
		negate: v.boolean(),
	}),
	v.check(
		(c) =>
			!(PATTERN_CONDITIONS as readonly string[]).includes(c.type) ||
			c.pattern.trim().length > 0,
		i18n.validation_pattern_required(),
	),
	v.check(
		(c) =>
			!(VALUE_CONDITIONS as readonly string[]).includes(c.type) ||
			c.value.trim().length > 0,
		i18n.validation_value_required(),
	),
	v.check(
		(c) => c.type !== "resolution" || ["720p", "1080p", "2160p"].includes(c.value),
		i18n.validation_pick_resolution(),
	),
	v.check(
		(c) => c.type !== "size" || c.min_gb > 0 || c.max_gb > 0,
		i18n.validation_size_bounds(),
	),
	v.check(
		(c) => c.type !== "size" || c.max_gb === 0 || c.max_gb >= c.min_gb,
		i18n.validation_max_below_min(),
	),
	v.check((c) => c.type !== "seeders" || c.min > 0, i18n.validation_min_seeders()),
	v.check(
		(c) => c.type !== "audio_tracks" || c.min > 0,
		i18n.validation_min_audio_tracks(),
	),
);

export const customFormat = v.object({
	name: v.pipe(
		v.string(),
		v.minLength(1, i18n.validation_required()),
		v.maxLength(64, i18n.validation_too_long()),
	),
	conditions: v.pipe(
		v.array(customFormatCondition),
		v.minLength(1, i18n.validation_one_condition()),
	),
});

const port = v.pipe(
	v.number(i18n.validation_port_required()),
	v.integer(),
	v.minValue(1, "1–65535"),
	v.maxValue(65535, "1–65535"),
);

const priority = v.pipe(
	v.number(i18n.validation_priority_required()),
	v.integer(),
	v.minValue(0, "0–255"),
	v.maxValue(255, "0–255"),
);

export const indexerProtocol = v.picklist(
	["torznab", "prowlarr"] as const,
	i18n.validation_pick_protocol(),
);

export const indexerForm = v.object({
	name: v.pipe(v.string(), v.minLength(1, i18n.validation_required())),
	protocol: indexerProtocol,
	host: v.pipe(v.string(), v.minLength(1, i18n.validation_required())),
	port,
	path: v.string(),
	use_ssl: v.boolean(),
	// Blank keeps the existing key on edit; the backend requires it on create.
	api_key: v.string(),
	priority,
	enabled: v.boolean(),
});

export const downloadClientType = v.picklist(
	["qbittorrent", "transmission", "deluge"] as const,
	i18n.validation_pick_client(),
);

export const downloadClientAuth = v.picklist(
	["password", "api_key"] as const,
	i18n.validation_pick_auth(),
);

export const downloadClientForm = v.object({
	name: v.pipe(v.string(), v.minLength(1, i18n.validation_required())),
	client_type: downloadClientType,
	host: v.pipe(v.string(), v.minLength(1, i18n.validation_required())),
	port,
	auth_method: downloadClientAuth,
	username: v.string(),
	password: v.string(),
	api_key: v.string(),
	use_ssl: v.boolean(),
	priority,
	enabled: v.boolean(),
});

// Built-in torrent engine (anacrolix) config. No host/port/auth/priority —
// a constructed engine runs in-process. listen_port 0 = auto; kbps 0 =
// unlimited; seed_ratio 0 = unlimited; seed_time empty = unlimited.
const kbps = v.pipe(
	v.number(i18n.validation_enter_number()),
	v.integer(),
	v.minValue(0, i18n.validation_zero_unlimited()),
);

export const builtinClientForm = v.object({
	download_dir: v.pipe(
		v.string(),
		v.minLength(1, i18n.validation_required()),
		v.regex(/^\//, i18n.validation_absolute_path()),
	),
	bind_interface: v.pipe(
		v.string(),
		v.regex(
			/^$|^[A-Za-z0-9._:-]+$/,
			i18n.validation_bind_interface(),
		),
	),
	listen_port: v.pipe(
		v.number(i18n.validation_enter_port()),
		v.integer(),
		v.minValue(0, i18n.validation_port_auto_range()),
		v.maxValue(65535, i18n.validation_port_auto_range()),
	),
	max_download_kbps: kbps,
	max_upload_kbps: kbps,
	seed_ratio: v.pipe(v.number(i18n.validation_enter_ratio()), v.minValue(0, i18n.validation_zero_unlimited())),
	seed_time: v.pipe(
		v.string(),
		v.regex(
			/^$|^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$/,
			i18n.validation_seed_time(),
		),
	),
	disable_dht: v.boolean(),
	enabled: v.boolean(),
});

export const mediaServerType = v.picklist(
	["plex", "jellyfin", "emby"] as const,
	i18n.validation_pick_server_type(),
);

export const mediaServerForm = v.object({
	name: v.pipe(v.string(), v.minLength(1, i18n.validation_required())),
	server_type: mediaServerType,
	host: v.pipe(v.string(), v.minLength(1, i18n.validation_required())),
	api_key: v.string(),
	library_section: v.string(),
	library_section_tv: v.string(),
	enabled: v.boolean(),
});

export const scheduleInterval = goDuration;

export const importMode = v.picklist(["in_place", "rename"] as const, i18n.validation_pick_mode());

export const importTransferMode = v.picklist(
	["", "hardlink", "copy", "move"] as const,
	i18n.validation_pick_transfer_mode(),
);

export const importScanKind = v.picklist(
	["movie", "series"] as const,
	i18n.validation_pick_media_type(),
);

export const importStartForm = v.object({
	source_path: v.pipe(
		v.string(),
		v.minLength(1, i18n.validation_required()),
		v.regex(/^\//, i18n.validation_absolute_path()),
	),
	kind: importScanKind,
	mode: importMode,
	import_mode: importTransferMode,
});

// Best first, which is the server's config.MusicFormats order. Which names are
// accepted is the server's call and it answers 422.
export const MUSIC_FORMATS = [
	"flac-24",
	"flac",
	"mp3-320",
	"mp3-v0",
	"mp3-256",
	"mp3-192",
	"other",
] as const satisfies readonly MusicFormat[];

const musicFormat = v.picklist(MUSIC_FORMATS, i18n.validation_required());

export const musicQualityProfile = v.object({
	name: v.pipe(v.string(), v.minLength(1, i18n.validation_required())),
	formats: v.pipe(
		v.array(musicFormat),
		v.minLength(1, i18n.validation_required()),
	),
	cutoff: musicFormat,
	upgrade_allowed: v.boolean(),
});

// "" in quality_profile is the server default.
export const musicArtistAdd = v.object({
	monitored: v.boolean(),
	quality_profile: v.string(),
});
