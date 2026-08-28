<script lang="ts">
	import { untrack } from "svelte";
	import { onMount } from "svelte";
	import { createInfiniteQuery, createQuery } from "@tanstack/svelte-query";
	import { api, errorText, type Paginated } from "../../lib/api";
	import { formatRelative } from "../../lib/dates";
	import { loadPref, savePref } from "../../lib/prefs";
	import { pageMeta } from "../../lib/page-meta.svelte";
	import MoviesToolbar from "../../components/movies/MoviesToolbar.svelte";
	import MovieGrid from "../../components/movies/MovieGrid.svelte";
	import MovieList from "../../components/movies/MovieList.svelte";
	import MoviesEmpty from "../../components/movies/MoviesEmpty.svelte";
	import MovieBulkActions from "../../components/movies/MovieBulkActions.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import type {
		Movie,
		MovieCounts,
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

	// An explicit ?sort in the URL wins so shared links keep their ordering;
	// otherwise fall back to the last sort this browser chose, then A→Z.
	const SORT_PREF = "streamline:movies:sort";

	function readParams() {
		const p =
			typeof window === "undefined"
				? new URLSearchParams()
				: new URLSearchParams(window.location.search);
		const stored = loadPref(SORT_PREF)?.split("-") ?? [];
		const rawTab = p.get("status") ?? "all";
		const rawSort = p.get("sort") ?? stored[0] ?? "title";
		const rawOrder = p.get("order") ?? stored[1] ?? "asc";
		const rawView = p.get("view") ?? "grid";
		return {
			tab: VALID_TABS.has(rawTab) ? rawTab : "all",
			query: p.get("q") ?? "",
			sort: (rawSort === "year" ? "year" : "title") as SortKey,
			order: (rawOrder === "desc" ? "desc" : "asc") as SortOrder,
			view: (rawView === "list" ? "list" : "grid") as View,
		};
	}

	const initial = readParams();
	let tab = $state(initial.tab);
	let query = $state(initial.query);
	let sort = $state<SortKey>(initial.sort);
	let order = $state<SortOrder>(initial.order);
	let view = $state<View>(initial.view);
	// debouncedQuery is what the server query keys on. Typing must not fire a
	// request per keystroke now that filtering is server-side.
	let debouncedQuery = $state(initial.query);
	$effect(() => {
		const q = query;
		const t = setTimeout(() => (debouncedQuery = q), 250);
		return () => clearTimeout(t);
	});
	// A phone has no width for the list table — seven columns at 390px is not a
	// readable row. Below md the library is posters only, whatever the URL or the
	// last-used view says, and the sheet stops offering the choice.
	let narrow = $state(false);
	onMount(() => {
		const mql = window.matchMedia("(max-width: 767px)");
		const sync = () => (narrow = mql.matches);
		sync();
		mql.addEventListener("change", sync);
		return () => mql.removeEventListener("change", sync);
	});
	let shownView = $derived<View>(narrow ? "grid" : view);

	function setSort(s: SortKey, o: SortOrder) {
		sort = s;
		order = o;
		savePref(SORT_PREF, `${s}-${o}`);
	}

	function openAddMovie() {
		window.dispatchEvent(new CustomEvent("streamline:open-add-movie"));
	}

	$effect(() => {
		// Every reactive read happens before the early returns below: a run that
		// bails out first would register no dependencies and the effect would
		// never fire again.
		const p = new URLSearchParams();
		if (tab !== "all") p.set("status", tab);
		if (query) p.set("q", query);
		if (sort !== "title") p.set("sort", sort);
		if (order !== "asc") p.set("order", order);
		if (view !== "grid") p.set("view", view);
		const search = p.toString();

		if (typeof window === "undefined") return;
		// Routify mounts the incoming route before it updates window.location, so
		// on a navigation *into* this page the first flush still sees the outgoing
		// URL. Writing then would stamp this page's (default, empty) filters onto
		// the previous route's path and cancel the navigation — which is exactly
		// what a detail page's back link hit: /movies/2?tab=cast became /movies/2
		// and never reached the list.
		if (window.location.pathname !== "/movies") return;

		const next = `${window.location.pathname}${search ? `?${search}` : ""}`;
		if (next !== window.location.pathname + window.location.search) {
			window.history.replaceState(null, "", next);
		}
	});

	// Filtering, sorting and paging are the server's. The page used to pull the
	// whole library and do all three in the browser, which meant every stale
	// refetch — window focus, any mutation — re-walked 600+ rows over seven
	// requests to render twenty cards.
	const PAGE = 50;
	const moviesQuery = createInfiniteQuery<
		Paginated<Movie>,
		Error,
		{ pages: Paginated<Movie>[]; pageParams: number[] },
		readonly ["movies", string, string, SortKey, SortOrder],
		number
	>(() => ({
		queryKey: ["movies", tab, debouncedQuery, sort, order] as const,
		queryFn: ({ pageParam }) => {
			const p = new URLSearchParams({
				page: String(pageParam),
				limit: String(PAGE),
				sort,
				order,
			});
			if (tab !== "all") p.set("status", tab);
			if (debouncedQuery.trim()) p.set("query", debouncedQuery.trim());
			return api<Paginated<Movie>>(`/movies?${p.toString()}`);
		},
		initialPageParam: 1,
		getNextPageParam: (last, pages) =>
			pages.flatMap((p) => p.items).length < last.total
				? pages.length + 1
				: undefined,
	}));

	const countsQuery = createQuery<MovieCounts>(() => ({
		queryKey: ["movies", "counts"],
		queryFn: () => api<MovieCounts>("/movies/counts"),
	}));

	const schedulesQuery = createQuery<ScheduleList>(() => ({
		queryKey: ["schedules"],
		queryFn: () => api<ScheduleList>("/schedules"),
	}));

	let counts = $derived(
		countsQuery.data ?? {
			total: 0,
			wanted: 0,
			downloading: 0,
			available: 0,
			failed: 0,
		},
	);

	// Everything loaded so far. Bulk selection and the "N of M" line read this,
	// so both mean "of what you have pulled in", not "of the whole library".
	let visibleMovies = $derived(
		(moviesQuery.data?.pages ?? []).flatMap((p) => p.items),
	);
	let matchedTotal = $derived(moviesQuery.data?.pages?.[0]?.total ?? 0);

	let libraryEmpty = $derived(
		tab === "all" && !debouncedQuery && counts.total === 0,
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
					moviesQuery.hasNextPage &&
					!moviesQuery.isFetchingNextPage
				) {
					moviesQuery.fetchNextPage();
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
		query = "";
	}

	// Below md the page gives up its own count line and the topbar carries it
	// under the title instead; at md and up the line below the toolbar stays.
	let metaLine = $derived.by(() => {
		const parts = [`${counts.total} titles`];
		if (lastScan) parts.push(`scan ${formatRelative(lastScan)}`);
		return parts.join(" · ");
	});

	$effect(() => {
		pageMeta.set(metaLine);
		return () => pageMeta.clear();
	});

	// ── Selection ────────────────────────────────────────────────────────────
	// Held as ids rather than movie objects so a refetch can't strand a stale
	// copy in the set. Reassigned on every change — Set mutation isn't reactive.
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
	// A press and hold on a poster is the touch way in: it turns selection on and
	// takes the held card with it.
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
		selected = new Set(visibleMovies.map((m) => m.id));
	}
	function clearSelection() {
		if (selected.size > 0) selected = new Set();
	}

	// A selection made under one filter is meaningless under the next, and the
	// bar would keep acting on rows the user can no longer see.
	$effect(() => {
		tab;
		debouncedQuery;
		sort;
		order;
		untrack(clearSelection);
	});
