import { ApiError } from "./api";

// The server sends no `code` on a 503, so the message is the only way to tell
// a missing or rejected Hardcover key from any other outage.
export function hardcoverUnavailable(err: unknown): boolean {
	return (
		err instanceof ApiError &&
		err.status === 503 &&
		/hardcover/i.test(err.message)
	);
}
