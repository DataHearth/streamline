import { createMutation, useQueryClient, type QueryKey } from "@tanstack/svelte-query";
import { errorText } from "./api";
import { toast } from "./toast";
import { m as i18n } from "./paraglide/messages.js";

// The query families a write to a title touches: its list, counts and detail
// pages. A write re-reads the whole family, so a monitor toggle on a detail
// page also moves the list it came from.
export const MOVIES: readonly QueryKey[] = [["movies"], ["movie"]];
export const SERIES: readonly QueryKey[] = [["series"]];
export const MUSIC: readonly QueryKey[] = [["music"]];
export const BOOKS: readonly QueryKey[] = [["books"]];

type ActionOptions<V, D> = {
	run: (vars: V) => Promise<D>;
	// Re-read after the write settles, whether it succeeded or not: a refused
	// toggle must snap back to what the server kept rather than keep showing
	// the state the user asked for.
	refresh: readonly QueryKey[];
	// The action's wording, used when the failure carries nothing better
	// (errorText prefers the error code, then the HTTP status).
	failed: () => string;
	// A second line under the error, saying who fixes it and where (a
	// rejected Hardcover key, say).
	detail?: (e: Error) => string | undefined;
	// A confirmation toast, when the change is not visible on its own.
	succeeded?: (vars: V, data: D) => string | undefined;
	// Anything else on success: closing a dialog, navigating away.
	then?: (vars: V, data: D) => void;
};

// createAction is the one write path for a library title, whatever its media
// type: every monitor toggle, edit, search and delete reports a failure the
// same way. It used to be written per page — movies and series through
// mutations with an error toast, music and books with a bare await — and a
// refused request on the latter did nothing at all.
//
// Call it at component init, like createMutation; `isPending` drives
// MonitorToggle's `pending`.
export function createAction<V = void, D = unknown>(options: () => ActionOptions<V, D>) {
	const qc = useQueryClient();
	return createMutation<D, Error, V>(() => {
		const o = options();
		return {
			mutationFn: o.run,
			onSuccess: (data, vars) => {
				const msg = o.succeeded?.(vars, data);
				if (msg) toast.ok(msg);
				o.then?.(vars, data);
			},
			onError: (e) => {
				const description = o.detail?.(e);
				toast.err(errorText(e, o.failed()), description ? { description } : undefined);
			},
			onSettled: () => {
				for (const queryKey of o.refresh) qc.invalidateQueries({ queryKey });
			},
		};
	});
}

// The toast for a title's monitor flag moving: the same two sentences for
// every media type.
export const monitorToast = (next: boolean) => (next ? i18n.monitor_now_monitoring() : i18n.monitor_stopped());

export const updateFailed = () => i18n.common_update_failed();
export const searchFailed = () => i18n.common_search_failed();
export const deleteFailed = () => i18n.common_delete_failed();
export const refreshFailed = () => i18n.common_refresh_failed();
