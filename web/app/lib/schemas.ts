import * as v from "valibot";
import { m as i18n } from "./paraglide/messages.js";

export const password = v.pipe(
	v.string(),
	v.minLength(8, i18n.validation_password_min()),
	v.maxLength(128, i18n.validation_too_long()),
);

export const displayName = v.pipe(
	v.string(),
	v.maxLength(64, "Too long"),
);

export const email = v.pipe(v.string(), v.email(i18n.validation_invalid_email()));

export const userRole = v.picklist(
	["admin", "member", "request_only"] as const,
	"Invalid role",
);

export const inviteEmail = v.pipe(v.string(), v.email("Invalid email"));

export const goDuration = v.pipe(
	v.string(),
	v.regex(
		/^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$/,
		"Use a Go duration (e.g. 168h, 30m, 10s)",
	),
);

export const registrationMode = v.picklist(
	["disabled", "open", "invite"] as const,
	"Invalid mode",
);

export const authConfigPatch = v.object({
	registration_mode: registrationMode,
	session_ttl: goDuration,
	oidc_default_role: userRole,
});

export const oidcProviderCreate = v.object({
	name: v.pipe(
		v.string(),
		v.minLength(1, "Required"),
		v.regex(/^[a-z0-9_-]+$/i, "Letters, digits, dash, underscore only"),
	),
	issuer: v.pipe(v.string(), v.url("Must be a valid URL")),
	client_id: v.pipe(v.string(), v.minLength(1, "Required")),
	client_secret: v.pipe(v.string(), v.minLength(1, "Required")),
});

export const resolution = v.picklist(
	["720p", "1080p", "2160p"] as const,
	"Invalid resolution",
);

export const qualityProfileFormatScore = v.object({
	name: v.pipe(v.string(), v.minLength(1, "Pick a format")),
	score: v.pipe(v.number("Score required"), v.integer("Whole numbers only")),
});

const score = v.pipe(v.number("Number required"), v.integer("Whole numbers only"));

export const qualityProfile = v.object({
	name: v.pipe(v.string(), v.minLength(1, "Required")),
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
	] as const,
	"Pick a condition type",
);

// Which fields a condition type actually reads. The editor keeps every field
// on every row so a type switch is reversible, so the checks below have to be
// scoped by type rather than run over the whole row.
export const PATTERN_CONDITIONS = ["release_title", "release_group"] as const;
export const VALUE_CONDITIONS = ["resolution", "source", "codec"] as const;

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
		"Pattern required",
	),
	v.check(
		(c) =>
			!(VALUE_CONDITIONS as readonly string[]).includes(c.type) ||
			c.value.trim().length > 0,
		"Value required",
	),
	v.check(
		(c) => c.type !== "resolution" || ["720p", "1080p", "2160p"].includes(c.value),
		"Pick a resolution",
	),
	v.check(
		(c) => c.type !== "size" || c.min_gb > 0 || c.max_gb > 0,
		"Set a minimum, a maximum, or both",
	),
	v.check(
		(c) => c.type !== "size" || c.max_gb === 0 || c.max_gb >= c.min_gb,
		"Maximum must not be below the minimum",
	),
	v.check((c) => c.type !== "seeders" || c.min > 0, "Minimum seeders required"),
);

export const customFormat = v.object({
	name: v.pipe(
		v.string(),
		v.minLength(1, "Required"),
		v.maxLength(64, "Too long"),
	),
	conditions: v.pipe(
		v.array(customFormatCondition),
		v.minLength(1, "Add at least one condition"),
	),
});

const port = v.pipe(
	v.number("Port required"),
	v.integer(),
	v.minValue(1, "1–65535"),
	v.maxValue(65535, "1–65535"),
);

const priority = v.pipe(
	v.number("Priority required"),
	v.integer(),
	v.minValue(0, "0–255"),
	v.maxValue(255, "0–255"),
);

export const indexerProtocol = v.picklist(
	["torznab", "prowlarr"] as const,
	"Pick a protocol",
);

export const indexerForm = v.object({
	name: v.pipe(v.string(), v.minLength(1, "Required")),
	protocol: indexerProtocol,
	host: v.pipe(v.string(), v.minLength(1, "Required")),
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
	"Pick a client",
);

export const downloadClientAuth = v.picklist(
	["password", "api_key"] as const,
	"Pick an auth method",
);

export const downloadClientForm = v.object({
	name: v.pipe(v.string(), v.minLength(1, "Required")),
	client_type: downloadClientType,
	host: v.pipe(v.string(), v.minLength(1, "Required")),
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
	v.number("Enter a number"),
	v.integer(),
	v.minValue(0, "0 = unlimited"),
);

export const builtinClientForm = v.object({
	download_dir: v.pipe(
		v.string(),
		v.minLength(1, "Required"),
		v.regex(/^\//, "Must be an absolute path"),
	),
	bind_interface: v.pipe(
		v.string(),
		v.regex(
			/^$|^[A-Za-z0-9._:-]+$/,
			"Interface name (e.g. wg0) or IP — empty = all interfaces",
		),
	),
	listen_port: v.pipe(
		v.number("Enter a port"),
		v.integer(),
		v.minValue(0, "0 (auto) – 65535"),
		v.maxValue(65535, "0 (auto) – 65535"),
	),
	max_download_kbps: kbps,
	max_upload_kbps: kbps,
	seed_ratio: v.pipe(v.number("Enter a ratio"), v.minValue(0, "0 = unlimited")),
	seed_time: v.pipe(
		v.string(),
		v.regex(
			/^$|^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$/,
			"Empty = unlimited, or a Go duration (e.g. 72h)",
		),
	),
	disable_dht: v.boolean(),
	enabled: v.boolean(),
});

export const mediaServerType = v.picklist(
	["plex", "jellyfin", "emby"] as const,
	"Pick a server type",
);

export const mediaServerForm = v.object({
	name: v.pipe(v.string(), v.minLength(1, "Required")),
	server_type: mediaServerType,
	host: v.pipe(v.string(), v.minLength(1, "Required")),
	api_key: v.string(),
	library_section: v.string(),
	enabled: v.boolean(),
});

export const scheduleInterval = goDuration;

export const importMode = v.picklist(["in_place", "rename"] as const, "Pick a mode");

export const importTransferMode = v.picklist(
	["", "hardlink", "copy", "move"] as const,
	"Pick a transfer mode",
);

export const importScanKind = v.picklist(
	["movie", "series"] as const,
	"Pick a media type",
);

export const importStartForm = v.object({
	source_path: v.pipe(
		v.string(),
		v.minLength(1, "Required"),
		v.regex(/^\//, "Must be an absolute path"),
	),
	kind: importScanKind,
	mode: importMode,
	import_mode: importTransferMode,
});
