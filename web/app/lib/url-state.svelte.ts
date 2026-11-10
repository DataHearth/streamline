import { untrack } from "svelte";
import { activeRoute } from "@roxi/routify";

// A page's filters, search and sort live in its query string, so a reload, a
// shared link or Back reopens the same view (#134). One helper does it for
// every page, with one vocabulary:
//
//   q          free-text search
//   status     the status facet (a comma list where the chips multi-select)
//   monitored  all | monitored | unmonitored
//   kind       which family of media (books' shelves, quality profiles,
//              requests); series `type` is another axis (standard/anime/daily)
//   sort+order the sort key and its direction, always both
//   view       a layout or sub-view switch (grid/list, queue/history/events…)
//   tab        a detail page's tab
//
// Paging never goes in the URL. A value equal to the page's default is left
// out, so a page with nothing picked is its bare path.
//
// Call it while the component initialises: it registers two effects.
//
// `apply` gets the query the page is rendered for (and its path) and sets the
// page's state from it. It runs once at mount, then again on Back/Forward and on a link to
// the same path while the page stays mounted. It must accept anything — a
// hand-edited or stale URL falls back to the defaults through `oneOf` and
// `listOf` — and read a renamed parameter under its old name too.
//
// `serialize` returns the page's state as a query. It is the effect's only
// tracked read, and the URL is rewritten whenever it changes. `onSync`, when
// given, gets each query the page settles on while it is the page on screen
// (the library lists remember it for a detail page's back link).
//
// `path` is the page's path, or a pattern for a page with one path per title
// (`/^\/movies\/\d+$/`).
export function syncRouteQuery(
	path: string | RegExp,
	apply: (p: URLSearchParams, path: string) => void,
	serialize: () => URLSearchParams,
	onSync?: (search: string) => void,
) {
	const matches = (p: string) => (typeof path === "string" ? p === path : path.test(p));
	// The path the route was last rendered for while it was this page's, null
	// before the first and once the route moves elsewhere. Nothing is written
	// before `apply` has seeded the page from its own URL, or the defaults the
	// page starts with would replace the filters a link carries.
	let active = $state<string | null>(null);

	$effect(() =>
		untrack(() =>
			activeRoute.subscribe((r) => {
				if (!r) return;
				const url = new URL(r.url ?? "", "http://x");
				if (!matches(url.pathname)) {
					active = null;
					return;
				}
				// Routify mounts the incoming route before window.location catches up,
				// so a page reading its own query at mount sees the *previous* URL;
				// activeRoute carries the one the page is rendered for. After that, a
				// replaceState on the same path is the page's own write-back coming
				// round again — applying it could revert a keystroke typed since —
				// while a popState (Back/Forward) or a pushState (a link to this same
				// page, or to another title on a detail page) is a view the page has
				// to show.
				const echo = active === url.pathname && r.mode === "replaceState";
				active = url.pathname;
				if (!echo) apply(url.searchParams, url.pathname);
			}),
		),
	);

	$effect(() => {
		const search = serialize().toString();
		if (active === null || typeof window === "undefined") return;
		// A navigation into this page — or from one title to another — still has
		// the outgoing URL in the address bar; writing now would stamp this page's
		// state onto that route's path and cancel the navigation.
		const { pathname, hash } = window.location;
		if (pathname !== active) return;
		onSync?.(search);
		const next = `${pathname}${search ? `?${search}` : ""}${hash}`;
		if (next === pathname + window.location.search + hash) return;
		// The state must be null. Routify patches history.replaceState, routes the
		// call through its router and writes its own state; data carrying a
		// `routify` key — a copy of history.state — makes it ignore the call, and
		// the address bar would silently stop following the page.
		window.history.replaceState(null, "", next);
	});
}

// The value if it is one of `allowed`, else the fallback.
export function oneOf<T extends string>(
	value: string | null | undefined,
	allowed: readonly T[],
	fallback: T,
): T {
	return allowed.includes(value as T) ? (value as T) : fallback;
}

// A comma list keeping only the values in `allowed`, each once, in URL order.
export function listOf<T extends string>(
	value: string | null | undefined,
	allowed: readonly T[],
): T[] {
	const out: T[] = [];
	for (const v of (value ?? "").split(",")) {
		const t = v.trim() as T;
		if (allowed.includes(t) && !out.includes(t)) out.push(t);
	}
	return out;
}

// The first of `names` the query carries: the current name, then the old
// ones, so a link from before a rename still opens the view it named.
export function param(p: URLSearchParams, ...names: string[]): string | null {
	for (const n of names) {
		const v = p.get(n);
		if (v !== null) return v;
	}
	return null;
}

// Sets `key` unless the value is its default (or empty). A list is written as
// a comma list, and an empty one is the default.
export function setParam(
	p: URLSearchParams,
	key: string,
	value: string | readonly string[],
	fallback = "",
) {
	const v = typeof value === "string" ? value : value.join(",");
	if (v && v !== fallback) p.set(key, v);
}
