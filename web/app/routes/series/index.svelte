<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { formatRelative } from "../../lib/dates";
	import SeriesToolbar from "../../components/series/SeriesToolbar.svelte";
	import type {
		SeriesTab,
		SeriesTypeFilter,
		SeriesSort,
		SeriesTabCounts,
	} from "../../components/series/SeriesToolbar.svelte";
	import SeriesGrid from "../../components/series/SeriesGrid.svelte";
	import SeriesList from "../../components/series/SeriesList.svelte";
	import SeriesEmpty from "../../components/series/SeriesEmpty.svelte";
	import type { PaginatedTVShows, ScheduleList, TVShow } from "../../lib/types";

	type View = "grid" | "list";

	const VALID_TABS = new Set<SeriesTab>([
		"all",
		"continuing",
		"ended",
		"upcoming",
		"missing",
	]);
	const VALID_TYPES = new Set<SeriesTypeFilter>([
		"all",
		"standard",
		"anime",
		"daily",
	]);
	const VALID_SORTS = new Set<SeriesSort>([
		"recent",
		"title",
		"year",
		"rating",
		"episodes",
	]);

	function readParams() {
		const p =
			typeof window === "undefined"
				? new URLSearchParams()
				: new URLSearchParams(window.location.search);
		const rawTab = (p.get("status") ?? "all") as SeriesTab;
		const rawType = (p.get("type") ?? "all") as SeriesTypeFilter;
		const rawSort = (p.get("sort") ?? "recent") as SeriesSort;
		const rawView = p.get("view") ?? "grid";
		return {
			tab: VALID_TABS.has(rawTab) ? rawTab : "all",
			typeFilter: VALID_TYPES.has(rawType) ? rawType : "all",
			query: p.get("q") ?? "",
			sort: VALID_SORTS.has(rawSort) ? rawSort : "recent",
			view: (rawView === "list" ? "list" : "grid") as View,
		};
	}

	const initial = readParams();
	let tab = $state<SeriesTab>(initial.tab);
	let typeFilter = $state<SeriesTypeFilter>(initial.typeFilter);
	let query = $state(initial.query);
	let sort = $state<SeriesSort>(initial.sort);
	let view = $state<View>(initial.view);

	function openAddSeries() {
		window.dispatchEvent(new CustomEvent("streamline:open-add-series"));
	}

	$effect(() => {
		if (typeof window === "undefined") return;
		const p = new URLSearchParams();
		if (tab !== "all") p.set("status", tab);
		if (typeFilter !== "all") p.set("type", typeFilter);
		if (query) p.set("q", query);
		if (sort !== "recent") p.set("sort", sort);
		if (view !== "grid") p.set("view", view);
		const search = p.toString();
		const next = `${window.location.pathname}${search ? `?${search}` : ""}`;
		if (next !== window.location.pathname + window.location.search) {
			window.history.replaceState(null, "", next);
		}
	});

	const seriesQuery = createQuery<PaginatedTVShows>(() => ({
		queryKey: ["series"],
		queryFn: () => api<PaginatedTVShows>("/series?page=1&limit=500"),
	}));

	const schedulesQuery = createQuery<ScheduleList>(() => ({
		queryKey: ["schedules"],
		queryFn: () => api<ScheduleList>("/schedules"),
	}));

	let allSeries = $derived(seriesQuery.data?.items ?? []);

	let counts = $derived.by<SeriesTabCounts>(() => {
		const c: SeriesTabCounts = {
			all: allSeries.length,
			continuing: 0,
			ended: 0,
			upcoming: 0,
			missing: 0,
		};
		for (const s of allSeries) {
			if (s.series_status === "continuing") c.continuing++;
			else if (s.series_status === "ended") c.ended++;
			else if (s.series_status === "upcoming") c.upcoming++;
			if ((s.wanted_episodes ?? 0) > 0) c.missing++;
		}
		return c;
	});

	function passesTab(s: TVShow): boolean {
		if (tab === "all") return true;
		if (tab === "missing") return (s.wanted_episodes ?? 0) > 0;
		return s.series_status === tab;
	}

	let visibleSeries = $derived.by(() => {
		let list = allSeries.filter(passesTab);
		if (typeFilter !== "all") list = list.filter((s) => s.type === typeFilter);
		const q = query.trim().toLowerCase();
		if (q)
			list = list.filter(
				(s) =>
					s.title.toLowerCase().includes(q) ||
					(s.network ?? "").toLowerCase().includes(q) ||
					(s.genres ?? []).join(" ").toLowerCase().includes(q),
			);
		const sorted = [...list];
		sorted.sort((a, b) => {
			switch (sort) {
				case "title":
					return a.title.localeCompare(b.title, undefined, {
						sensitivity: "base",
					});
				case "year":
					return (b.year ?? 0) - (a.year ?? 0);
				case "rating":
					return (b.rating ?? 0) - (a.rating ?? 0);
				case "episodes":
					return (b.total_episodes ?? 0) - (a.total_episodes ?? 0);
				default:
					// "recent": no added-date is exposed, so id descending is the
					// closest proxy for most-recently-added.
					return b.id - a.id;
			}
		});
		return sorted;
	});

	let totalEpisodes = $derived(
		allSeries.reduce((sum, s) => sum + (s.have_episodes ?? 0), 0),
	);

	let libraryEmpty = $derived(
		tab === "all" && typeFilter === "all" && !query && allSeries.length === 0,
	);

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
		typeFilter = "all";
		query = "";
	}
</script>

<div class="flex flex-col">
	{#if seriesQuery.isLoading}
		<div class="w-full px-4 py-16 text-center text-sm text-fg-subtle md:px-6">
			Loading series…
		</div>
	{:else if seriesQuery.isError}
		<div class="w-full px-4 md:px-6">
			<div
				class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center"
			>
				<p class="text-sm font-semibold text-status-failed">
					Failed to load series
				</p>
				<p class="mt-1 text-xs text-fg-subtle">
					{seriesQuery.error?.message ?? "Unknown error"}
				</p>
			</div>
		</div>
	{:else}
		<SeriesToolbar
			{tab}
			{typeFilter}
			{query}
			{sort}
			{view}
			{counts}
			onTabChange={(t) => (tab = t)}
			onTypeChange={(t) => (typeFilter = t)}
			onQueryChange={(q) => (query = q)}
			onSortChange={(s) => (sort = s)}
			onViewChange={(v) => (view = v)}
			onAddSeries={openAddSeries}
		/>

		<div
			class="flex w-full flex-wrap items-baseline justify-between gap-2 px-4 pt-4 pb-2 font-mono text-[11px] text-fg-subtle md:px-6"
		>
			<div>
				{visibleSeries.length} of {counts.all} series
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
				<span>{totalEpisodes.toLocaleString()} episodes</span>
				{#if lastScan}
					<span class="text-fg-faint">·</span>
					<span>last scan {formatRelative(lastScan)}</span>
				{/if}
			</div>
		</div>

		<div class="w-full px-4 pb-12 md:px-6">
			{#if visibleSeries.length === 0}
				<SeriesEmpty
					variant={libraryEmpty ? "library" : "filter"}
					onClear={clearFilters}
				/>
			{:else if view === "list"}
				<SeriesList series={visibleSeries} />
			{:else}
				<SeriesGrid series={visibleSeries} />
			{/if}
		</div>
	{/if}
</div>
