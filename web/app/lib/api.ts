import { m as i18n } from "./paraglide/messages.js";

const BASE = "/api/v1";

export type ApiErrorBody = { message?: string; code?: string } | null;

export class ApiError extends Error {
	status: number;
	body: ApiErrorBody;
	// Seconds to wait, from a 429's Retry-After. Absent when the header is.
	retryAfter?: number;
	// The response had no body at all. Streamline never answers an error that
	// way, so this one came from something in front of it.
	bare = false;
	constructor(status: number, message: string, body: ApiErrorBody, retryAfter?: number) {
		super(message);
		this.status = status;
		this.body = body;
		this.retryAfter = retryAfter;
	}
}

// Retry-After is delta-seconds or an HTTP date; either becomes seconds from now.
function retryAfterOf(res: Response): number | undefined {
	const v = res.headers.get("Retry-After")?.trim();
	if (!v) return undefined;
	const s = /^\d+$/.test(v) ? Number(v) : Math.ceil((Date.parse(v) - Date.now()) / 1000);
	return Number.isFinite(s) && s > 0 ? s : undefined;
}
// `rate_limited` is an upstream metadata provider (MusicBrainz, Hardcover);
// a login or API-key lockout is `too_many_attempts` or a bare 429.
const waitText = (s: number, provider: boolean) => {
	const minutes = Math.ceil(s / 60);
	if (provider)
		return s < 60 ? i18n.err_provider_rate_limited_seconds({ seconds: s }) : i18n.err_provider_rate_limited_minutes({ minutes });
	return s < 60 ? i18n.err_rate_limited_seconds({ seconds: s }) : i18n.err_rate_limited_minutes({ minutes });
};

export type ApiOptions = {
	method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
	body?: unknown;
	headers?: Record<string, string>;
};

// The API answers in English by design — `message` stays readable for curl,
// logs and non-browser clients. The SPA therefore never renders it: it resolves
// the stable `code` first, falls back to the HTTP status, and only then to the
// caller's action-specific text. Many handlers still pass raw service errors
// through `message`, so showing it would leak internal Go strings as well as
// English.
const BY_CODE: Record<string, () => string> = {
	invalid_credentials: i18n.err_invalid_credentials,
	invite_invalid: i18n.err_invite_invalid,
	invite_required: i18n.err_invite_required,
	email_required: i18n.err_email_required,
	weak_password: i18n.err_weak_password,
	password_mismatch: i18n.err_password_mismatch,
	registration_disabled: i18n.err_registration_disabled,
	register_failed: i18n.err_register_failed,
	too_many_attempts: i18n.err_rate_limited,
	rate_limited: i18n.err_provider_rate_limited,
	no_quality_profile: i18n.err_no_quality_profile,
	smtp_not_configured: i18n.account_device_unconfigured,
	series_volume: i18n.err_series_volume,
	bad_request: i18n.err_bad_request,
	body_too_large: i18n.err_body_too_large,
	// Hardcover, on any book call: search, lookup, add, refresh. Where there is
	// room to say who fixes the key, the caller checks hardcoverIssue() first.
	hardcover_key_rejected: i18n.books_lookup_key_rejected,
	hardcover_not_configured: i18n.err_hardcover_not_configured,
	// A release's file list, read before grabbing it.
	files_unavailable: i18n.release_files_magnet,
	torrent_unreadable: i18n.release_files_failed,
	// A media server, download client or indexer moved to another address with
	// its secret left blank: the stored one is never sent to the new address.
	secret_required: i18n.err_secret_required,
	// A 403 an admin gets for a secret the config file owns, not for a missing
	// permission.
	secret_file_managed: i18n.err_secret_file_managed,
	// Creating an API key past the per-account cap: the fix is revoking one,
	// not anything in the form.
	api_key_quota: i18n.err_api_key_quota,
};

function byStatus(status: number): string {
	switch (status) {
		case 400:
			return i18n.err_bad_request();
		case 401:
			return i18n.err_unauthorized();
		case 403:
			return i18n.err_forbidden();
		case 404:
			return i18n.err_not_found();
		case 409:
			return i18n.err_conflict();
		// A fronting reverse proxy can cap the body first and answer 413 with no
		// JSON body, so there is no code to key on.
		case 413:
			return i18n.err_body_too_large();
		case 422:
			return i18n.err_unprocessable();
		case 429:
			return i18n.err_rate_limited();
		case 503:
		case 504:
			return i18n.err_unavailable();
		default:
			return status >= 500 ? i18n.err_server() : i18n.err_generic();
	}
}

