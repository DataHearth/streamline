// Which of the queue's downloads a detail page — or a part of one — is waiting
// on. The queue names each download's title the way Activity titles its row:
// an episode record its anchor and every episode it links (`episode_ids`), a
// discography pack its artist and the albums it covers (`album_ids`), a volume
// its series. A season or an episode is matched on those links, not on the
// release name, so a season pack shows on each of its episodes and on its
// season, and an episode's upgrade shows on that episode alone.

import { createQuery } from "@tanstack/svelte-query";
import { api } from "./api";
import { SILENT } from "./query";
import { href, link, ROUTE } from "./routes";
import type { DownloadQueue, QueueEntry } from "./types";

/**
 * The look of a link to a download wherever it is a word rather than a pill:
 * "Downloading ›" in a details table, "Download ›" at the end of a history
 * row. One class string, so every such link reads the same.
 */
export const DOWNLOAD_CHIP =
	"group/dl touch-hit inline-flex cursor-pointer items-center whitespace-nowrap rounded-full font-sans border border-accent-line bg-accent-soft px-2 py-[2px] text-[10.5px] font-semibold text-accent-text transition hover:border-accent hover:text-fg";

/**
 * The chip's "›", shown on hover and keyboard focus only — at rest the chip
 * is the word, and the arrow says "this goes somewhere" when the pointer asks.
 * A touch screen has no hover, so there it always shows. Wraps the icon; the
 * chip (or the link around it) must carry `group/dl`.
 */
export const DOWNLOAD_CHIP_ARROW =
	"-mr-1 inline-flex w-0 justify-end overflow-hidden opacity-0 transition-[width,opacity] duration-150 group-hover/dl:w-3.5 group-hover/dl:opacity-100 group-focus-visible/dl:w-3.5 group-focus-visible/dl:opacity-100 pointer-coarse:w-3.5 pointer-coarse:opacity-100";

export type DownloadScope =
	| { kind: "movie"; id: number }
	| { kind: "show"; id: number }
	// episodeIds: the season's episodes, which a pack filed under another
	// season's first episode still links.
	| { kind: "season"; seriesId: number; season: number; episodeIds?: number[] }
	| { kind: "episode"; seriesId: number; id: number }
	| { kind: "artist"; id: number }
	| { kind: "album"; id: number }
	// slot: one of the book's two formats, each its own download.
	| { kind: "book"; id: number; slot?: "ebook" | "audiobook" }
	| { kind: "bookSeries"; id: number };

/**
 * What a match reads off a download: a queue entry, or a history entry, which
 * carries the same refs without the episode links.
 */
export type DownloadRefs = Pick<QueueEntry, "movie" | "episode" | "episode_ids" | "album" | "artist" | "book">;

export function downloadMatches(q: DownloadRefs, scope: DownloadScope): boolean {
	switch (scope.kind) {
		case "movie":
			// Every other record carries an empty placeholder movie (id 0).
			return !!q.movie?.id && q.movie.id === scope.id;
		case "show":
			return q.episode?.series_id === scope.id;
		case "season": {
			const ep = q.episode;
			if (!ep || ep.series_id !== scope.seriesId) return false;
			if (ep.season === scope.season) return true;
			const ids = new Set(scope.episodeIds ?? []);
			return (q.episode_ids ?? []).some((id) => ids.has(id));
		}
		case "episode": {
			const ep = q.episode;
			if (!ep || ep.series_id !== scope.seriesId) return false;
			return ep.episode_id === scope.id || (q.episode_ids ?? []).includes(scope.id);
		}
		case "artist":
			return q.album?.artist_id === scope.id || q.artist?.id === scope.id;
		case "album":
			return q.album?.id === scope.id || (q.artist?.album_ids ?? []).includes(scope.id);
		case "book":
			return q.book?.id === scope.id && (!scope.slot || !q.book.slot || q.book.slot === scope.slot);
		case "bookSeries":
			return q.book?.series?.id === scope.id;
	}
}

