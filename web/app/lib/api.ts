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
