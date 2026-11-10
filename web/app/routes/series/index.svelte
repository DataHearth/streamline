<script lang="ts">
	import SkeletonToolbar from "@components/shared/SkeletonToolbar.svelte";
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import Skeleton from "@components/shared/Skeleton.svelte";
	import { untrack } from "svelte";
	import { onMount } from "svelte";
	import {
		createInfiniteQuery,
		createQuery,
		keepPreviousData,
	} from "@tanstack/svelte-query";
	import { api, errorText, type Paginated } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { formatRelative } from "@lib/dates";
	import { loadPref, savePref, SERIES_SEARCH } from "@lib/prefs";
	import { oneOf, param, setParam, syncRouteQuery } from "@lib/url-state.svelte";
	import SeriesToolbar from "@components/series/SeriesToolbar.svelte";
	import type {
		SeriesTab,
		SeriesTypeFilter,
		SeriesMonFilter,
		SeriesSort,
		SeriesOrder,
		SeriesTabCounts,
		SeriesTypeCounts,
		SeriesMonCounts,
	} from "@components/series/SeriesToolbar.svelte";
	import SeriesGrid from "@components/series/SeriesGrid.svelte";
	import SeriesList from "@components/series/SeriesList.svelte";
	import SeriesEmpty from "@components/series/SeriesEmpty.svelte";
	import SeriesBulkActions from "@components/series/SeriesBulkActions.svelte";
	import {
		seriesSelectionRow,
		type ScheduleList,
		type SeriesSelectionRow,
		type TVShow,
		type TVShowCounts,
	} from "@lib/types";
	import { toast } from "@lib/toast";
	import { openAdd } from "@lib/add-flow";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type View = "grid" | "list";

	const TABS: SeriesTab[] = [
		"all",
		"continuing",
		"ended",
		"missing",
		"downloading",
		"importing",
	];
	// `type` is how a show numbers its episodes, an axis of its own — not the
	// `kind` other pages use for a family of media.
	const TYPES: SeriesTypeFilter[] = ["all", "standard", "anime", "daily"];
	const MON: SeriesMonFilter[] = ["all", "monitored", "unmonitored"];
	const SORTS: SeriesSort[] = ["recent", "title", "year", "rating", "episodes"];

	// An explicit ?sort in the URL wins so shared links keep their ordering;
	// otherwise fall back to the last sort this browser chose, then A→Z. The
	// pref stores "<key>-<order>", matching the movies library.
	const SORT_PREF = "streamline:series:sort";

	function readParams(
		p = new URLSearchParams(
			typeof window === "undefined" ? "" : window.location.search,
		),
	) {
		const stored = loadPref(SORT_PREF)?.split("-") ?? [];
		return {
			tab: oneOf(param(p, "status"), TABS, "all"),
			typeFilter: oneOf(param(p, "type"), TYPES, "all"),
			mon: oneOf(param(p, "monitored"), MON, "all"),
			query: param(p, "q") ?? "",
			sort: oneOf(param(p, "sort") ?? stored[0], SORTS, "title"),
			order: oneOf<SeriesOrder>(param(p, "order") ?? stored[1], ["asc", "desc"], "asc"),
			view: oneOf<View>(param(p, "view"), ["grid", "list"], "grid"),
		};
	}

	const initial = readParams();
	let tab = $state<SeriesTab>(initial.tab);
	let typeFilter = $state<SeriesTypeFilter>(initial.typeFilter);
	let mon = $state<SeriesMonFilter>(initial.mon);
	let query = $state(initial.query);
	let sort = $state<SeriesSort>(initial.sort);
	let order = $state<SeriesOrder>(initial.order);
	let view = $state<View>(initial.view);
	// debouncedQuery is what the server query keys on. Typing must not fire a
	// request per keystroke, since filtering is server-side.
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

	syncRouteQuery(
		"/series",
		(p) => {
			const v = readParams(p);
			tab = v.tab;
			typeFilter = v.typeFilter;
			mon = v.mon;
			query = v.query;
			debouncedQuery = v.query;
			sort = v.sort;
			order = v.order;
			view = v.view;
		},
		() => {
			const p = new URLSearchParams();
			setParam(p, "status", tab, "all");
			setParam(p, "type", typeFilter, "all");
			setParam(p, "monitored", mon, "all");
			setParam(p, "q", query);
			setParam(p, "sort", sort, "title");
			setParam(p, "order", order, "asc");
			setParam(p, "view", view, "grid");
			return p;
		},
		(search) => savePref(SERIES_SEARCH, search),
	);

	function setSort(s: SeriesSort, o: SeriesOrder) {
		sort = s;
		order = o;
		savePref(SORT_PREF, `${s}-${o}`);
	}

	// Filtering, sorting and paging are the server's — db.FilterTVShows has
	// pushed all three into SQL since the list-performance work; the page just
	// never used the params. It pulled the whole library and redid the work in
	// the browser on every stale refetch.
	const PAGE = 50;
	// The filters alone, which the list, the counts and the selection share.
	function filterParams(): URLSearchParams {
		const p = new URLSearchParams();
		if (tab !== "all") p.set("status", tab);
		if (typeFilter !== "all") p.set("type", typeFilter);
		if (mon !== "all") p.set("monitored", mon);
		if (debouncedQuery.trim()) p.set("query", debouncedQuery.trim());
		return p;
	}
	const seriesQuery = createInfiniteQuery<
		Paginated<TVShow>,
		Error,
		{ pages: Paginated<TVShow>[]; pageParams: number[] },
		readonly [
			"series",
			SeriesTab,
			SeriesTypeFilter,
			SeriesMonFilter,
			string,
			SeriesSort,
			SeriesOrder,
		],
		number
	>(() => ({
		queryKey: [
			"series",
			tab,
			typeFilter,
			mon,
			debouncedQuery,
			sort,
			order,
		] as const,
		queryFn: ({ pageParam }) => {
			const p = filterParams();
			p.set("page", String(pageParam));
			p.set("limit", String(PAGE));
			p.set("sort", sort);
			p.set("order", order);
			return api<Paginated<TVShow>>(`/series?${p.toString()}`);
		},
		initialPageParam: 1,
		getNextPageParam: (last, pages) =>
			pages.flatMap((p) => p.items).length < last.total
				? pages.length + 1
				: undefined,
		// Every filter keystroke is a new query key, and without this the page
		// falls back to isLoading — which unmounts the toolbar the user is
		// typing in, taking the caret with it.
		placeholderData: keepPreviousData,
	}));

	// The counts carry the same filters as the list: each facet is tallied with
	// the *other* facets applied, so a dropdown says how many series picking a
	// value would leave rather than how many the unfiltered library holds.
	const countsQuery = createQuery<TVShowCounts>(() => ({
		queryKey: ["series", "counts", tab, typeFilter, mon, debouncedQuery] as const,
		queryFn: () => {
			const qs = filterParams().toString();
			return api<TVShowCounts>(`/series/counts${qs ? `?${qs}` : ""}`);
		},
		placeholderData: keepPreviousData,
	}));

	const schedulesQuery = createQuery<ScheduleList>(() => ({
		queryKey: ["schedules"],
		queryFn: () => api<ScheduleList>("/schedules"),
		enabled: auth.isAdmin,
	}));

	// Each facet reads its own "all": the three totals diverge as soon as two
	// facets are filtered, and sharing `total` made a dropdown's "all" row
	// claim a population it would not return.
	let counts = $derived<SeriesTabCounts>({
		all: countsQuery.data?.status_total ?? 0,
		continuing: countsQuery.data?.continuing ?? 0,
		ended: countsQuery.data?.ended ?? 0,
		missing: countsQuery.data?.missing ?? 0,
		downloading: countsQuery.data?.downloading ?? 0,
		importing: countsQuery.data?.importing ?? 0,
	});

	let typeCounts = $derived<SeriesTypeCounts>({
		all: countsQuery.data?.type_total ?? 0,
		standard: countsQuery.data?.standard ?? 0,
		anime: countsQuery.data?.anime ?? 0,
		daily: countsQuery.data?.daily ?? 0,
	});

	let monCounts = $derived<SeriesMonCounts>({
		all: countsQuery.data?.monitored_total ?? 0,
		monitored: countsQuery.data?.monitored ?? 0,
		unmonitored: countsQuery.data?.unmonitored ?? 0,
	});

	// The library, unfiltered — the header line and the empty state, neither of
	// which is a facet row.
	let libraryTotal = $derived(countsQuery.data?.total ?? 0);

	// Everything loaded so far, which the "N of M" line reads.
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

	let filtering = $derived(
		tab !== "all" || typeFilter !== "all" || mon !== "all" || !!debouncedQuery,
	);
	let libraryEmpty = $derived(!filtering && libraryTotal === 0);

	// The next page is appended as the sentinel comes into view: a page is 50
	// cards, which mounts without blocking, and there is no whole library
	// sitting in memory to slice.
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
		mon = "all";
		query = "";
	}

	// The heading's count line, below md only: from md the row under the
	// toolbar carries the same counts and the last scan.
	let metaLine = $derived.by(() => {
		const parts = [
			filtering
				? i18n.series_count_of({ visible: matchedTotal, total: libraryTotal })
				: i18n.series_shows_count({ count: libraryTotal }),
		];
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

	// ── Selection ────────────────────────────────────────────────────────────
	let selected = $state(new Set<number>());
	// Grid cards only reveal their checkbox on hover, which leaves touch users
	// with no way in — the toolbar toggle pins them open instead.
	let selectMode = $state(false);

	function setSelectMode(v: boolean) {
		selectMode = v;
		if (!v) clearSelection();
	}
	// Bumped whenever a selection is cleared, so a select all still in flight
	// when the filters change does not select the next filter's rows.
	let selectionEpoch = 0;
	// "Select all" asks the server for every match in one slim list rather
	// than loading every page of full cards; a hand-picked selection reads the
	// loaded cards. Dropped with the selection.
	let fetchedRows = $state<SeriesSelectionRow[] | null>(null);
	let selectionRows = $derived(fetchedRows ?? visibleSeries.map(seriesSelectionRow));
	async function selectAll() {
		selectMode = true;
		const epoch = selectionEpoch;
		const qs = filterParams().toString();
		let rows: SeriesSelectionRow[];
		try {
			rows = (await api<{ items: SeriesSelectionRow[] }>(`/series/selection${qs ? `?${qs}` : ""}`)).items;
		} catch (e) {
			toast.err(errorText(e, i18n.common_unknown_error()));
			return;
		}
		if (epoch !== selectionEpoch) return;
		fetchedRows = rows;
		selected = new Set(rows.map((r) => r.id));
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
		void selectAll();
	}
	function clearSelection() {
		selectionEpoch++;
		fetchedRows = null;
		if (selected.size > 0) selected = new Set();
	}

	$effect(() => {
		tab;
		typeFilter;
		mon;
		query;
		untrack(clearSelection);
	});
</script>

<div class="flex flex-col">
	<!-- The title is the top bar's; the phone meta line sits outside the loading
	     branches so it is in place before the list is. -->
	<p class="truncate px-4 pt-4 text-sm text-fg-muted md:hidden">{metaLine}</p>
	{#if seriesQuery.isLoading}
		<span class="sr-only" role="status">{i18n.common_loading_series()}</span>
		<SkeletonToolbar triggers={3} />
		<!-- The count row ("24 of 24 titles" · "363 episodes · last scan …") is
		     also loaded-branch-only, and is 40.5px of the offset. Same gating as
		     the real one, so it stays absent at phone where the real row is too. -->
		<div
			class="hidden w-full flex-wrap items-baseline justify-between gap-2 px-4 pb-2 pt-4 md:flex md:px-6"
			aria-hidden="true"
		>
			<div class="h-[16.5px] w-[104px] animate-pulse rounded bg-white/[0.06] motion-reduce:animate-none"></div>
			<div class="h-[16.5px] w-[196px] animate-pulse rounded bg-white/[0.06] motion-reduce:animate-none"></div>
		</div>
		<div class="w-full px-4 pb-6 pt-3 md:px-6 md:pt-0">
			{#if shownView === "list"}
				<div
					class="@container overflow-hidden rounded-lg border border-border bg-bg-elevated/70"
				>
					<div class="border-b border-border px-3 py-2.5">
						<Skeleton w="180px" h={9} />
					</div>
					<SkeletonList variant="media-row" count={8} />
				</div>
			{:else}
				<div
					class="grid grid-cols-[repeat(auto-fill,minmax(160px,1fr))] gap-x-4 gap-y-6 md:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] xl:grid-cols-[repeat(auto-fill,minmax(200px,1fr))]"
				>
					<SkeletonList variant="poster" count={12} />
				</div>
			{/if}
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
			{mon}
			{query}
			{sort}
			{order}
			view={shownView}
			{counts}
			{typeCounts}
			{monCounts}
			{selectMode}
			selectedCount={selected.size}
			matchedCount={matchedTotal}
			onTabChange={(t) => (tab = t)}
			onTypeChange={(t) => (typeFilter = t)}
			onMonChange={(m) => (mon = m)}
			onQueryChange={(q) => (query = q)}
			onSortChange={setSort}
			onViewChange={(v) => (view = v)}
			onClearFilters={clearFilters}
			onSelectModeChange={setSelectMode}
			onSelectAll={selectAll}
			onAddSeries={() => openAdd("series")}
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

		{#if auth.canAddDirectly}
			<SeriesBulkActions
				items={selectionRows}
				{selected}
				total={matchedTotal}
				onSelectAll={selectAll}
				onClear={clearSelection}
			/>
		{/if}
	{/if}
</div>