// errorText turns any thrown value into a translated, user-safe sentence.
// `fallback` describes the attempted action (e.g. "Update failed") and is used
// only for non-HTTP failures, where there is no status to explain the cause.
export function errorText(err: unknown, fallback?: string): string {
	if (err instanceof ApiError) {
		const code = err.body?.code;
		// A connection test's message is a diagnostic the server composed
		// ("plex: unexpected status 401"), not a leaked driver string, and it is
		// the only place the upstream status appears. Rendering it as a plain 422
		// ("some of those values weren't accepted") blames credentials that are
		// usually fine and hides the real cause.
		if (code === "connection_failed" && err.message) return err.message;
		// Same deal for a custom-format condition that would not compile: the
		// form has no client-side regex gate, so this message — a condition
		// index plus the regexp diagnostic, all derived from what was typed —
		// is the only thing that says which condition is wrong and why.
		if (code === "invalid_condition" && err.message) return err.message;
		// And for a grab the server refused: an untrusted download host, or a
		// release whose files match no wanted episode. The generic 422 blames
		// the request body, which sent an operator hunting through releases for
		// a reason this message had already named.
		if (code === "grab_rejected" && err.message) return err.message;
		// A send's refusal names the reason: the file's format, the SMTP server's
		// own reply. The generic 422 or 502 would hide it.
		if ((code === "send_failed" || code === "not_sendable" || code === "no_device_email") && err.message) return err.message;
		// A 429 that says how long to wait: the sentence says it too (a login,
		// a Hardcover add or approve).
		if (err.status === 429 && err.retryAfter) return waitText(err.retryAfter, code === "rate_limited");
		// Every 403 streamline writes carries a body. A bare one is a reverse
		// proxy or WAF refusing the request before it arrived — typically a
		// method allow-list without PATCH or DELETE — and "you don't have
		// permission" sent an admin looking at their role instead.
		if (err.status === 403 && err.bare) return i18n.err_forbidden_upstream();
		if (code && BY_CODE[code]) return BY_CODE[code]();
		return byStatus(err.status);
	}
	// fetch() rejects with a TypeError when the request never reached the server.
	if (err instanceof TypeError) return i18n.err_offline();
	return fallback ?? i18n.err_generic();
}

// readBody reads a response that *should* be JSON but, on error paths, may be
// plaintext (chi / oapi-codegen default error handlers, reverse proxies). It
// NEVER throws — a non-JSON body yields parsed=null while keeping the raw text
// so callers can still surface a meaningful message instead of a SyntaxError.
async function readBody(
	res: Response,
): Promise<{ parsed: unknown; text: string }> {
	const text = await res.text();
	if (!text) return { parsed: null, text: "" };
	try {
		return { parsed: JSON.parse(text), text };
	} catch {
		return { parsed: null, text };
	}
}

function errorFrom(res: Response, parsed: unknown, text: string): ApiError {
	const body =
		parsed && typeof parsed === "object" ? (parsed as ApiErrorBody) : null;
	const message =
		body?.message || (text ? text.trim().slice(0, 300) : "") || res.statusText;
	const err = new ApiError(res.status, message, body, res.status === 429 ? retryAfterOf(res) : undefined);
	err.bare = !text;
	return err;
}

export async function api<T = unknown>(
	path: string,
	{ method = "GET", body, headers }: ApiOptions = {},
): Promise<T> {
	const res = await fetch(BASE + path, {
		method,
		credentials: "same-origin",
		headers: {
			Accept: "application/json",
			...(body !== undefined ? { "Content-Type": "application/json" } : {}),
			...headers,
		},
		body: body !== undefined ? JSON.stringify(body) : undefined,
	});

	if (res.status === 401) {
		if (location.pathname === "/login" || location.pathname === "/register") {
			throw new ApiError(401, res.statusText, null);
		}
		const next = encodeURIComponent(location.pathname + location.search);
		location.href = `/login?next=${next}`;
		return new Promise<T>(() => {});
	}

	if (res.status === 204) return null as T;

	const { parsed, text } = await readBody(res);
	if (!res.ok) throw errorFrom(res, parsed, text);
	return parsed as T;
}

// authFetch hits the non-/api/v1 auth endpoints (`/auth/login`,
// `/auth/register`, `/auth/logout`, `/auth/config`, `/auth/invite/:token`).
// Unlike `api`, it never auto-redirects on 401 — login/register pages need to
// surface the credential error inline instead of redirecting back to themselves.
export async function authFetch<T = unknown>(
	path: string,
	{ method = "GET", body, headers }: ApiOptions = {},
): Promise<T> {
	const res = await fetch(path, {
		method,
		credentials: "same-origin",
		headers: {
			Accept: "application/json",
			...(body !== undefined ? { "Content-Type": "application/json" } : {}),
			...headers,
		},
		body: body !== undefined ? JSON.stringify(body) : undefined,
	});

	if (res.status === 204) return null as T;

	const { parsed, text } = await readBody(res);
	if (!res.ok) throw errorFrom(res, parsed, text);
	return parsed as T;
}

// PAGE_LIMIT is the largest page every paginated list endpoint will serve; the
// spec declares `maximum: 100` on each limit param and the handlers answer 400
// past it, so a caller that wants the whole set has to walk pages against
// `total`.
export const PAGE_LIMIT = 100;

// Paginated is the aggregate apiAllPages returns: every item plus the server-
// reported total. It deliberately drops the envelope's page/limit, which mean
// nothing once the pages are merged.
export type Paginated<T> = { items: T[]; total: number };

// apiAllPages walks a paginated endpoint until it has collected `total` items.
// Pages that count, filter, search and sort client-side need the whole set;
// with a single oversized request they silently showed only the first 100 rows.
// `path` may already carry a query string — page and limit are appended.
export async function apiAllPages<T>(path: string): Promise<Paginated<T>> {
	const sep = path.includes("?") ? "&" : "?";
	const fetchPage = (page: number) =>
		api<Paginated<T>>(`${path}${sep}page=${page}&limit=${PAGE_LIMIT}`);

	const first = await fetchPage(1);
	const total = first.total ?? first.items.length;
	const pages = Math.ceil(total / PAGE_LIMIT);
	if (pages <= 1) return { items: first.items, total };

	// Pages 2..n in parallel: they are independent reads, and a library of a few
	// thousand rows is a handful of requests rather than a serial chain.
	const rest = await Promise.all(
		Array.from({ length: pages - 1 }, (_, i) => fetchPage(i + 2)),
	);
	return { items: [...first.items, ...rest.flatMap((r) => r.items)], total };
}
