<script lang="ts">
	import { untrack } from "svelte";
	import { onMount } from "svelte";
	import { Plus } from "@lucide/svelte";
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { formatRelative } from "../../lib/dates";
	import { formatBytes } from "../../lib/format";
	import { movieStatus } from "../../lib/status";
	import { loadPref, savePref } from "../../lib/prefs";
	import { pageMeta } from "../../lib/page-meta.svelte";
	import MoviesToolbar from "../../components/movies/MoviesToolbar.svelte";
	import MovieGrid from "../../components/movies/MovieGrid.svelte";
	import MovieList from "../../components/movies/MovieList.svelte";
	import MoviesEmpty from "../../components/movies/MoviesEmpty.svelte";
	import MovieBulkActions from "../../components/movies/MovieBulkActions.svelte";
	import type {
		Movie,
		PaginatedMovies,
		ScheduleList,
	} from "../../lib/types";

	type View = "grid" | "list";
	type SortKey = "title" | "year";
	type SortOrder = "asc" | "desc";
	type Density = "compact" | "roomy";

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
	// Poster density is a phone-only choice and never worth a URL parameter.
	const DENSITY_PREF = "streamline:movies:density";

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
	let density = $state<Density>(
		loadPref(DENSITY_PREF) === "roomy" ? "roomy" : "compact",
	);

	function setDensity(d: Density) {
		density = d;
		savePref(DENSITY_PREF, d);
	}

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
		if (monitoredOnly) p.set("monitored", "1");
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

	// Below md the page gives up its own count line and the topbar carries it
	// under the title instead; at md and up the line below the toolbar stays.
	let metaLine = $derived.by(() => {
		const parts = [`${counts.total} titles`];
		if (monitoredSize > 0)
			parts.push(`${formatBytes(monitoredSize, "0 B")} monitored`);
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
		query;
		monitoredOnly;
		untrack(clearSelection);
	});
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
			view={shownView}
			{counts}
			{monitoredOnly}
			{monitoredCount}
			{density}
			{selectMode}
			selectedCount={selected.size}
			visibleCount={visibleMovies.length}
			onTabChange={(t) => (tab = t)}
			onQueryChange={(q) => (query = q)}
			onMonitoredChange={(v) => (monitoredOnly = v)}
			onSortChange={setSort}
			onViewChange={(v) => (view = v)}
			onDensityChange={setDensity}
			onClearFilters={clearFilters}
			onSelectModeChange={setSelectMode}
			onSelectAll={selectAll}
			onAddMovie={openAddMovie}
		/>

		<div
			class="hidden w-full flex-wrap items-baseline justify-between gap-2 px-4 pt-4 pb-2 font-mono text-[11px] text-fg-subtle md:flex md:px-6"
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
					{density}
					onToggle={toggle}
					onLongPress={beginLongPress}
				/>
			{/if}
		</div>

		<MovieBulkActions
			movies={visibleMovies}
			{selected}
			total={visibleMovies.length}
			onSelectAll={() => toggleAll(true)}
			onClear={clearSelection}
		/>

		<!-- Touch entry point: the toolbar's Add button is a 36px control at the top
		     of the page, out of thumb reach on a phone. It stands down while a
		     selection owns the bottom of the screen. -->
		{#if selected.size === 0 && !selectMode}
			<button
				type="button"
				onclick={openAddMovie}
				aria-label="Add movie"
				class="fixed right-4 bottom-[calc(env(safe-area-inset-bottom)+4.75rem)] z-30 grid h-14 w-14 place-items-center rounded-full bg-accent text-fg-on-accent shadow-3 transition active:scale-95 md:hidden"
			>
				<Plus size={26} aria-hidden="true" />
			</button>
		{/if}
	{/if}
</div>
