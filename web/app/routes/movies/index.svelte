<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { formatRelative } from "../../lib/dates";
	import { formatBytes } from "../../lib/format";
	import { movieStatus } from "../../lib/status";
	import MoviesToolbar from "../../components/movies/MoviesToolbar.svelte";
	import MovieGrid from "../../components/movies/MovieGrid.svelte";
	import MovieList from "../../components/movies/MovieList.svelte";
	import MoviesEmpty from "../../components/movies/MoviesEmpty.svelte";
	import type {
		Movie,
		PaginatedMovies,
		ScheduleList,
	} from "../../lib/types";

	type View = "grid" | "list";
	type SortKey = "title" | "year";
	type SortOrder = "asc" | "desc";

	const VALID_TABS = new Set([
		"all",
		"available",
		"wanted",
		"downloading",
		"failed",
	]);

	function readParams() {
		const p =
			typeof window === "undefined"
				? new URLSearchParams()
				: new URLSearchParams(window.location.search);
		const rawTab = p.get("status") ?? "all";
		const rawSort = p.get("sort") ?? "title";
		const rawOrder = p.get("order") ?? "asc";
		const rawView = p.get("view") ?? "grid";
		return {
			tab: VALID_TABS.has(rawTab) ? rawTab : "all",
			query: p.get("q") ?? "",
			sort: (rawSort === "year" ? "year" : "title") as SortKey,
			order: (rawOrder === "desc" ? "desc" : "asc") as SortOrder,
			view: (rawView === "list" ? "list" : "grid") as View,
			monitoredOnly: p.get("monitored") === "1",
		};
	}

	const initial = readParams();
	let tab = $state(initial.tab);
	let query = $state(initial.query);
	let sort = $state<SortKey>(initial.sort);
	let order = $state<SortOrder>(initial.order);
	let view = $state<View>(initial.view);
	let monitoredOnly = $state(initial.monitoredOnly);

	function openAddMovie() {
		window.dispatchEvent(new CustomEvent("streamline:open-add-movie"));
	}

	$effect(() => {
		if (typeof window === "undefined") return;
		const p = new URLSearchParams();
		if (tab !== "all") p.set("status", tab);
		if (query) p.set("q", query);
		if (monitoredOnly) p.set("monitored", "1");
		if (sort !== "title") p.set("sort", sort);
		if (order !== "asc") p.set("order", order);
		if (view !== "grid") p.set("view", view);
		const search = p.toString();
		const next = `${window.location.pathname}${search ? `?${search}` : ""}`;
		if (next !== window.location.pathname + window.location.search) {
			window.history.replaceState(null, "", next);
		}
	});

	const moviesQuery = createQuery<PaginatedMovies>(() => ({
		queryKey: ["movies"],
		queryFn: () => api<PaginatedMovies>("/movies?page=1&limit=500"),
	}));

	const schedulesQuery = createQuery<ScheduleList>(() => ({
		queryKey: ["schedules"],
		queryFn: () => api<ScheduleList>("/schedules"),
	}));

	let allMovies = $derived(moviesQuery.data?.items ?? []);

	let counts = $derived.by(() => {
		const c = {
			total: allMovies.length,
			wanted: 0,
			downloading: 0,
			available: 0,
			failed: 0,
		};
		for (const m of allMovies) {
			const s = movieStatus(m);
			if (s === "wanted") c.wanted++;
			else if (s === "downloading") c.downloading++;
			else if (s === "available") c.available++;
			else if (s === "failed") c.failed++;
		}
		return c;
	});

	// Unmonitored fileless movies resolve to "missing", which has no tab — they
	// stay reachable under All rather than padding the Wanted queue.
	let monitoredCount = $derived(allMovies.filter((m) => m.monitored).length);

	function passesTab(m: Movie): boolean {
		if (tab === "all") return true;
		return movieStatus(m) === tab;
	}

	let visibleMovies = $derived.by(() => {
		let list = allMovies.filter(passesTab);
		if (monitoredOnly) list = list.filter((m) => m.monitored);
		const q = query.trim().toLowerCase();
		if (q)
			list = list.filter(
				(m) =>
					m.title.toLowerCase().includes(q) ||
					m.original_title.toLowerCase().includes(q),
			);
		const sorted = [...list];
		sorted.sort((a, b) => {
			let cmp: number;
			if (sort === "year") cmp = (a.year ?? 0) - (b.year ?? 0);
			else
				cmp = a.title.localeCompare(b.title, undefined, {
					sensitivity: "base",
				});
			return order === "asc" ? cmp : -cmp;
		});
		return sorted;
	});

	let libraryEmpty = $derived(
		tab === "all" && !query && !monitoredOnly && allMovies.length === 0,
	);

	let monitoredSize = $derived.by(() => {
		let total = 0;
		for (const m of allMovies)
			for (const f of m.media_files ?? []) total += f.size;
		return total;
	});

	let lastScan = $derived.by(() => {
		const items = schedulesQuery.data?.items ?? [];
		let mostRecent: string | null = null;
		for (const s of items) {
			if (!s.last_finished_at) continue;
			if (!mostRecent || s.last_finished_at > mostRecent)
				mostRecent = s.last_finished_at;
		}
		return mostRecent;
	});

	function clearFilters() {
		tab = "all";
		query = "";
		monitoredOnly = false;
	}
