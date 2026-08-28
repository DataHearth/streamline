<script lang="ts">
	import { untrack } from "svelte";
	import { onMount } from "svelte";
	import { createInfiniteQuery, createQuery } from "@tanstack/svelte-query";
	import { api, errorText, type Paginated } from "../../lib/api";
	import { formatRelative } from "../../lib/dates";
	import { loadPref, savePref } from "../../lib/prefs";
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
	import type { ScheduleList, TVShow, TVShowCounts } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

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
		};
	}

	const initial = readParams();
	let tab = $state<SeriesTab>(initial.tab);
	let typeFilter = $state<SeriesTypeFilter>(initial.typeFilter);
	let query = $state(initial.query);
	let sort = $state<SeriesSort>(initial.sort);
	let view = $state<View>(initial.view);
	// debouncedQuery is what the server query keys on. Typing must not fire a
	// request per keystroke now that filtering is server-side.
	let debouncedQuery = $state(initial.query);
	$effect(() => {
		const q = query;
		const t = setTimeout(() => (debouncedQuery = q), 250);
		return () => clearTimeout(t);
	});
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

	// Filtering, sorting and paging are the server's — db.FilterTVShows has
	// pushed all three into SQL since the list-performance work; the page just
	// never used the params. It pulled the whole library and redid the work in
	// the browser on every stale refetch.
	const PAGE = 50;
	const seriesQuery = createInfiniteQuery<
		Paginated<TVShow>,
		Error,
		{ pages: Paginated<TVShow>[]; pageParams: number[] },
		readonly ["series", SeriesTab, SeriesTypeFilter, string, SeriesSort],
		number
	>(() => ({
		queryKey: ["series", tab, typeFilter, debouncedQuery, sort] as const,
		queryFn: ({ pageParam }) => {
			const p = new URLSearchParams({
				page: String(pageParam),
				limit: String(PAGE),
				sort,
			});
			if (tab !== "all") p.set("status", tab);
			if (typeFilter !== "all") p.set("type", typeFilter);
			if (debouncedQuery.trim()) p.set("query", debouncedQuery.trim());
			return api<Paginated<TVShow>>(`/series?${p.toString()}`);
		},
		initialPageParam: 1,
		getNextPageParam: (last, pages) =>
			pages.flatMap((p) => p.items).length < last.total
				? pages.length + 1
				: undefined,
	}));

	const countsQuery = createQuery<TVShowCounts>(() => ({
		queryKey: ["series", "counts"],
		queryFn: () => api<TVShowCounts>("/series/counts"),
	}));

	const schedulesQuery = createQuery<ScheduleList>(() => ({
		queryKey: ["schedules"],
		queryFn: () => api<ScheduleList>("/schedules"),
	}));

	let counts = $derived<SeriesTabCounts>({
		all: countsQuery.data?.total ?? 0,
		continuing: countsQuery.data?.continuing ?? 0,
		ended: countsQuery.data?.ended ?? 0,
		upcoming: countsQuery.data?.upcoming ?? 0,
		missing: countsQuery.data?.missing ?? 0,
	});

	// Everything loaded so far. Bulk selection and the "N of M" line read this,
	// so both mean "of what you have pulled in", not "of the whole library".
	let visibleSeries = $derived(
		(seriesQuery.data?.pages ?? []).flatMap((p) => p.items),
	);
	let matchedTotal = $derived(seriesQuery.data?.pages?.[0]?.total ?? 0);

	let totalEpisodes = $derived(
		visibleSeries.reduce((sum, s) => sum + (s.have_episodes ?? 0), 0),
	);
	let libraryEpisodes = $derived(
		visibleSeries.reduce((sum, s) => sum + (s.total_episodes ?? 0), 0),
	);

	let libraryEmpty = $derived(
		tab === "all" &&
			typeFilter === "all" &&
			!debouncedQuery &&
			counts.all === 0,
	);

	// Appending the next page as the sentinel comes into view replaces the old
	// IncrementalList: a page is 50 cards, which mounts without blocking, and
	// there is no longer a whole library sitting in memory to slice.
	let pageSentinel = $state<HTMLDivElement | null>(null);
	$effect(() => {
		const el = pageSentinel;
		if (!el) return;
		const io = new IntersectionObserver(
			(entries) => {
				if (
					entries[0]?.isIntersecting &&
					seriesQuery.hasNextPage &&
					!seriesQuery.isFetchingNextPage
				) {
					seriesQuery.fetchNextPage();
				}
			},
			{ rootMargin: "600px" },
		);
		io.observe(el);
		return () => io.disconnect();
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
		typeFilter = "all";
		query = "";
	}

	// Below md the topbar carries this under the title and the page's own count
	// line stands down; at md and up nothing changes.
	let metaLine = $derived.by(() => {
		const parts = [i18n.series_shows_count({ count: counts.all })];
		if (libraryEpisodes > 0)
			parts.push(
				i18n.series_episodes_of({
					have: totalEpisodes.toLocaleString(),
					total: libraryEpisodes.toLocaleString(),
				}),
			);
		if (lastScan)
			parts.push(i18n.series_scan_meta({ when: formatRelative(lastScan) }));
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
		untrack(clearSelection);
	});
</script>

<div class="flex flex-col">
	{#if seriesQuery.isLoading}
		<div class="w-full px-4 py-16 text-center text-sm text-fg-subtle md:px-6">
			{i18n.common_loading_series()}
		</div>
	{:else if seriesQuery.isError}
		<div class="w-full px-4 md:px-6">
			<div
				class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center"
			>
				<p class="text-sm font-semibold text-status-failed">
					{i18n.series_load_failed()}
				</p>
				<p class="mt-1 text-xs text-fg-subtle">
					{errorText(seriesQuery.error, i18n.common_unknown_error())}
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
			{selectMode}
			selectedCount={selected.size}
			visibleCount={visibleSeries.length}
			onTabChange={(t) => (tab = t)}
			onTypeChange={(t) => (typeFilter = t)}
			onQueryChange={(q) => (query = q)}
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
				{i18n.series_count_of({
					visible: visibleSeries.length,
					total: matchedTotal,
				})}
				{#if query}
					<span
						class="ml-2 inline-flex items-center gap-1 rounded-full bg-accent-soft px-2 py-0.5 text-accent-text"
					>
						“{query}”
						<button
							type="button"
							onclick={() => (query = "")}
							aria-label={i18n.common_clear_search()}
							class="text-accent-text transition hover:text-fg"
						>
							×
						</button>
					</span>
				{/if}
			</div>
			<div class="flex flex-wrap items-center gap-2">
				<span
					>{i18n.series_episode_count({
						count: totalEpisodes.toLocaleString(),
					})}</span
				>
				{#if lastScan}
					<span class="text-fg-faint">·</span>
					<span>{i18n.series_last_scan({ when: formatRelative(lastScan) })}</span>
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
			{#if seriesQuery.hasNextPage}
				<div bind:this={pageSentinel} class="h-10">
					{#if seriesQuery.isFetchingNextPage}
						<p class="py-3 text-center text-xs text-fg-subtle">
							{i18n.common_loading()}
						</p>
					{/if}
				</div>
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
