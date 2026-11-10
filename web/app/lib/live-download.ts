// Which of the queue's downloads a title's page, or a part of one, is waiting
// on, and the Activity address that leads to it.
//
// The queue names what each download is for by id: an episode record its
// anchor and every episode it covers (`episode_ids`), a discography pack the
// albums it covers (`album_ids`), a book its slot. A part of a page is matched
// on those ids, never on the release name, so a season pack shows on each of
// its episodes and an episode's upgrade on that episode alone.

import { createQuery } from "@tanstack/svelte-query";
import { api } from "./api";
import { SILENT } from "./query";
import { link } from "./routes";
import { downloadMatches } from "./title-downloads";
import type { DownloadQueue, QueueEntry } from "./types";

/** The parts of a title page that wait on a download. */
export type DownloadScope =
	| { kind: "movie"; id: number }
	// episodeIds: the season's own episodes, which a pack anchored on another
	// season still covers.
	| { kind: "season"; seriesId: number; season: number; episodeIds: number[] }
	| { kind: "episode"; id: number }
	| { kind: "album"; id: number }
	// slot: one of the book's two formats, each its own download.
	| { kind: "book"; id: number; slot?: "ebook" | "audiobook" };

/** The statuses a movie or an episode reads while a download is behind it. */
export const IN_FLIGHT: ReadonlySet<string> = new Set(["downloading", "importing", "paused"]);

export function waitsOn(q: QueueEntry, scope: DownloadScope): boolean {
	switch (scope.kind) {
		case "movie":
			// Every other record carries an empty placeholder movie (id 0).
			return !!q.movie?.id && q.movie.id === scope.id;
		case "season":
			return (
				downloadMatches(q, scope) ||
				(q.episode?.series_id === scope.seriesId &&
					(q.episode_ids ?? []).some((id) => scope.episodeIds.includes(id)))
			);
		case "episode":
			return q.episode?.episode_id === scope.id || (q.episode_ids ?? []).includes(scope.id);
		case "album":
			return q.album?.id === scope.id || (q.artist?.album_ids ?? []).includes(scope.id);
		case "book":
			return q.book?.id === scope.id && (!scope.slot || !q.book.slot || q.book.slot === scope.slot);
	}
}

/**
 * The Activity address of the first download the queue lists for the scope,
 * or undefined: the queue lists the one furthest along first.
 */
export function downloadHref(items: QueueEntry[] | undefined, scope: DownloadScope): string | undefined {
	const q = (items ?? []).find((q) => waitsOn(q, scope));
	return q ? link.download(q.id) : undefined;
}

/**
 * Where a season waiting on downloads links: the one download itself, or the
 * queue filtered on the season when there are several (grabbed episode by
 * episode, or a pack beside an upgrade). Undefined with none.
 */
export function seasonDownloads(
	items: QueueEntry[] | undefined,
	scope: Extract<DownloadScope, { kind: "season" }>,
): { href: string; count: number } | undefined {
	const matches = (items ?? []).filter((q) => waitsOn(q, scope));
	const [first] = matches;
	if (!first) return undefined;
	if (matches.length === 1) return { href: link.download(first.id), count: 1 };
	return {
		href: link.downloads({ kind: "season", seriesId: scope.seriesId, season: scope.season }),
		count: matches.length,
	};
}

/**
 * The queue, under Activity's own key so a detail page and Activity share one
 * cache entry, asked only while `active`: a title at rest has nothing on the
 * queue. `items` is undefined while inactive, whatever the cache holds: the
 * nav and Activity feed the same entry, and a queue they left behind would
 * link a download that is over. Call it while the component initialises.
 */
export function liveQueue(active: () => boolean) {
	const queue = createQuery<DownloadQueue>(() => ({
		queryKey: ["activity", "queue"],
		queryFn: () => api<DownloadQueue>("/activity/queue"),
		meta: SILENT,
		retry: false,
		enabled: active(),
		refetchInterval: 5000,
	}));
	return {
		get items(): QueueEntry[] | undefined {
			return active() ? queue.data?.items : undefined;
		},
	};
}

/**
 * The Activity address of the download one part of a page is waiting on, or
 * undefined. For a part with a query of its own (a movie's status, an open
 * episode sheet, a book format); a list shares one `liveQueue`.
 */
export function liveDownload(scope: () => DownloadScope | null, active: () => boolean) {
	const queue = liveQueue(active);
	return {
		get href(): string | undefined {
			const s = scope();
			return s ? downloadHref(queue.items, s) : undefined;
		},
	};
}