</script>

<div class="flex flex-col">
	{#if moviesQuery.isLoading}
		<div class="w-full px-4 py-16 text-center text-sm text-fg-subtle md:px-6">
			Loading movies…
		</div>
	{:else if moviesQuery.isError}
		<div class="w-full px-4 md:px-6">
			<div
				class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center"
			>
				<p class="text-sm font-semibold text-status-failed">
					Failed to load movies
				</p>
				<p class="mt-1 text-xs text-fg-subtle">
					{moviesQuery.error?.message ?? "Unknown error"}
				</p>
			</div>
		</div>
	{:else}
		<MoviesToolbar
			{tab}
			{query}
			{sort}
			{order}
			{view}
			{counts}
			{monitoredOnly}
			{monitoredCount}
			onTabChange={(t) => (tab = t)}
			onQueryChange={(q) => (query = q)}
			onMonitoredChange={(v) => (monitoredOnly = v)}
			onSortChange={(s, o) => {
				sort = s;
				order = o;
			}}
			onViewChange={(v) => (view = v)}
			onAddMovie={openAddMovie}
		/>

		<div
			class="flex w-full flex-wrap items-baseline justify-between gap-2 px-4 pt-4 pb-2 font-mono text-[11px] text-fg-subtle md:px-6"
		>
			<div>
				{visibleMovies.length} of {counts.total} titles
				{#if query}
					<span
						class="ml-2 inline-flex items-center gap-1 rounded-full bg-accent-soft px-2 py-0.5 text-accent-text"
					>
						“{query}”
						<button
							type="button"
							onclick={() => (query = "")}
							aria-label="Clear search"
							class="text-accent-text transition hover:text-fg"
						>
							×
						</button>
					</span>
				{/if}
			</div>
			<div class="flex flex-wrap items-center gap-2">
				<span>{formatBytes(monitoredSize, "0 B")} monitored</span>
				{#if lastScan}
					<span class="text-fg-faint">·</span>
					<span>last scan {formatRelative(lastScan)}</span>
				{/if}
			</div>
		</div>

		<div class="w-full px-4 pb-6 md:px-6">
			{#if visibleMovies.length === 0}
				<MoviesEmpty
					variant={libraryEmpty ? "library" : "filter"}
					onClear={clearFilters}
				/>
			{:else if view === "list"}
				<MovieList
					movies={visibleMovies}
					{sort}
					{order}
					onSortChange={(s, o) => {
						sort = s;
						order = o;
					}}
				/>
			{:else}
				<MovieGrid movies={visibleMovies} />
			{/if}
		</div>
	{/if}
</div>
