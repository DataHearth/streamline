// Activity nav model, shared by Sidebar and BottomNav.
//
// Queue and History are one page (the toolbar switches between them); torrents
// is its own route and carries live status pills in the nav.

import { createQuery } from "@tanstack/svelte-query";
import { api, ApiError } from "./api";
import { auth } from "./auth.svelte";
import { transcodingDisabled } from "./transcoding";
import { NAV_POLL_MS, SILENT } from "./query";
import type {
	DownloadClient,
	TorrentList,
	TranscodeConfig,
	TranscodeJob,
} from "./types";

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

export type TranscodeCounts = {
	running: number;
	queued: number;
	failed: number;
};

// Which of the two admin Activity routes have anything behind them. The
// torrents page shows the builtin engine only, so with no enabled builtin
// client it is a permanent 404; transcoding is a permanent 409 while
// `transcoding.enabled` is off. Every nav surface and the search pages hide a
// route that is off — a link to a page that can only say "this is off" is
// clutter on an instance that chose not to run it. Both read as off until the
// config has answered, so a disabled row never flashes in and out at load.
// The keys are the settings pages' own, so a toggle there updates the nav in
// the same tick.
export function activityFeatures() {
	const clients = createQuery<DownloadClient[]>(() => ({
		queryKey: ["download-clients"],
		queryFn: () => api<DownloadClient[]>("/download-clients"),
		meta: SILENT,
		enabled: auth.isAdmin,
		retry: false,
		staleTime: 300000,
	}));
	const transcoding = createQuery<TranscodeConfig>(() => ({
		queryKey: ["config", "transcoding"],
		queryFn: () => api<TranscodeConfig>("/config/transcoding"),
		meta: SILENT,
		enabled: auth.isAdmin,
		retry: false,
		staleTime: 300000,
	}));
	return {
		get torrents(): boolean {
			return (
				auth.isAdmin &&
				!!clients.data?.some((c) => c.client_type === "builtin" && c.enabled)
			);
		},
		get transcoding(): boolean {
			return auth.isAdmin && transcoding.data?.enabled === true;
		},
	};
}

// The builtin engine's torrent list under the torrents page's own key, polled
// only while a builtin client is configured. The nav counts read it, and the
// activity page reads it to know which queue rows still have a torrent to open.
export function builtinTorrentsQuery() {
	// The engine is a download-clients entry; with it off, /torrents only ever
	// 404s. Shares the settings sidebar's cached list rather than probing.
	const features = activityFeatures();
	// Same key as the torrents page, so there is never a second request in
	// flight — but the interval below is not the whole story: refetchInterval
	// lives on the cache entry, so whichever observer is mounted sets the rate.
	// On the torrents page that is its own 2 s; everywhere else, this 15 s.
	return createQuery<TorrentList>(() => ({
		queryKey: ["activity", "torrents"],
		queryFn: () => api<TorrentList>("/torrents"),
		meta: SILENT,
		enabled: features.torrents,
		retry: false,
		refetchInterval: (q) =>
			engineDisabled(q.state.error) ? false : NAV_POLL_MS,
	}));
}

// Where a download record's torrent opens, or null when the builtin engine
// does not list it: the torrents page shows that engine only, so a record
// held by another client, or one whose torrent is gone, has nowhere to go.
export function torrentHref(
	hash: string | undefined,
	listed: ReadonlySet<string>,
): string | null {
	if (!hash) return null;
	const h = hash.toLowerCase();
	return listed.has(h) ? `/torrents?hash=${encodeURIComponent(h)}` : null;
}

export function torrentCountsQuery() {
	const q = builtinTorrentsQuery();
	return {
		get counts(): TorrentCounts {
			const out: TorrentCounts = {};
			for (const t of q.data?.items ?? [])
				out[t.status] = (out[t.status] ?? 0) + 1;
			return out;
		},
	};
}

// The queue route's own query polls every 2 s while something is live; away
// from it this is the nav's cadence. Admin-only endpoint, and it 409s while
// transcoding is disabled — which no refetch can clear — so it is not asked at
// all until the config says transcoding is on, and a 409 that still lands
// (switched off since) stops the poll rather than retrying forever.
export function transcodeCountsQuery() {
	const features = activityFeatures();
	const q = createQuery<TranscodeJob[]>(() => ({
		queryKey: ["transcoding", "queue", ""],
		queryFn: () => api<TranscodeJob[]>("/transcoding/queue"),
		meta: SILENT,
		enabled: features.transcoding,
		retry: false,
		refetchInterval: (q) =>
			transcodingDisabled(q.state.error) ? false : NAV_POLL_MS,
	}));
	return {
		get counts(): TranscodeCounts {
			const out: TranscodeCounts = { running: 0, queued: 0, failed: 0 };
			for (const j of q.data ?? []) {
				if (j.status === "running") out.running++;
				else if (j.status === "queued") out.queued++;
				else if (j.status === "failed") out.failed++;
			}
			return out;
		},
	};
}

export type IsActiveFn = (
	path: string,
	params?: Record<string, string>,
	options?: { recursive?: boolean },
) => boolean;

// The routes the Activity group reaches. Torrents and transcoding are
// top-level routes rather than children of /activity, so a recursive
// isActive("/activity") no longer covers them — the group's own highlight has
// to name all three.
export const ACTIVITY_ROUTES = ["/activity", "/torrents", "/transcoding"];

export const activityGroupActive = (isActive: IsActiveFn) =>
	ACTIVITY_ROUTES.some((p) => isActive(p));

// Non-recursive resolves to the index node, pinning each link to its own route
// (`/activity?view=events` still counts as /activity).
export const activityCurrent = (isActive: IsActiveFn, href: string) =>
	isActive(href, {}, { recursive: false });
