import { activeRoute } from "@roxi/routify";

// Routify mounts the incoming route before window.location catches up, so a page
// reading its own query string at mount sees the *previous* URL — a list page
// reached from a detail page's back link came up with default filters while the
// address bar showed the filtered ones. activeRoute carries the URL the page is
// being rendered for, so the filters come off that instead. Only the first
// emission naming this path applies; later ones are the page's own write-back.
export function onRouteQuery(
	path: string,
	apply: (p: URLSearchParams) => void,
) {
	let done = false;
	return activeRoute.subscribe((r) => {
		if (done || !r) return;
		const [routePath, search] = (r.url ?? "").split("?");
		if (routePath !== path) return;
		done = true;
		apply(new URLSearchParams(search ?? ""));
	});
}

/**
 * Every emission naming `path`, as a plain record — for a page that is linked
 * to from itself (Activity's event feed opens a download on the same route,
 * which onRouteQuery's first-emission-only rule would drop). It reads
 * activeRoute rather than the `params` store because a write needs the raw
 * query string as its base, and `params` merges the path's params into the
 * query's. Every replaceRouteQuery write is itself a Routify navigation
 * (its patched `replaceState` routes the URL), so the page's own writes come
 * back here too: the caller decides what is new, comparing against what it
 * last applied, and hands `search` back to replaceRouteQuery as the query to
 * write over.
 */
export function onEachRouteQuery(
	path: string,
	apply: (p: Record<string, string>, search: string) => void,
) {
	return onEachRouteUrl((routePath, search) => {
		if (routePath !== path) return;
		apply(Object.fromEntries(new URLSearchParams(search)), search);
	});
}

/**
 * Every route emission as its path and raw query — for a page whose own path
 * moves with a param (/series/[id]), which cannot name one path up front. The
 * caller keeps both and uses the query as replaceRouteQuery's base only while
 * the path is still its own.
 */
export function onEachRouteUrl(apply: (path: string, search: string) => void) {
	return activeRoute.subscribe((r) => {
		if (!r) return;
		const [routePath, rest] = (r.url ?? "").split("?");
		apply(routePath ?? "", (rest ?? "").split("#")[0] ?? "");
	});
}

/**
 * Rewrites the current URL's query in place: `mutate` edits a copy of it,
 * and an empty value (null, undefined, "") removes the key, so a page at its
 * defaults stays a bare path. It writes only while the address bar is on
 * `path` — at mount it still names the page being left, and stamping this
 * page's state onto that one broke back links — and it keeps the `#hash`.
 * It passes `null` as the history state on purpose: Routify patches
 * `history.replaceState`, and a state carrying its own `routify` record
 * (`history.state`, as issue #134 proposed keeping) is taken for a call
 * aimed at another router and dropped, so the write never lands. The write half of the deep-link convention; `onRouteQuery` and
 * `onEachRouteQuery` are the read half.
 *
 * Returns the query it wrote (undefined when it wrote nothing), which the
 * caller must use as the base of its next write: Routify applies the write
 * asynchronously, so until the route re-emits, the base it handed in is the
 * query from before this write, and a second write in the same tick built on
 * it silently undid the first (a view switch closing a row lost ?view).
 */
export function replaceRouteQuery(
	path: string,
	mutate: (p: URLSearchParams) => Record<string, string | number | null | undefined> | void,
	// The query to write over: the route's, from onEachRouteQuery. A link to
	// the page from itself updates the route before window.location, and a
	// write built on the address bar then put back the URL being left.
	base?: string,
): string | undefined {
	if (typeof window === "undefined" || window.location.pathname !== path) return undefined;
	const p = new URLSearchParams(base ?? window.location.search);
	const set = mutate(p);
	for (const [k, v] of Object.entries(set ?? {})) {
		if (v === null || v === undefined || v === "") p.delete(k);
		else p.set(k, String(v));
	}
	const qs = p.toString();
	// Nothing to change on the route's own query: Routify reflects it to the
	// address bar itself. Writing it first turned a link's pending pushState
	// into a replaceState (the write is a navigation of its own, and the one
	// reflect that follows takes the last one's mode), so Back skipped the
	// page state the link was clicked on.
	if (base !== undefined && qs === base) return qs;
	const next = `${path}${qs ? `?${qs}` : ""}${window.location.hash}`;
	if (next !== window.location.pathname + window.location.search + window.location.hash) {
		window.history.replaceState(null, "", next);
	}
	return qs;
}
