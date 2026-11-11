import { m as i18n } from "./paraglide/messages.js";

// Extensions: WebAssembly modules an admin adds from a source (an https://
// URL or an absolute path on the server) and a SHA-256, both copied from the
// release notes. There is no catalogue and no one-click install, by decision.
// A module declares what it needs; the admin grants all of it or nothing, and
// the entry keeps exactly those grants. Changes apply live.

// `starting` is an entry the server's boot has not reached yet: seconds, and
// only right after a restart.
export type ExtensionState = "running" | "disabled" | "needs_approval" | "failed" | "starting";

// How an address signs its users in: by itself (an e-reader with an app
// password), or with the Streamline session the browser already has.
export type MountAuth = "self" | "session";
// `auth` is the module's own declaration, so a module that could not be read
// leaves it unknown.
export type ExtensionMount = { path: string; auth?: MountAuth };

// One kind of library data the module can read, with the server's
// admin-facing description ("See users' email addresses"). Version 1 has
// the account views (users, user_logins) and the books and music bundles.
export type ExtensionView = { name: string; description: string; bundle?: string };

export type ExtensionService = "serve_file" | "serve_poster" | "ffmpeg" | "library_scan";

// What a module asks for, or what an entry holds, in the words the admin reads.
export type ExtensionPermissions = {
	mounts: ExtensionMount[];
	views: ExtensionView[];
	services: ExtensionService[];
	folders: string[];
};

// What an entry stores and the YAML carries: names only.
export type ExtensionGrants = {
	mounts: string[];
	views: string[];
	services: ExtensionService[];
	folders: string[];
};

// An account card the module declares. `credential` is the only kind in
// version 1: core draws it and owns every word on it; the module gives a
// title, the mount the client is pointed at, and whether the client signs in
// with the account's email or with no username at all.
export type ExtensionCardDecl = {
	kind: "credential";
	title: string;
	mount: string;
	username: "email" | "none";
};

export type Extension = {
	name: string;
	// Read from the module, so absent when it could not be read.
	version?: string;
	source: string;
	// Required for a URL source, optional for a path.
	sha256?: string;
	enabled: boolean;
	state: ExtensionState;
	// failed: why, in the server's words. Rendered verbatim: checksum
	// mismatch, file over 32 MiB, http:// source, reserved or taken address,
	// unknown view, needs an on-disk database.
	reason?: string;
	// What the module asks for (absent when it could not be read) and what the
	// entry holds. needs_approval is a module asking for something the entry
	// does not hold — typically a new version.
	requested?: ExtensionPermissions;
	granted: ExtensionPermissions;
	card?: ExtensionCardDecl;
};

// GET /extensions (admin).
export type ExtensionList = { items: Extension[] };

// POST /extensions/inspect: the module read, nothing saved.
export type ExtensionInspect = {
	name: string;
	version: string;
	requested: ExtensionPermissions;
	card?: ExtensionCardDecl;
};

// POST /extensions: the name is the module's own.
export type ExtensionSave = {
	name: string;
	source: string;
	sha256?: string;
	enabled: boolean;
	grants: ExtensionGrants;
};

// GET /account/extensions: one per running extension that declares a card,
// for whoever is signed in.
export type ExtensionAccountCard = {
	extension: string;
	kind: "credential";
	title: string;
	// The path a client is pointed at, under this instance's address: /opds.
	mount: string;
	username: "email" | "none";
	// Whether the signed-in account has a secret for it — the On / Off the
	// touch index states beside the row.
	enabled: boolean;
};

export const EXTENSIONS_PATH = "/extensions";
export const EXTENSIONS_KEY = ["extensions"];
export const extensionPath = (name: string) => `/extensions/${encodeURIComponent(name)}`;
export const ACCOUNT_EXTENSIONS_PATH = "/account/extensions";
export const ACCOUNT_EXTENSIONS_KEY = ["account", "extensions"];
export const extensionCredentialPath = (name: string) =>
	`/account/extensions/${encodeURIComponent(name)}/credential`;
export const extensionCredentialKey = (name: string) => ["account", "extensions", name];

// Where the notice and the empty state send an admin for the how-to.
export const EXTENSIONS_DOCS_URL = "https://github.com/DataHearth/streamline/wiki/Extensions";

export const STATE_PILL: Record<ExtensionState, { token: string; label: () => string }> = {
	running: { token: "available", label: i18n.extensions_state_running },
	disabled: { token: "paused", label: i18n.extensions_state_disabled },
	needs_approval: { token: "wanted", label: i18n.extensions_state_needs_approval },
	failed: { token: "failed", label: i18n.extensions_state_failed },
	starting: { token: "importing", label: i18n.extensions_state_starting },
};

export const AUTH_LABEL: Record<MountAuth, () => string> = {
	self: i18n.extensions_auth_self,
	session: i18n.extensions_auth_session,
};

export const SERVICE_META: Record<ExtensionService, { label: () => string; help: () => string }> = {
	serve_file: { label: i18n.extensions_service_serve_file, help: i18n.extensions_service_serve_file_help },
	serve_poster: { label: i18n.extensions_service_serve_poster, help: i18n.extensions_service_serve_poster_help },
	ffmpeg: { label: i18n.extensions_service_ffmpeg, help: i18n.extensions_service_ffmpeg_help },
	library_scan: { label: i18n.extensions_service_library_scan, help: i18n.extensions_service_library_scan_help },
};

