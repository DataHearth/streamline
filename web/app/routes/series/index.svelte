<script lang="ts">
	import { untrack } from "svelte";
	import { onMount } from "svelte";
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { formatRelative } from "../../lib/dates";
	import { loadPref, savePref } from "../../lib/prefs";
	import { fold } from "../../lib/text";
	import { pageMeta } from "../../lib/page-meta.svelte";
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
	import SeriesBulkActions from "../../components/series/SeriesBulkActions.svelte";
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

	// An explicit ?sort in the URL wins so shared links keep their ordering;
	// otherwise fall back to the last sort this browser chose, then A→Z.
	const SORT_PREF = "streamline:series:sort";

	function readParams() {
		const p =
			typeof window === "undefined"
				? new URLSearchParams()
				: new URLSearchParams(window.location.search);
		const rawTab = (p.get("status") ?? "all") as SeriesTab;
		const rawType = (p.get("type") ?? "all") as SeriesTypeFilter;
		const rawSort = (p.get("sort") ??
			loadPref(SORT_PREF) ??
			"title") as SeriesSort;
		const rawView = p.get("view") ?? "grid";
		return {
			tab: VALID_TABS.has(rawTab) ? rawTab : "all",
			typeFilter: VALID_TYPES.has(rawType) ? rawType : "all",
			query: p.get("q") ?? "",
			sort: VALID_SORTS.has(rawSort) ? rawSort : "title",
			view: (rawView === "list" ? "list" : "grid") as View,
			monitoredOnly: p.get("monitored") === "1",
		};
	}

	const initial = readParams();
	let tab = $state<SeriesTab>(initial.tab);
	let typeFilter = $state<SeriesTypeFilter>(initial.typeFilter);
	let query = $state(initial.query);
	let sort = $state<SeriesSort>(initial.sort);
	let view = $state<View>(initial.view);
	let monitoredOnly = $state(initial.monitoredOnly);
	// A phone has no width for the list table, so below md the library is posters
	// only — whatever the URL or the last-used view says.
	let narrow = $state(false);
	onMount(() => {
		const mql = window.matchMedia("(max-width: 767px)");
		const sync = () => (narrow = mql.matches);
		sync();
		mql.addEventListener("change", sync);
		return () => mql.removeEventListener("change", sync);
	});
	let shownView = $derived<View>(narrow ? "grid" : view);

	function setSort(s: SeriesSort) {
		sort = s;
		savePref(SORT_PREF, s);
	}

	function openAddSeries() {
		window.dispatchEvent(new CustomEvent("streamline:open-add-series"));
	}

	$effect(() => {
		// Every reactive read happens before the early returns below: a run that
		// bails out first would register no dependencies and the effect would
		// never fire again.
		const p = new URLSearchParams();
		if (tab !== "all") p.set("status", tab);
		if (typeFilter !== "all") p.set("type", typeFilter);
		if (query) p.set("q", query);
		if (monitoredOnly) p.set("monitored", "1");
		if (sort !== "title") p.set("sort", sort);
		if (view !== "grid") p.set("view", view);
		const search = p.toString();

		if (typeof window === "undefined") return;
		// Routify mounts the incoming route before it updates window.location, so
		// on a navigation *into* this page the first flush still sees the outgoing
		// URL. Writing then would stamp this page's (default, empty) filters onto
		// the previous route's path and cancel the navigation — which is exactly
		// what a detail page's back link hit: /series/1?tab=episodes became
		// /series/1 and never reached the list.
		if (window.location.pathname !== "/series") return;

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

	let monitoredCount = $derived(allSeries.filter((s) => s.monitored).length);

	function passesTab(s: TVShow): boolean {
		if (tab === "all") return true;
		if (tab === "missing") return (s.wanted_episodes ?? 0) > 0;
		return s.series_status === tab;
	}

	let visibleSeries = $derived.by(() => {
		let list = allSeries.filter(passesTab);
		if (typeFilter !== "all") list = list.filter((s) => s.type === typeFilter);
		if (monitoredOnly) list = list.filter((s) => s.monitored);
		const q = fold(query.trim());
		if (q)
			list = list.filter(
				(s) =>
					fold(s.title).includes(q) ||
					fold(s.network ?? "").includes(q) ||
					fold((s.genres ?? []).join(" ")).includes(q),
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
	let libraryEpisodes = $derived(
		allSeries.reduce((sum, s) => sum + (s.total_episodes ?? 0), 0),
	);

	let libraryEmpty = $derived(
		tab === "all" &&
			typeFilter === "all" &&
			!query &&
			!monitoredOnly &&
			allSeries.length === 0,
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
		monitoredOnly = false;
	}

	// Below md the topbar carries this under the title and the page's own count
	// line stands down; at md and up nothing changes.
	let metaLine = $derived.by(() => {
		const parts = [`${counts.all} shows`];
		if (libraryEpisodes > 0)
			parts.push(
				`${totalEpisodes.toLocaleString()} of ${libraryEpisodes.toLocaleString()} episodes`,
			);
		if (lastScan) parts.push(`scan ${formatRelative(lastScan)}`);
		return parts.join(" · ");
	});

	$effect(() => {
		pageMeta.set(metaLine);
		return () => pageMeta.clear();
	});

	// ── Selection ────────────────────────────────────────────────────────────
	let selected = $state(new Set<number>());
	// Grid cards only reveal their checkbox on hover, which leaves touch users
	// with no way in — the toolbar toggle pins them open instead.
	let selectMode = $state(false);

	function setSelectMode(v: boolean) {
		selectMode = v;
		if (!v) clearSelection();
	}
	function selectAll() {
		selectMode = true;
		toggleAll(true);
	}
	// Press and hold a poster: selection on, that card selected.
	function beginLongPress(id: number) {
		selectMode = true;
		toggle(id, true);
	}

	function toggle(id: number, v: boolean) {
		const next = new Set(selected);
		if (v) next.add(id);
		else next.delete(id);
		selected = next;
	}
	function toggleAll(v: boolean) {
		if (!v) return clearSelection();
		selected = new Set(visibleSeries.map((s) => s.id));
	}
	function clearSelection() {
		if (selected.size > 0) selected = new Set();
	}

	$effect(() => {
		tab;
		typeFilter;
		query;
		monitoredOnly;
		untrack(clearSelection);
	});
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
			view={shownView}
			{counts}
			{monitoredOnly}
			{monitoredCount}
			{selectMode}
			selectedCount={selected.size}
			visibleCount={visibleSeries.length}
			onTabChange={(t) => (tab = t)}
			onTypeChange={(t) => (typeFilter = t)}
			onQueryChange={(q) => (query = q)}
			onMonitoredChange={(v) => (monitoredOnly = v)}
			onSortChange={setSort}
			onViewChange={(v) => (view = v)}
			onClearFilters={clearFilters}
			onSelectModeChange={setSelectMode}
			onSelectAll={selectAll}
			onAddSeries={openAddSeries}
		/>

		<div
			class="hidden w-full flex-wrap items-baseline justify-between gap-2 px-4 pt-4 pb-2 font-mono text-[11px] text-fg-subtle md:flex md:px-6"
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

		<div class="w-full px-4 pb-6 pt-3 md:px-6 md:pt-0">
			{#if visibleSeries.length === 0}
				<SeriesEmpty
					variant={libraryEmpty ? "library" : "filter"}
					onClear={clearFilters}
				/>
			{:else if shownView === "list"}
				<SeriesList
					series={visibleSeries}
					{selected}
					onToggle={toggle}
					onToggleAll={toggleAll}
				/>
			{:else}
				<SeriesGrid
					series={visibleSeries}
					{selected}
					{selectMode}
					onToggle={toggle}
					onLongPress={beginLongPress}
				/>
			{/if}
		</div>

		<SeriesBulkActions
			series={visibleSeries}
			{selected}
			total={visibleSeries.length}
			onSelectAll={() => toggleAll(true)}
			onClear={clearSelection}
		/>
	{/if}
</div>
