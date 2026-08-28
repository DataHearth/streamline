// Activity nav model, shared by Sidebar and BottomNav.
//
// Queue and History are one page (the toolbar switches between them); torrents
// is its own route and carries live status pills in the nav.

import { createQuery } from "@tanstack/svelte-query";
import { api, ApiError } from "./api";
import { NAV_POLL_MS, SILENT } from "./query";
import type { DownloadClient, TorrentList } from "./types";

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
	// The engine is a download-clients entry; with it off, /torrents only ever
	// 404s. Shares the settings sidebar's cached list rather than probing.
	const clients = createQuery<DownloadClient[]>(() => ({
		queryKey: ["download-clients"],
		queryFn: () => api<DownloadClient[]>("/download-clients"),
		meta: SILENT,
		retry: false,
		staleTime: 300000,
	}));
	// Same key as the torrents page, so there is never a second request in
	// flight — but the interval below is not the whole story: refetchInterval
	// lives on the cache entry, so whichever observer is mounted sets the rate.
	// On the torrents page that is its own 2 s; everywhere else, this 15 s.
	const q = createQuery<TorrentList>(() => ({
		queryKey: ["activity", "torrents"],
		queryFn: () => api<TorrentList>("/torrents"),
		meta: SILENT,
		enabled: !!clients.data?.some(
			(c) => c.client_type === "builtin" && c.enabled,
		),
		retry: false,
		refetchInterval: (q) =>
			engineDisabled(q.state.error) ? false : NAV_POLL_MS,
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