export function downloadsFor(items: QueueEntry[] | undefined, scope: DownloadScope): QueueEntry[] {
	return (items ?? []).filter((q) => downloadMatches(q, scope));
}

/**
 * The Activity address of the first download the queue lists for the scope —
 * the one furthest along — or undefined.
 */
export function firstDownloadHref(items: QueueEntry[] | undefined, scope: DownloadScope): string | undefined {
	const q = (items ?? []).find((q) => downloadMatches(q, scope));
	return q ? link.download(q.id) : undefined;
}

/**
 * The queue under Activity's own key, so a detail page and Activity share one
 * cache entry. Asked only while the page shows something in flight: a title
 * at rest has nothing on the queue, and every detail page polling it would be
 * a request every few seconds for nothing.
 */
export function titleQueueQuery(enabled: () => boolean) {
	return createQuery<DownloadQueue>(() => ({
		queryKey: ["activity", "queue"],
		queryFn: () => api<DownloadQueue>("/activity/queue"),
		meta: SILENT,
		enabled: enabled(),
		refetchInterval: 5000,
	}));
}

/**
 * The Activity address of the download a part of a page is waiting on — the
 * first the queue lists for the scope, the one furthest along — or undefined.
 * Call it while the component initialises: it creates the query.
 */
export function liveDownload(scope: () => DownloadScope | null, active: () => boolean) {
	const queue = titleQueueQuery(active);
	return {
		get href(): string | undefined {
			const s = scope();
			if (!s || !active()) return undefined;
			return firstDownloadHref(queue.data?.items, s);
		},
	};
}

// ── Activity filtered on a title ─────────────────────────────────────────────
// When a part of a page waits on several downloads — a season grabbed episode
// by episode, or a pack beside an upgrade — one link cannot open them all, so
// it opens the queue filtered on that part instead. The filter is the URL's,
// matched by downloadMatches over the queue the page already polls.

/** The scopes Activity filters on: the parts a page can wait on several of. */
export type DownloadFilter = Extract<DownloadScope, { kind: "show" | "season" | "artist" | "bookSeries" }>;

export const FILTER_KEYS = ["series", "season", "artist", "book_series"] as const;

/** `/activity?series=1&season=2` and the like. */
export function downloadsHref(f: DownloadFilter): string {
	switch (f.kind) {
		case "show":
			return href(ROUTE.activity, {}, { series: f.id });
		case "season":
			return href(ROUTE.activity, {}, { series: f.seriesId, season: f.season });
		case "artist":
			return href(ROUTE.activity, {}, { artist: f.id });
		case "bookSeries":
			return href(ROUTE.activity, {}, { book_series: f.id });
	}
}

/** The filter a query names, or null. */
export function downloadFilterFrom(p: Record<string, string | undefined>): DownloadFilter | null {
	const id = (k: string) => {
		const n = Number(p[k]);
		return Number.isInteger(n) && n > 0 ? n : 0;
	};
	if (id("series")) {
		const season = Number(p.season);
		return p.season !== undefined && p.season !== "" && Number.isInteger(season) && season >= 0
			? { kind: "season", seriesId: id("series"), season }
			: { kind: "show", id: id("series") };
	}
	if (id("artist")) return { kind: "artist", id: id("artist") };
	if (id("book_series")) return { kind: "bookSeries", id: id("book_series") };
	return null;
}

/**
 * Where a part of a page waiting on downloads links: the one download itself,
 * or the queue filtered on the part when there are several. Undefined with
 * none.
 */
export function downloadsLink(matches: QueueEntry[], f: DownloadFilter): { href: string; count: number } | undefined {
	const [first] = matches;
	if (!first) return undefined;
	if (matches.length === 1) return { href: link.download(first.id), count: 1 };
	return { href: downloadsHref(f), count: matches.length };
}