export const NO_PERMISSIONS: ExtensionPermissions = { mounts: [], views: [], services: [], folders: [] };

// What the list shows as the extension's permissions: what the module asks
// for, or, when it could not be read, what the entry still holds.
export const shownPermissions = (x: Extension) => x.requested ?? x.granted;

export const isEmpty = (p: ExtensionPermissions) =>
	p.mounts.length === 0 && p.views.length === 0 && p.services.length === 0 && p.folders.length === 0;

export const grantsOf = (p: ExtensionPermissions): ExtensionGrants => ({
	mounts: p.mounts.map((m) => m.path),
	views: p.views.map((v) => v.name),
	services: [...p.services],
	folders: [...p.folders],
});

// The approval's three parts: what the module asks for that the entry does
// not hold, what it holds already, and what it holds but no longer asks for
// (approving drops those: the grant becomes exactly what the module asks).
export function diffPermissions(requested: ExtensionPermissions, granted: ExtensionPermissions) {
	const has = grantsOf(granted);
	const asks = grantsOf(requested);
	return {
		added: {
			mounts: requested.mounts.filter((m) => !has.mounts.includes(m.path)),
			views: requested.views.filter((v) => !has.views.includes(v.name)),
			services: requested.services.filter((s) => !has.services.includes(s)),
			folders: requested.folders.filter((f) => !has.folders.includes(f)),
		},
		kept: {
			mounts: requested.mounts.filter((m) => has.mounts.includes(m.path)),
			views: requested.views.filter((v) => has.views.includes(v.name)),
			services: requested.services.filter((s) => has.services.includes(s)),
			folders: requested.folders.filter((f) => has.folders.includes(f)),
		},
		dropped: {
			mounts: granted.mounts.filter((m) => !asks.mounts.includes(m.path)),
			views: granted.views.filter((v) => !asks.views.includes(v.name)),
			services: granted.services.filter((s) => !asks.services.includes(s)),
			folders: granted.folders.filter((f) => !asks.folders.includes(f)),
		},
	} satisfies Record<string, ExtensionPermissions>;
}

// "1 address · 8 kinds of library data · 2 services", each part only when
// there is some.
export function permissionSummary(p: ExtensionPermissions): string {
	const parts: string[] = [];
	const n = (count: number, one: (a: { count: number }) => string, other: (a: { count: number }) => string) =>
		(count === 1 ? one : other)({ count });
	if (p.mounts.length) parts.push(n(p.mounts.length, i18n.extensions_n_addresses_one, i18n.extensions_n_addresses_other));
	if (p.views.length) parts.push(n(p.views.length, i18n.extensions_n_views_one, i18n.extensions_n_views_other));
	if (p.services.length) parts.push(n(p.services.length, i18n.extensions_n_services_one, i18n.extensions_n_services_other));
	return parts.length ? parts.join(" · ") : i18n.extensions_permissions_none();
}

// Client-side checks, so an obviously wrong source is caught before a round
// trip. The server decides everything else (and says so verbatim).
const SHA256 = /^[0-9a-f]{64}$/i;
export const isUrlSource = (s: string) => /^https?:\/\//i.test(s.trim());

export function sourceProblem(source: string): string | null {
	const s = source.trim();
	if (!s) return i18n.extensions_source_required();
	if (/^http:\/\//i.test(s)) return i18n.extensions_source_http();
	if (!/^https:\/\/[^\s/]+\/\S+$/i.test(s) && !/^\/\S+$/.test(s)) return i18n.extensions_source_invalid();
	return null;
}

export function shaProblem(sha: string, source: string): string | null {
	const v = sha.trim();
	if (!v) return isUrlSource(source) ? i18n.extensions_sha256_required() : null;
	if (!SHA256.test(v)) return i18n.extensions_sha256_invalid();
	return null;
}

// A long hex reads as noise; the start and end are what anyone compares.
export const shortSha = (sha: string) => (sha.length > 16 ? `${sha.slice(0, 8)}…${sha.slice(-8)}` : sha);

// The entry as it goes under `extensions:` in config.yaml, for a read-only
// instance: the form still runs, and ends on this to paste.
export function extensionYaml(entry: { name: string; source: string; sha256?: string; enabled: boolean; grants: ExtensionGrants }): string {
	const list = (xs: string[]) => `[${xs.join(", ")}]`;
	const lines = [
		"extensions:",
		`  - name: ${entry.name}`,
		`    source: ${entry.source.trim()}`,
		...(entry.sha256?.trim() ? [`    sha256: ${entry.sha256.trim().toLowerCase()}`] : []),
		`    enabled: ${entry.enabled}`,
		"    grants:",
		`      mounts: ${list(entry.grants.mounts)}`,
		`      views: ${list(entry.grants.views)}`,
		`      services: ${list(entry.grants.services)}`,
		`      folders: ${list(entry.grants.folders)}`,
	];
	return lines.join("\n");
}
