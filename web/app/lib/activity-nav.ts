// Activity nav model, shared by Sidebar and BottomNav.
//
// Queue and History are one page (the toolbar switches between them); torrents
// is its own route and carries live status pills in the nav.

import { createQuery } from "@tanstack/svelte-query";
import { api } from "./api";
import type { TorrentList } from "./types";

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
		refetchInterval: 15000,
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

export function activityCurrent(href: string): boolean {
	if (typeof window === "undefined") return false;
	return window.location.pathname === href.split("?")[0];
}
