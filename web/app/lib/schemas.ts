import * as v from "valibot";

export const password = v.pipe(
	v.string(),
	v.minLength(8, "At least 8 characters"),
	v.maxLength(128, "Too long"),
);

export const displayName = v.pipe(
	v.string(),
	v.maxLength(64, "Too long"),
);

export const email = v.pipe(v.string(), v.email("Invalid email"));

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

export const qualityProfile = v.object({
	name: v.pipe(v.string(), v.minLength(1, "Required")),
	preferred_resolution: resolution,
	min_resolution: resolution,
	upgrade_allowed: v.boolean(),
});

const port = v.pipe(
	v.number("Port required"),
	v.integer(),
	v.minValue(1, "1–65535"),
	v.maxValue(65535, "1–65535"),
);

const priority = v.pipe(
	v.number(),
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

export const importStartForm = v.object({
	source_path: v.pipe(
		v.string(),
		v.minLength(1, "Required"),
		v.regex(/^\//, "Must be an absolute path"),
	),
	mode: importMode,
	import_mode: importTransferMode,
});
