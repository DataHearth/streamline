// Every in-app address the SPA builds, in one place.
//
// Pages used to write their own links — `/movies/${id}`, `/music/${a}?release=${b}`,
// `/torrents?hash=${h}` — in a dozen resolvers and twice as many components, and
// each copy decided for itself what a title's page was and which query opened
// what on it. A route that moved, or a detail page that learned a new deep link
// (?season=, ?download=), had to be found in every one of them. Here the
// patterns are Routify's own (`routes/movies/[id].svelte` is `/movies/[id]`),
// `href()` fills them, and `link` is the one vocabulary for "the page of this
// thing": what a library title, a season, an episode, an album, a download
// record or a torrent opens on. `web/routes_test.go` checks every pattern
// against the route files, so a renamed route fails the build, not a click.

/** Routify route patterns, keyed by what they show. */
export const ROUTE = {
	home: "/",
	search: "/search",
	movies: "/movies",
	movie: "/movies/[id]",
	shows: "/series",
	show: "/series/[id]",
	music: "/music",
	artist: "/music/[id]",
	books: "/books",
	book: "/books/[id]",
	bookSeries: "/books/series/[id]",
	person: "/people/[id]",
	activity: "/activity",
	torrents: "/torrents",
	transcoding: "/transcoding",
	calendar: "/calendar",
	requests: "/requests",
	stats: "/stats",
	imports: "/imports",
	importScan: "/imports/[id]",
	settings: "/settings",
	account: "/account",
} as const;

export type RoutePattern = (typeof ROUTE)[keyof typeof ROUTE];

// `/books/series/[id]` → "id". A pattern with no bracket takes no params.
type ParamNames<P extends string> = P extends `${string}[${infer K}]${infer R}`
	? K | ParamNames<R>
	: never;

export type QueryValue = string | number | boolean | null | undefined;
export type Query = Record<string, QueryValue>;

type HrefArgs<P extends string> = [ParamNames<P>] extends [never]
	? [params?: Record<string, never>, query?: Query]
	: [params: Record<ParamNames<P>, string | number>, query?: Query];

/**
 * A concrete path for a route pattern: its params filled in, and the query
 * appended with every null, undefined, false or empty value left out (an
 * absent key is how every page reads "not set").
 */
export function href<P extends RoutePattern>(pattern: P, ...[params, query]: HrefArgs<P>): string {
	const path = pattern.replace(/\[([^\]]+)\]/g, (_, key: string) => {
		const v = (params as Record<string, string | number> | undefined)?.[key];
		if (v === undefined || v === "") throw new Error(`href: ${pattern} needs ${key}`);
		return encodeURIComponent(String(v));
	});
	const qs = queryString(query);
	return qs ? `${path}?${qs}` : path;
}

function queryString(query: Query | undefined): string {
	if (!query) return "";
	const p = new URLSearchParams();
	for (const [k, v] of Object.entries(query)) {
		if (v === null || v === undefined || v === false || v === "") continue;
		p.set(k, v === true ? "1" : String(v));
	}
	return p.toString();
}

/**
 * The arguments Routify's `goto` takes for the same address: it resolves route
 * PATTERNS, not concrete paths (`goto("/movies/1")` fails with "could not
 * travel to 1"), and reads the path params and the query from one record.
 */
export function gotoArgs<P extends RoutePattern>(
	pattern: P,
	...[params, query]: HrefArgs<P>
): [P, Record<string, string>] {
	const out: Record<string, string> = {};
	for (const [k, v] of Object.entries(params ?? {})) out[k] = String(v);
	for (const [k, v] of Object.entries(query ?? {})) {
		if (v === null || v === undefined || v === false || v === "") continue;
		out[k] = v === true ? "1" : String(v);
	}
	return [pattern, out];
}

// ── What each page opens on ─────────────────────────────────────────────────

export type MovieTab = "overview" | "history" | "cast";
export type ShowTab = "overview" | "episodes" | "history" | "cast";

export const link = {
	movie: (id: number, tab?: MovieTab) => href(ROUTE.movie, { id }, { tab: tab === "overview" ? undefined : tab }),
	show: (id: number, tab?: ShowTab) => href(ROUTE.show, { id }, { tab: tab === "overview" ? undefined : tab }),
	/** The show's episodes tab with that season selected. */
	season: (seriesId: number, season: number) => href(ROUTE.show, { id: seriesId }, { tab: "episodes", season }),
	/**
	 * The episode's sheet open, on its season when it is known (the page finds
	 * the season itself otherwise).
	 */
	episode: (seriesId: number, episodeId: number, season?: number) =>
		href(ROUTE.show, { id: seriesId }, { tab: "episodes", season, episode: episodeId }),
	artist: (id: number) => href(ROUTE.artist, { id }),
	/** An album lives on its artist's page: the discography with it open. */
	album: (artistId: number, albumId: number) => href(ROUTE.artist, { id: artistId }, { release: albumId }),
	book: (id: number) => href(ROUTE.book, { id }),
	bookSeries: (id: number) => href(ROUTE.bookSeries, { id }),
	person: (id: number) => href(ROUTE.person, { id }),
	/**
	 * A download record on Activity, its detail open: the queue while it is in
	 * flight, the history once it is done — the page finds which.
	 */
	download: (id: number) => href(ROUTE.activity, {}, { download: id }),
	/** The builtin engine's torrent drawer. */
	torrent: (hash: string) => href(ROUTE.torrents, {}, { hash: hash.toLowerCase() }),
	activityView: (view: "queue" | "history" | "events") =>
		href(ROUTE.activity, {}, { view: view === "queue" ? undefined : view }),
	importScan: (id: number) => href(ROUTE.importScan, { id }),
} as const;

/**
 * A library title, or a part of one, as the API names it in a queue row, an
 * event, a calendar entry or a torrent. `targetHref` is the only place that
 * turns one into an address, so every surface opens the same thing on the same
 * page: an episode on its show with its sheet open, an album on its artist
 * with the release selected, a volume on its own page.
 */
export type MediaTarget =
	| { kind: "movie"; id: number }
	| { kind: "show"; id: number }
	| { kind: "season"; seriesId: number; season: number }
	| { kind: "episode"; seriesId: number; season?: number; episodeId?: number }
	| { kind: "artist"; id: number }
	| { kind: "album"; id: number; artistId: number }
	| { kind: "book"; id: number }
	| { kind: "bookSeries"; id: number };

export function targetHref(t: MediaTarget | undefined | null): string | undefined {
	if (!t) return undefined;
	switch (t.kind) {
		case "movie":
			return t.id ? link.movie(t.id) : undefined;
		case "show":
			return t.id ? link.show(t.id) : undefined;
		case "season":
			return t.seriesId ? link.season(t.seriesId, t.season) : undefined;
		case "episode":
			if (!t.seriesId) return undefined;
			if (t.episodeId) return link.episode(t.seriesId, t.episodeId, t.season);
			// Without its id an episode still has a season to land on.
			return t.season === undefined ? link.show(t.seriesId, "episodes") : link.season(t.seriesId, t.season);
		case "artist":
			return t.id ? link.artist(t.id) : undefined;
		case "album":
			return t.artistId ? link.album(t.artistId, t.id) : undefined;
		case "book":
			return t.id ? link.book(t.id) : undefined;
		case "bookSeries":
			return t.id ? link.bookSeries(t.id) : undefined;
	}
}
