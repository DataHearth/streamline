import { m as i18n } from "./paraglide/messages.js";

const BASE = "/api/v1";

export type ApiErrorBody = { message?: string; code?: string } | null;

export class ApiError extends Error {
	status: number;
	body: ApiErrorBody;
	constructor(status: number, message: string, body: ApiErrorBody) {
		super(message);
		this.status = status;
		this.body = body;
	}
}

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
	rate_limited: i18n.err_rate_limited,
	bad_request: i18n.err_bad_request,
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
	return new ApiError(res.status, message, body);
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
// spec declares `maximum: 100` on each limit param and the handlers clamp to it
// silently. Asking for more returns 100 with no indication it was reduced, so a
// caller that wants the whole set has to walk pages against `total`.
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