</script>

<div class="flex flex-col">
	{#if moviesQuery.isLoading}
		<div class="w-full px-4 py-16 text-center text-sm text-fg-subtle md:px-6">
			{i18n.common_loading_movies()}
		</div>
	{:else if moviesQuery.isError}
		<div class="w-full px-4 md:px-6">
			<div
				class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center"
			>
				<p class="text-sm font-semibold text-status-failed">
					{i18n.movies_load_list_failed()}
				</p>
				<p class="mt-1 text-xs text-fg-subtle">
					{errorText(moviesQuery.error, i18n.common_unknown_error())}
				</p>
			</div>
		</div>
	{:else}
		<MoviesToolbar
			{tab}
			{query}
			{sort}
			{order}
			view={shownView}
			{counts}
			{selectMode}
			selectedCount={selected.size}
			visibleCount={visibleMovies.length}
			onTabChange={(t) => (tab = t)}
			onQueryChange={(q) => (query = q)}
			onSortChange={setSort}
			onViewChange={(v) => (view = v)}
			onClearFilters={clearFilters}
			onSelectModeChange={setSelectMode}
			onSelectAll={selectAll}
			onAddMovie={openAddMovie}
		/>

		<div
			class="hidden w-full flex-wrap items-baseline justify-between gap-2 px-4 pt-4 pb-2 font-mono text-[11px] text-fg-subtle md:flex md:px-6"
		>
			<div>
				{visibleMovies.length} of {matchedTotal} titles
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
				{#if lastScan}
					<span>last scan {formatRelative(lastScan)}</span>
				{/if}
			</div>
		</div>

		<div class="w-full px-4 pb-6 pt-3 md:px-6 md:pt-0">
			{#if visibleMovies.length === 0}
				<MoviesEmpty
					variant={libraryEmpty ? "library" : "filter"}
					onClear={clearFilters}
				/>
			{:else if shownView === "list"}
				<MovieList
					movies={visibleMovies}
					{sort}
					{order}
					onSortChange={setSort}
					{selected}
					onToggle={toggle}
					onToggleAll={toggleAll}
				/>
			{:else}
				<MovieGrid
					movies={visibleMovies}
					{selected}
					{selectMode}
					onToggle={toggle}
					onLongPress={beginLongPress}
				/>
			{/if}
			{#if moviesQuery.hasNextPage}
				<div bind:this={pageSentinel} class="h-10">
					{#if moviesQuery.isFetchingNextPage}
						<p class="py-3 text-center text-xs text-fg-subtle">
							{i18n.common_loading()}
						</p>
					{/if}
				</div>
			{/if}
		</div>

		<MovieBulkActions
			movies={visibleMovies}
			{selected}
			total={visibleMovies.length}
			onSelectAll={() => toggleAll(true)}
			onClear={clearSelection}
		/>
	{/if}
</div>
