// Section counts for the nav — the second line under a destination in the
// mobile sheets, and the badge dots on the tablet rail.
//
// Every query reuses the key of the page that owns the data, so the nav rides
// an existing cache instead of adding a poll of its own. Lines omit the parts
// that are zero: a settled library should read as one clean fact, not as a row
// of noughts.

import { createQuery } from "@tanstack/svelte-query";
import { api } from "./api";
import { auth } from "./auth.svelte";
import type {
	DownloadQueue,
	ImportCounts,
	ImportScanList,
	MovieCounts,
	TVShowCounts,
} from "./types";

const n = (v: number) => v.toLocaleString();

// There is no counts endpoint for imports, so the queue is derived from the
// list. ListImports orders by create_time descending and caps limit at 100;
// running and awaiting_review are by definition the newest rows, so they are
// always inside the first page and the count is exact rather than sampled.
const IMPORT_SCAN_WINDOW = 100;

function countImports(list: ImportScanList): ImportCounts {
	let running = 0;
	let awaiting_review = 0;
	for (const s of list.items) {
		if (s.status === "running") running++;
		else if (s.status === "awaiting_review") awaiting_review++;
	}
	return { running, awaiting_review };
}

export function navCountsQuery() {
	const movies = createQuery<MovieCounts>(() => ({
		queryKey: ["movies", "counts"],
		queryFn: () => api<MovieCounts>("/movies/counts"),
		retry: false,
	}));
	const series = createQuery<TVShowCounts>(() => ({
		queryKey: ["series", "counts"],
		queryFn: () => api<TVShowCounts>("/series/counts"),
		retry: false,
	}));
	// The activity page polls this key every 2 s while it is mounted; away from
	// it, 30 s is plenty for a summary line.
	const queue = createQuery<DownloadQueue>(() => ({
		queryKey: ["activity", "queue"],
		queryFn: () => api<DownloadQueue>("/activity/queue"),
		retry: false,
		refetchInterval: 30000,
	}));
	const imports = createQuery<ImportCounts>(() => ({
		queryKey: ["imports", "counts"],
		queryFn: async () =>
			countImports(
				await api<ImportScanList>(
					`/library/imports?limit=${IMPORT_SCAN_WINDOW}`,
				),
			),
		enabled: auth.isAdmin,
		retry: false,
		refetchInterval: 60000,
	}));

	return {
		get imports(): ImportCounts | null {
			return imports.data ?? null;
		},
		get moviesTotal(): number | null {
			return movies.data?.total ?? null;
		},
		get seriesTotal(): number | null {
			return series.data?.total ?? null;
		},
		get moviesLine(): string {
			const d = movies.data;
			if (!d) return "";
			return `${n(d.total)} titles${d.wanted ? ` · ${n(d.wanted)} wanted` : ""}`;
		},
		get seriesLine(): string {
			const d = series.data;
			if (!d) return "";
			const wanted = d.wanted_episodes
				? ` · ${n(d.wanted_episodes)} episodes wanted`
				: "";
			return `${n(d.total)} shows${wanted}`;
		},
		get queueLine(): string {
			const items = queue.data?.items;
			if (!items) return "";
			if (!items.length) return "Nothing in the queue";
			const by: Record<string, number> = {};
			for (const i of items) by[i.status] = (by[i.status] ?? 0) + 1;
			return (["downloading", "importing", "paused", "error"] as const)
				.filter((s) => by[s])
				.map((s) => `${n(by[s])} ${s === "error" ? "failed" : s}`)
				.join(" · ");
		},
		get importsLine(): string {
			const d = imports.data;
			if (!d) return "";
			const parts: string[] = [];
			if (d.running) parts.push(`${n(d.running)} running`);
			if (d.awaiting_review)
				parts.push(`${n(d.awaiting_review)} awaiting review`);
			return parts.join(" · ") || "Nothing in flight";
		},
		// Rail badge: review first — it is the state that needs a person.
		get importsDot(): string | null {
			const d = imports.data;
			if (!d) return null;
			if (d.awaiting_review) return "wanted";
			if (d.running) return "downloading";
			return null;
		},
	};
}
