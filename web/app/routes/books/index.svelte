<script lang="ts">
	import { pageTitle } from "@lib/page-title.svelte";
	import { onMount } from "svelte";
	import { createQuery, keepPreviousData } from "@tanstack/svelte-query";
	import { ArrowLeft, Layers, UserRound } from "@lucide/svelte";
	import { api, apiAllPages, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { formatRelative } from "@lib/dates";
	import { onRouteQuery } from "@lib/route-query";
	import LibraryToolbar from "@components/shared/LibraryToolbar.svelte";
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import Shelf from "@components/books/Shelf.svelte";
	import ShelfCard from "@components/books/ShelfCard.svelte";
	import type { BookCounts, ShelfItem } from "@lib/music-books";
	import type { ScheduleList } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Shelves by kind; "See all" turns the page into that kind's full grid,
	// with ?kind= in the URL so back returns to the shelves.
	type Kind = "all" | "novel" | "bd" | "manga";
	const KINDS = new Set(["novel", "bd", "manga"]);
	const STATUSES = new Set(["all", "wanted", "downloading", "available"]);

	let kind = $state<Kind>("all");
	let status = $state("all");
	let author = $state("all");
	let format = $state("all");
	let query = $state("");
	let debounced = $state("");
	let sort = $state("added");

	function apply(p: URLSearchParams) {
		const k = p.get("kind") ?? "";
		const s = p.get("status") ?? "all";
		kind = (KINDS.has(k) ? k : "all") as Kind;
		status = STATUSES.has(s) ? s : "all";
		author = p.get("author") ?? "all";
		format = ["ebook", "audiobook"].includes(p.get("format") ?? "") ? (p.get("format") as string) : "all";
		query = debounced = p.get("q") ?? "";
		sort = p.get("sort") === "title" ? "title" : "added";
	}
	function params() {
		const p = new URLSearchParams();
		if (kind !== "all") p.set("kind", kind);
		if (status !== "all") p.set("status", status);
		if (author !== "all") p.set("author", author);
		if (format !== "all") p.set("format", format);
		if (query) p.set("q", query);
		if (sort !== "added") p.set("sort", sort);
		return p.toString();
	}
	onMount(() => {
		const off = onRouteQuery("/books", apply);
		const pop = () => {
			if (window.location.pathname === "/books") apply(new URLSearchParams(window.location.search));
		};
		window.addEventListener("popstate", pop);
		return () => {
			off();
			window.removeEventListener("popstate", pop);
		};
	});
	$effect(() => {
		const q = query;
		const t = setTimeout(() => (debounced = q), 250);
		return () => clearTimeout(t);
	});
	$effect(() => {
		const search = params();
		if (typeof window === "undefined" || window.location.pathname !== "/books") return;
		const next = `/books${search ? `?${search}` : ""}`;
		if (next !== window.location.pathname + window.location.search) window.history.replaceState(null, "", next);
	});
	function openKind(k: Kind) {
		kind = k;
		const search = params();
		window.history.pushState(null, "", `/books${search ? `?${search}` : ""}`);
		document.getElementById("main")?.scrollTo({ top: 0 });
	}

	// The server's kind filter for the page: the BD shelf holds comics too.
	const KIND_PARAM: Record<Kind, string> = { all: "", novel: "novel", bd: "bd,comic", manga: "manga" };
	function filterParams(withKind: boolean) {
		const p = new URLSearchParams();
		if (status !== "all") p.set("status", status);
		if (author !== "all") p.set("author", author);
		if (format !== "all") p.set("format", format);
		if (withKind && KIND_PARAM[kind]) p.set("kind", KIND_PARAM[kind]);
		if (debounced.trim()) p.set("query", debounced.trim());
		return p;
	}

	// The landing page folds every kind into its shelves, so it needs the whole
	// matching shelf; a kind's grid asks the server for that kind alone.
	const listQuery = createQuery<{ items: ShelfItem[]; total: number }>(() => ({
		queryKey: ["books", "list", kind, status, author, format, debounced, sort],
		queryFn: () => {
			const p = filterParams(true);
			p.set("sort", sort);
			return apiAllPages<ShelfItem>(`/books?${p}`);
		},
		placeholderData: keepPreviousData,
	}));
	// Carries the page's filters so the facet rows count against them; the nav
	// badge keeps the unfiltered ["books", "counts"] entry.
	const countsQuery = createQuery<BookCounts>(() => ({
		queryKey: ["books", "counts", { kind, status, author, format, query: debounced }],
		queryFn: () => {
			const qs = filterParams(true).toString();
			return api<BookCounts>(`/books/counts${qs ? `?${qs}` : ""}`);
		},
		placeholderData: keepPreviousData,
	}));
	const schedulesQuery = createQuery<ScheduleList>(() => ({
		queryKey: ["schedules"],
		queryFn: () => api<ScheduleList>("/schedules"),
		enabled: auth.isAdmin,
	}));

	let items = $derived(listQuery.data?.items ?? []);
	let counts = $derived(countsQuery.data);
	const inKind = (i: ShelfItem, k: Kind) =>
		k === "all" || (k === "bd" ? i.kind === "bd" || i.kind === "comic" : i.kind === k);
	let shelves = $derived(
		(
			[
				["novel", i18n.books_shelf_novel()],
				["bd", i18n.books_shelf_bd()],
				["manga", i18n.books_shelf_manga()],
			] as [Kind, string][]
		)
			.map(([k, title]) => ({ k, title, items: items.filter((i) => inKind(i, k)) }))
			.filter((s) => s.items.length > 0),
	);
	let recent = $derived([...items].sort((a, b) => b.added_at.localeCompare(a.added_at)).slice(0, 8));
	let kindItems = $derived(items.filter((i) => inKind(i, kind)));
	let kindTitle = $derived(
		kind === "novel" ? i18n.books_shelf_novel() : kind === "bd" ? i18n.books_shelf_bd() : kind === "manga" ? i18n.books_shelf_manga() : "",
	);
	let shown = $derived(kind === "all" ? items : kindItems);
	let filtering = $derived(status !== "all" || author !== "all" || format !== "all" || !!debounced);
	let lastFinished = $derived.by(() => {
		let latest: string | null = null;
		for (const s of schedulesQuery.data?.items ?? [])
			if (s.last_finished_at && (!latest || s.last_finished_at > latest)) latest = s.last_finished_at;
		return latest;
	});
	let lastScan = $derived(lastFinished ? formatRelative(lastFinished) : "");

	let tabs = $derived([
		{ key: "all", label: i18n.common_all(), count: counts?.status_total },
		{ key: "wanted", label: i18n.status_wanted(), count: counts?.wanted, dot: "bg-status-wanted" },
		{ key: "downloading", label: i18n.status_downloading(), count: counts?.downloading, dot: "bg-status-downloading" },
		{ key: "available", label: i18n.status_available(), count: counts?.available, dot: "bg-status-available" },
	]);
	let facets = $derived([
		{
			key: "author",
			label: i18n.books_author(),
			icon: UserRound,
			value: author,
			options: [{ key: "all", label: i18n.books_any_author() }, ...(counts?.authors ?? []).map((a) => ({ key: a.name, label: a.name, count: a.count }))],
			onChange: (v: string) => (author = v),
		},
		{
			key: "format",
			label: i18n.books_format(),
			icon: Layers,
			value: format,
			options: [
				{ key: "all", label: i18n.books_any_format() },
				{ key: "ebook", label: i18n.books_format_ebook(), count: counts?.ebook },
				{ key: "audiobook", label: i18n.books_format_audiobook(), count: counts?.audiobook },
			],
			onChange: (v: string) => (format = v),
		},
	]);
	const SORTS = [
		{ key: "added", label: i18n.library_sort_recent() },
		{ key: "title", label: i18n.library_sort_title() },
	];
	function reset() {
		status = "all";
		author = "all";
		format = "all";
		query = "";
	}
	let metaLine = $derived(
		[
			i18n.movies_visible_of({ visible: String(shown.length), total: String(counts?.total ?? 0) }),
			lastScan ? i18n.movies_last_scan({ when: lastScan }) : "",
		]
			.filter(Boolean)
			.join(" · "),
	);
	$effect(() => pageTitle.claim(kind === "all" ? i18n.books_label() : kindTitle));
</script>

<div class="flex flex-col pb-6">
	<header class={kind !== "all" ? "w-full px-4 pt-4 md:px-6 md:pt-5" : "w-full px-4 pt-4 md:hidden"}>
		{#if kind !== "all"}
			<button
				type="button"
				onclick={() => openKind("all")}
				class="touch-hit mb-3 inline-flex items-center gap-1.5 rounded-full border border-border bg-black/40 px-3 py-1.5 text-[11.5px] font-medium text-fg-muted transition hover:bg-black/60 hover:text-fg"
			>
				<ArrowLeft size={13} aria-hidden="true" />
				{i18n.books_label()}
			</button>
		{/if}
		<p class="truncate text-sm text-fg-muted md:hidden">{metaLine}</p>
	</header>

	<LibraryToolbar
		statusLabel={i18n.filter_status()}
		{tabs}
		tab={status}
		onTabChange={(k) => (status = k)}
		{facets}
		{query}
		onQueryChange={(q) => (query = q)}
		placeholder={i18n.books_filter_placeholder()}
		sorts={SORTS}
		{sort}
		onSortChange={(k) => (sort = k)}
		onReset={reset}
	/>

	<div class="hidden w-full flex-wrap items-baseline justify-between gap-2 px-6 pb-1 pt-4 font-mono text-[11px] text-fg-subtle md:flex">
		<div>{i18n.movies_visible_of({ visible: String(shown.length), total: String(counts?.total ?? 0) })}</div>
		<div>{#if lastScan}{i18n.movies_last_scan({ when: lastScan })}{/if}</div>
	</div>

	{#if listQuery.isLoading}
		<span class="sr-only" role="status">{i18n.common_loading()}</span>
		<div class="grid w-full grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-x-4 gap-y-6 px-4 pt-4 md:grid-cols-[repeat(auto-fill,minmax(170px,1fr))] md:px-6">
			<SkeletonList variant="poster" count={8} />
		</div>
	{:else if listQuery.isError}
		<div class="w-full px-4 pt-4 md:px-6">
			<div class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center">
				<p class="text-sm font-semibold text-status-failed">{i18n.books_load_failed()}</p>
				<p class="mt-1 text-xs text-fg-subtle">{errorText(listQuery.error, i18n.common_unknown_error())}</p>
			</div>
		</div>
	{:else if shown.length === 0}
		<div class="w-full px-4 pt-4 md:px-6">
			<div class="rounded-lg border border-dashed border-border-strong py-12 text-center">
				<p class="text-sm font-semibold text-fg">{filtering ? i18n.books_empty_filter() : i18n.books_empty_library()}</p>
				{#if filtering}
					<button type="button" onclick={reset} class="mt-3 text-sm font-medium text-accent-text transition hover:text-fg">
						{i18n.common_clear_filters()}
					</button>
				{/if}
			</div>
		</div>
	{:else if kind === "all"}
		<Shelf title={i18n.books_shelf_recent()} items={recent} />
		{#each shelves as s (s.k)}
			<Shelf title={s.title} items={s.items} seeAllHref={`/books?kind=${s.k}`} onSeeAll={() => openKind(s.k)} />
		{/each}
	{:else}
		<div class="grid w-full grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-x-4 gap-y-6 px-4 pt-5 md:grid-cols-[repeat(auto-fill,minmax(170px,1fr))] md:px-6">
			{#each kindItems as it (`${it.type}-${it.id}`)}
				<ShelfCard item={it} />
			{/each}
		</div>
	{/if}
</div>
