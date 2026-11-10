<script lang="ts">
	import { onMount, tick, untrack } from "svelte";
	import { createInfiniteQuery, createQuery, keepPreviousData } from "@tanstack/svelte-query";
	import { Eye, Tags } from "@lucide/svelte";
	import { api, errorText, PAGE_LIMIT, type Paginated } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { createAction, monitorToast, MUSIC, searchFailed, updateFailed } from "@lib/actions";
	import { cn } from "@lib/cn";
	import { formatRelative } from "@lib/dates";
	import { loadPref, savePref } from "@lib/prefs";
	import { oneOf, param, setParam, syncRouteQuery } from "@lib/url-state.svelte";
	import LibraryToolbar from "@components/shared/LibraryToolbar.svelte";
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import ArtistCard from "@components/music/ArtistCard.svelte";
	import ArtistTable from "@components/music/ArtistTable.svelte";
	import LetterScroller from "@components/music/LetterScroller.svelte";
	import LetterRail from "@components/music/LetterRail.svelte";
	import { LETTERS, sortLetter, type Artist, type ArtistCounts } from "@lib/music-books";
	import type { ScheduleList } from "@lib/types";
	import { openAdd } from "@lib/add-flow";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The music library is the Series pattern: one card per artist, a grid or a
	// list, and everything else on the artist page. Sorted by name it is filed
	// under letters: their headers stay pinned, a jump bar (a fast scroller on
	// a phone) goes straight to one, and a letter past what is loaded opens the
	// list at its page rather than fetching every page before it.
	const STATUSES = ["all", "wanted", "downloading", "available"] as const;
	const MONS = ["all", "monitored", "unmonitored"] as const;
	type View = "grid" | "list";
	type SortKey = "title" | "recent";
	type SortOrder = "asc" | "desc";
	// The direction each sort starts in, and the one a URL naming the sort alone
	// means: names A→Z, additions newest first.
	const NATURAL: Record<SortKey, SortOrder> = { title: "asc", recent: "desc" };
	// The last sort this browser chose, "<key>-<order>", for a URL naming none.
	const SORT_PREF = "streamline:music:sort";

	let status = $state("all");
	let mon = $state("all");
	let genre = $state("all");
	let query = $state("");
	let debounced = $state("");
	let sort = $state<SortKey>("title");
	let order = $state<SortOrder>("asc");
	let view = $state<View>("grid");

	syncRouteQuery(
		"/music",
		(p) => {
			const stored = loadPref(SORT_PREF)?.split("-") ?? [];
			const named = param(p, "sort");
			status = oneOf(param(p, "status"), STATUSES, "all");
			mon = oneOf(param(p, "monitored"), MONS, "all");
			genre = param(p, "genre") || "all";
			query = debounced = param(p, "q") ?? "";
			// `name` is the old spelling of `title`.
			sort = oneOf<SortKey>((named === "name" ? "title" : named) ?? stored[0], ["title", "recent"], "title");
			order = oneOf<SortOrder>(param(p, "order") ?? (named ? undefined : stored[1]), ["asc", "desc"], NATURAL[sort]);
			view = oneOf<View>(param(p, "view"), ["grid", "list"], "grid");
		},
		() => {
			const p = new URLSearchParams();
			setParam(p, "status", status, "all");
			setParam(p, "monitored", mon, "all");
			setParam(p, "genre", genre, "all");
			setParam(p, "q", query);
			setParam(p, "sort", sort, "title");
			setParam(p, "order", order, NATURAL[sort]);
			setParam(p, "view", view, "grid");
			return p;
		},
	);
	function setSort(key: string) {
		const [s, o] = key.split("-") as [SortKey, SortOrder];
		sort = s;
		order = o;
		savePref(SORT_PREF, key);
	}
	$effect(() => {
		const q = query;
		const t = setTimeout(() => (debounced = q), 250);
		return () => clearTimeout(t);
	});

	// A phone has no width for the table, so below md the library is the grid,
	// whatever the URL says, as in Series.
	let narrow = $state(false);
	onMount(() => {
		const mql = window.matchMedia("(max-width: 767px)");
		const sync = () => (narrow = mql.matches);
		sync();
		mql.addEventListener("change", sync);
		return () => mql.removeEventListener("change", sync);
	});
	let shownView = $derived<View>(narrow ? "grid" : view);

	// A jump past what is loaded opens the list at the letter's page. It holds
	// only for the filters it was made under; any other result starts at page 1.
	let filterKey = $derived(JSON.stringify([status, mon, genre, debounced, sort, order]));
	let jumped = $state<{ key: string; page: number } | null>(null);
	let fromPage = $derived(jumped && jumped.key === filterKey ? jumped.page : 1);
	// Forgotten as soon as the filters move, so going back to them later starts
	// at the top too.
	$effect(() => {
		if (jumped && jumped.key !== filterKey) jumped = null;
	});

	const filterParams = () => {
		const p = new URLSearchParams();
		if (status !== "all") p.set("status", status);
		if (mon !== "all") p.set("monitored", mon);
		if (genre !== "all") p.set("genre", genre);
		if (debounced.trim()) p.set("query", debounced.trim());
		return p;
	};

	const listQuery = createInfiniteQuery<
		Paginated<Artist>,
		Error,
		{ pages: Paginated<Artist>[]; pageParams: number[] },
		readonly ["music", "artists", string, string, string, string, SortKey, SortOrder, number],
		number
	>(() => ({
		queryKey: ["music", "artists", status, mon, genre, debounced, sort, order, fromPage] as const,
		queryFn: ({ pageParam }) => {
			const p = filterParams();
			p.set("sort", sort === "title" ? "name" : sort);
			p.set("order", order);
			p.set("page", String(pageParam));
			p.set("limit", String(PAGE_LIMIT));
			return api<Paginated<Artist>>(`/music/artists?${p}`);
		},
		initialPageParam: fromPage,
		getNextPageParam: (last, _pages, lastParam) => (lastParam * PAGE_LIMIT < last.total ? lastParam + 1 : undefined),
		getPreviousPageParam: (_first, _pages, firstParam) => (firstParam > 1 ? firstParam - 1 : undefined),
		refetchInterval: (q) => (q.state.data?.pages.some((pg) => pg.items.some((a) => a.hydrating)) ? 10_000 : false),
		placeholderData: keepPreviousData,
	}));
	// The facet rows only mean something against the page's own filters; the
	// nav badge reads the unfiltered entry under ["music", "artists", "counts"].
	const countsQuery = createQuery<ArtistCounts>(() => ({
		queryKey: ["music", "artists", "counts", { status, monitored: mon, genre, query: debounced }],
		queryFn: () => {
			const qs = filterParams().toString();
			return api<ArtistCounts>(`/music/artists/counts${qs ? `?${qs}` : ""}`);
		},
		placeholderData: keepPreviousData,
	}));
	const schedulesQuery = createQuery<ScheduleList>(() => ({
		queryKey: ["schedules"],
		queryFn: () => api<ScheduleList>("/schedules"),
		enabled: auth.isAdmin,
	}));

	let items = $derived((listQuery.data?.pages ?? []).flatMap((p) => p.items));
	let firstIndex = $derived(((listQuery.data?.pageParams?.[0] ?? 1) - 1) * PAGE_LIMIT);
	let matched = $derived(listQuery.data?.pages?.[0]?.total ?? 0);
	let counts = $derived(countsQuery.data);
	let letters = $derived(counts?.letters ?? {});
	let filtering = $derived(status !== "all" || mon !== "all" || genre !== "all" || !!debounced);
	// Sorted by name A→Z the list is filed under letters, whatever its length.
	// "Recently added" has no letters to file under, and the jump bar's page
	// arithmetic counts letters from the top of an A→Z list, so Z→A is a plain
	// list too.
	let lettered = $derived(sort === "title" && order === "asc");
	let lastFinished = $derived.by(() => {
		let latest: string | null = null;
		for (const s of schedulesQuery.data?.items ?? [])
			if (s.last_finished_at && (!latest || s.last_finished_at > latest)) latest = s.last_finished_at;
		return latest;
	});
	let lastScan = $derived(lastFinished ? formatRelative(lastFinished) : "");

	// Sorted by name, the loaded artists fall into runs of one letter. A run
	// that continues on the next page stays one section.
	type Section = { letter: string | null; count?: number; items: Artist[] };
	let sections = $derived.by<Section[]>(() => {
		if (!lettered) return [{ letter: null, items }];
		const out: Section[] = [];
		for (const a of items) {
			const l = sortLetter(a);
			const last = out[out.length - 1];
			if (last && last.letter === l) last.items.push(a);
			else out.push({ letter: l, count: letters[l], items: [a] });
		}
		return out;
	});

	// ── Letters: where the list is, and going to one ───────────────────────
	// Each anchor lands under the sticky bars: the top bar, plus the phone's chip
	// row. From md the letters are a rail down the right edge, not a bar.
	const ANCHOR = "scroll-mt-[124px] md:scroll-mt-16";
	let listEl = $state<HTMLElement | null>(null);
	let mainEl: HTMLElement | null = null;
	let current = $state<string | null>(null);
	let progress = $state(0);
	let scrolling = $state(false);
	let pendingJump = $state<string | null>(null);

	const anchorOf = (l: string) => listEl?.querySelector<HTMLElement>(`[data-letter="${l}"]`) ?? null;
	function syncPosition() {
		if (!listEl || !mainEl) return;
		const top = mainEl.getBoundingClientRect().top;
		let cur: string | null = null;
		for (const el of listEl.querySelectorAll<HTMLElement>("[data-letter]")) {
			const margin = parseFloat(getComputedStyle(el).scrollMarginTop) || 0;
			if (cur === null || el.getBoundingClientRect().top - top - margin <= 1) cur = el.dataset.letter ?? null;
			else break;
		}
		current = cur;
		const span = mainEl.scrollHeight - mainEl.clientHeight;
		const f = span > 0 ? mainEl.scrollTop / span : 0;
		progress = matched ? Math.min(1, (firstIndex + f * items.length) / matched) : 0;
	}
	onMount(() => {
		mainEl = document.getElementById("main");
		let frame = 0;
		let idle: ReturnType<typeof setTimeout> | undefined;
		const onScroll = () => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(syncPosition);
			scrolling = true;
			clearTimeout(idle);
			idle = setTimeout(() => (scrolling = false), 1200);
		};
		mainEl?.addEventListener("scroll", onScroll, { passive: true });
		return () => {
			mainEl?.removeEventListener("scroll", onScroll);
			cancelAnimationFrame(frame);
			clearTimeout(idle);
		};
	});
	$effect(() => {
		void sections;
		tick().then(syncPosition);
	});

	function offsetOf(l: string) {
		let n = 0;
		for (const x of LETTERS) {
			if (x === l) break;
			n += letters[x] ?? 0;
		}
		return n;
	}
	// A jump glides there; a drag along the letters follows the finger at once.
	// While a glide runs no page is added above: making up its height would
	// stop the glide where it was.
	let gliding = $state(false);
	let glideEnd: (() => void) | null = null;
	function glide() {
		glideEnd?.();
		gliding = true;
		const timer = setTimeout(() => glideEnd?.(), 1200);
		const done = () => {
			clearTimeout(timer);
			mainEl?.removeEventListener("scrollend", done);
			glideEnd = null;
			gliding = false;
		};
		mainEl?.addEventListener("scrollend", done);
		glideEnd = done;
	}
	const still = () => window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
	function land(l: string, smooth = false) {
		const el = anchorOf(l);
		if (!el) return false;
		const behavior: ScrollBehavior = smooth && !still() ? "smooth" : "auto";
		if (behavior === "smooth") glide();
		// The library's first letter is its top, toolbar included: the way back
		// to the filters from deep in the list, which scroll away with the page.
		if (firstIndex === 0 && el === listEl?.querySelector("[data-letter]")) mainEl?.scrollTo({ top: 0, behavior });
		else el.scrollIntoView({ block: "start", behavior });
		current = l;
		return true;
	}
	function jump(l: string) {
		if (land(l, true)) return;
		pendingJump = l;
		jumped = { key: filterKey, page: Math.floor(offsetOf(l) / PAGE_LIMIT) + 1 };
	}
	// The letters show a loaded one as a drag passes it and fetch one further
	// on only when the drag ends; a click is a drag that ends where it began.
	function scrub(l: string, final: boolean) {
		if (final) jump(l);
		else land(l);
	}
	$effect(() => {
		void sections;
		const l = pendingJump;
		if (!l || listQuery.isPending) return;
		tick().then(() => {
			if (land(l, true)) pendingJump = null;
		});
	});

	// ── Paging, both ways ──────────────────────────────────────────────────
	// After a jump the list can start mid-library: scrolling up fetches the
	// page before, and the scroll is moved by what was added above so the
	// artists in view stay put. The observers only record whether each end is
	// in range and the fetching follows from that, because an end that stays in
	// range while a page lands (a short page, a jump) gets no new entry.
	let pageSentinel = $state<HTMLDivElement | null>(null);
	let topSentinel = $state<HTMLDivElement | null>(null);
	let nearEnd = $state(false);
	let nearTop = $state(false);
	let settling = $state(false);
	const RANGE = 600;
	function watch(el: HTMLElement | null, set: (near: boolean) => void) {
		if (!el) return;
		// The page scrolls inside #main: with the window as the root, #main
		// would clip the margin away and a page would only load once its end
		// was on screen.
		const io = new IntersectionObserver((e) => set(!!e[0]?.isIntersecting), { root: mainEl, rootMargin: `${RANGE}px 0px` });
		io.observe(el);
		return () => {
			io.disconnect();
			set(false);
		};
	}
	// The observer's word arrives a frame late, after a page has already pushed
	// its end away, so the position is checked again before fetching.
	function inRange(el: HTMLElement | null) {
		if (!el || !mainEl) return false;
		const r = el.getBoundingClientRect();
		const m = mainEl.getBoundingClientRect();
		return r.bottom >= m.top - RANGE && r.top <= m.bottom + RANGE;
	}
	$effect(() => watch(pageSentinel, (v) => (nearEnd = v)));
	$effect(() => watch(topSentinel, (v) => (nearTop = v)));
	$effect(() => {
		if (!nearEnd || listQuery.isFetching || !listQuery.hasNextPage) return;
		untrack(() => inRange(pageSentinel) && listQuery.fetchNextPage());
	});
	$effect(() => {
		if (!nearTop || settling || gliding || listQuery.isFetching || !listQuery.hasPreviousPage) return;
		untrack(() => inRange(topSentinel) && loadPrevious());
	});
	async function loadPrevious() {
		settling = true;
		const before = mainEl?.scrollHeight ?? 0;
		try {
			await listQuery.fetchPreviousPage();
			await tick();
			if (mainEl) mainEl.scrollTop += mainEl.scrollHeight - before;
		} finally {
			settling = false;
		}
	}

	const artistsCount = (n: number) =>
		(n === 1 ? i18n.music_artists_one : i18n.music_artists_other)({ count: n.toLocaleString() });
	let visibleOf = $derived(
		i18n.music_visible_of({ visible: matched.toLocaleString(), total: (counts?.total ?? 0).toLocaleString() }),
	);
	let metaLine = $derived(
		[filtering ? visibleOf : artistsCount(counts?.total ?? 0), lastScan ? i18n.movies_last_scan({ when: lastScan }) : ""]
			.filter(Boolean)
			.join(" · "),
	);

	let tabs = $derived([
		{ key: "all", label: i18n.common_all(), count: counts?.status_total },
		{ key: "wanted", label: i18n.status_wanted(), count: counts?.wanted, dot: "bg-status-wanted" },
		{ key: "downloading", label: i18n.status_downloading(), count: counts?.downloading, dot: "bg-status-downloading" },
		{ key: "available", label: i18n.status_available(), count: counts?.available, dot: "bg-status-available" },
	]);
	let facets = $derived([
		{
			key: "monitored",
			label: i18n.music_monitoring(),
			icon: Eye,
			value: mon,
			options: [
				{ key: "all", label: i18n.music_mon_all() },
				{ key: "monitored", label: i18n.music_mon_monitored() },
				{ key: "unmonitored", label: i18n.music_mon_unmonitored() },
			],
			onChange: (v: string) => (mon = v),
		},
		{
			key: "genre",
			label: i18n.music_genre(),
			icon: Tags,
			value: genre,
			options: [
				{ key: "all", label: i18n.music_genre_all() },
				...(counts?.genres ?? []).map((g) => ({ key: g.name, label: g.name, count: g.count })),
				// A genre from a link that the counts no longer list stays choosable.
				...(genre !== "all" && !(counts?.genres ?? []).some((g) => g.name === genre) ? [{ key: genre, label: genre }] : []),
			],
			onChange: (v: string) => (genre = v),
		},
	]);
	const SORTS = [
		{ key: "title-asc", label: i18n.sort_name_az() },
		{ key: "title-desc", label: i18n.sort_name_za() },
		{ key: "recent-desc", label: i18n.library_sort_recent() },
		{ key: "recent-asc", label: i18n.sort_oldest_added() },
	];

	function reset() {
		status = "all";
		mon = "all";
		genre = "all";
		query = "";
	}

	const searchMissing = createAction<Artist>(() => ({
		run: (a) => api(`/music/artists/${a.id}/search-now`, { method: "POST" }),
		refresh: [],
		failed: searchFailed,
		succeeded: (a) => i18n.music_search_started({ title: a.name }),
	}));
	// The list's bookmark is all-or-nothing; the detail page's select picks
	// the finer policies.
	const toggleMonitor = createAction<Artist>(() => ({
		run: (a) => api(`/music/artists/${a.id}`, { method: "PATCH", body: { monitor: a.monitor === "none" ? "all" : "none" } }),
		refresh: MUSIC,
		failed: updateFailed,
		succeeded: (a) => monitorToast(a.monitor === "none"),
	}));
	let monitorPendingId = $derived(toggleMonitor.isPending ? (toggleMonitor.variables?.id ?? null) : null);

	const GRID =
		"grid grid-cols-3 gap-x-3 gap-y-6 md:grid-cols-[repeat(auto-fill,minmax(120px,1fr))] md:gap-x-4 md:gap-y-7 lg:gap-x-5";
