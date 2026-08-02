// Activity nav model, shared by Sidebar and BottomNav.
//
// Queue and History are one page (the toolbar switches between them); torrents
// is its own route and carries live status pills in the nav.

import { createQuery } from "@tanstack/svelte-query";
import { api, ApiError } from "./api";
import type { TorrentList } from "./types";

// /torrents 404s while the built-in engine is off, and it can't come back
// without a config change. Refetching a query that has never held data resets
// it to `pending` (TanStack's fetchState), so polling a permanent 404 flips
// the UI between loading and empty forever — every refetch path checks this.
export const engineDisabled = (e: unknown) =>
	e instanceof ApiError && e.status === 404;

// The three states worth watching at a glance. Completed/paused torrents are
// not news, so they stay on the page's own filter row.
export const TORRENT_PILLS = [
	{ key: "downloading", short: "DL", dot: "downloading" },
	{ key: "seeding", short: "S", dot: "seeding" },
	{ key: "stalled", short: "!", dot: "stalled" },
] as const;

export type TorrentCounts = Record<string, number>;

export function torrentCountsQuery() {
	// Same key as the torrents page, so the nav rides its 2 s poll instead of
	// adding a second one.
	const q = createQuery<TorrentList>(() => ({
		queryKey: ["activity", "torrents"],
		queryFn: () => api<TorrentList>("/torrents"),
		retry: false,
		refetchInterval: (q) => (engineDisabled(q.state.error) ? false : 15000),
	}));
	return {
		get counts(): TorrentCounts {
			const out: TorrentCounts = {};
			for (const t of q.data?.items ?? [])
				out[t.status] = (out[t.status] ?? 0) + 1;
			return out;
		},
	};
}

export type IsActiveFn = (
	path: string,
	params?: Record<string, string>,
	options?: { recursive?: boolean },
) => boolean;

// Routify's isActive matches the whole chain, so "/activity" also reports
// active on /activity/torrents. Non-recursive resolves to the index node
// instead, pinning each link to its own route.
export const activityCurrent = (isActive: IsActiveFn, href: string) =>
	isActive(href, {}, { recursive: false });