</script>

<div class="flex flex-col">
	<p class="truncate px-4 pt-4 text-sm text-fg-muted md:hidden">{metaLine}</p>

	<LibraryToolbar
		statusLabel={i18n.filter_status()}
		{tabs}
		tab={status}
		onTabChange={(k) => (status = k)}
		{facets}
		foldFacets
		{query}
		onQueryChange={(q) => (query = q)}
		placeholder={i18n.music_filter_placeholder()}
		sorts={SORTS}
		sort="{sort}-{order}"
		onSortChange={setSort}
		onReset={reset}
		{view}
		onViewChange={(v) => (view = v)}
		add={{
			label: auth.canAddDirectly ? i18n.action_add_artist() : i18n.action_request_artist_title(),
			onClick: () => openAdd("artist"),
		}}
	/>

	<!-- The count row, from md (the phone has the line above the toolbar). -->
	<div class="hidden w-full flex-wrap items-baseline justify-between gap-2 px-6 pb-2 pt-4 font-mono text-[11px] text-fg-subtle md:flex">
		<div>{visibleOf}</div>
		<div class="flex flex-wrap items-center gap-2">
			{#if counts}<span>{i18n.music_releases_total({ count: counts.albums.toLocaleString() })}</span>{/if}
			{#if counts && lastScan}<span class="text-fg-faint">·</span>{/if}
			{#if lastScan}<span>{i18n.movies_last_scan({ when: lastScan })}</span>{/if}
		</div>
	</div>

	{#if listQuery.isLoading}
		<div class={cn(GRID, "px-4 pt-4 md:px-6")}>
			<SkeletonList variant="artist" count={14} />
		</div>
	{:else if listQuery.isError}
		<div class="w-full px-4 md:px-6">
			<div class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center">
				<p class="text-sm font-semibold text-status-failed">{i18n.music_load_failed()}</p>
				<p class="mt-1 text-xs text-fg-subtle">{errorText(listQuery.error, i18n.common_unknown_error())}</p>
			</div>
		</div>
	{:else if items.length === 0}
		<div class="w-full px-4 pt-3 md:px-6">
			<div class="rounded-lg border border-dashed border-border-strong py-12 text-center">
				<p class="text-sm font-semibold text-fg">{filtering ? i18n.music_empty_filter() : i18n.music_empty_library()}</p>
				{#if filtering}
					<button type="button" onclick={reset} class="mt-3 text-sm font-medium text-accent-text transition hover:text-fg">
						{i18n.common_clear_filters()}
					</button>
				{/if}
			</div>
		</div>
	{:else}
		<!-- No scroll anchoring here: a page added above is compensated by hand. -->
		<div bind:this={listEl} class="px-4 pb-10 [overflow-anchor:none] md:px-6">
			<div bind:this={topSentinel} aria-hidden="true"></div>
			{#if listQuery.hasPreviousPage}
				<!-- Holds its height while the page before loads, so nothing in view
				     moves until that page is in and the scroll has been made up. -->
				<p class="flex h-10 items-center justify-center font-mono text-[11px] text-fg-subtle">
					{#if listQuery.isFetchingPreviousPage}{i18n.common_loading()}{/if}
				</p>
			{/if}
			{#if shownView === "list"}
				<div class="pt-2">
					<ArtistTable
						{sections}
						canEdit={auth.canAddDirectly}
						onSearch={(a) => searchMissing.mutate(a)}
						onMonitor={(a) => toggleMonitor.mutate(a)}
						{monitorPendingId}
						anchorClass={ANCHOR}
					/>
				</div>
			{:else}
				{#each sections as s}
					<section data-letter={s.letter ?? undefined} class={cn(s.letter && ANCHOR, "pt-2 md:pt-1")}>
						{#if s.letter}
							<div
								class="sticky top-[124px] z-10 -mx-4 mb-3 flex items-center gap-3 bg-bg-deep/85 px-4 py-2.5 backdrop-blur-md md:top-16 md:-mx-6 md:px-6"
							>
								<h2 class="text-[20px] font-bold leading-none tracking-tight text-fg">{s.letter}</h2>
								{#if s.count != null}
									<span class="font-mono text-[11px] text-fg-faint">{artistsCount(s.count)}</span>
								{/if}
								<span class="h-px flex-1 bg-border" aria-hidden="true"></span>
							</div>
						{/if}
						<div class={cn(GRID, "pb-6", !s.letter && "pt-3")}>
							{#each s.items as a (a.id)}
								<ArtistCard
									artist={a}
									canEdit={auth.canAddDirectly}
									onSearch={() => searchMissing.mutate(a)}
									onMonitor={() => toggleMonitor.mutate(a)}
									monitorPending={monitorPendingId === a.id}
								/>
							{/each}
						</div>
					</section>
				{/each}
			{/if}
			<div bind:this={pageSentinel} aria-hidden="true"></div>
		</div>

		{#if lettered}
			<!-- The letters: a rail down the right edge from md, the fast scroller
			     on a phone. -->
			<LetterRail {letters} {current} onScrub={scrub} />
			{#if shownView === "grid"}
				<LetterScroller {letters} {progress} active={scrolling} onScrub={scrub} />
			{/if}
		{/if}
	{/if}
</div>
