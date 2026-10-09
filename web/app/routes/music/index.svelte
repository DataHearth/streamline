<script lang="ts">
	import { onMount } from "svelte";
	import { createQuery, keepPreviousData, useQueryClient } from "@tanstack/svelte-query";
	import { Eye } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { toast } from "@lib/toast";
	import { formatRelative } from "@lib/dates";
	import { onRouteQuery } from "@lib/route-query";
	import LibraryToolbar from "@components/shared/LibraryToolbar.svelte";
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import ArtistRow from "@components/music/ArtistRow.svelte";
	import type { Artist, ArtistCounts, ArtistList } from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	const STATUSES = new Set(["all", "wanted", "downloading", "available"]);
	const MONS = new Set(["all", "monitored", "unmonitored"]);

	let status = $state("all");
	let mon = $state("all");
	let query = $state("");
	let debounced = $state("");
	let sort = $state("added");

	onMount(() =>
		onRouteQuery("/music", (p) => {
			const s = p.get("status") ?? "all";
			const m = p.get("monitored") ?? "all";
			status = STATUSES.has(s) ? s : "all";
			mon = MONS.has(m) ? m : "all";
			query = debounced = p.get("q") ?? "";
			sort = p.get("sort") === "name" ? "name" : "added";
		}),
	);
	$effect(() => {
		const q = query;
		const t = setTimeout(() => (debounced = q), 250);
		return () => clearTimeout(t);
	});
	$effect(() => {
		const p = new URLSearchParams();
		if (status !== "all") p.set("status", status);
		if (mon !== "all") p.set("monitored", mon);
		if (query) p.set("q", query);
		if (sort !== "added") p.set("sort", sort);
		const search = p.toString();
		if (typeof window === "undefined" || window.location.pathname !== "/music") return;
		const next = `/music${search ? `?${search}` : ""}`;
		if (next !== window.location.pathname + window.location.search) window.history.replaceState(null, "", next);
	});

	const listQuery = createQuery<ArtistList>(() => ({
		queryKey: ["music", "artists", status, mon, debounced, sort],
		queryFn: () => {
			const p = new URLSearchParams({ sort });
			if (status !== "all") p.set("status", status);
			if (mon !== "all") p.set("monitored", mon);
			if (debounced.trim()) p.set("query", debounced.trim());
			return api<ArtistList>(`/music/artists?${p}`);
		},
		placeholderData: keepPreviousData,
	}));
	const countsQuery = createQuery<ArtistCounts>(() => ({
		queryKey: ["music", "artists", "counts"],
		queryFn: () => api<ArtistCounts>("/music/artists/counts"),
	}));

	let items = $derived(listQuery.data?.items ?? []);
	let counts = $derived(countsQuery.data);
	let filtering = $derived(status !== "all" || mon !== "all" || !!debounced);
	let lastScan = $derived(counts?.last_scan ? formatRelative(counts.last_scan) : "");
	const artistsCount = (n: number) =>
		(n === 1 ? i18n.music_artists_one : i18n.music_artists_other)({ count: n.toLocaleString() });
	let metaLine = $derived(
		[
			filtering
				? i18n.music_visible_of({ visible: String(items.length), total: String(counts?.total ?? 0) })
				: artistsCount(counts?.total ?? 0),
			lastScan ? i18n.movies_last_scan({ when: lastScan }) : "",
		]
			.filter(Boolean)
			.join(" · "),
	);

	let tabs = $derived([
		{ key: "all", label: i18n.common_all(), count: counts?.total },
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
	]);
	const SORTS = [
		{ key: "added", label: i18n.library_sort_recent() },
		{ key: "name", label: i18n.sort_name_az() },
	];

	function reset() {
		status = "all";
		mon = "all";
		query = "";
	}

	const qc = useQueryClient();
	async function searchMissing(a: Artist) {
		await api(`/music/artists/${a.id}/search`, { method: "POST" });
		toast.ok(i18n.music_search_started({ title: a.name }));
	}
	async function toggleMonitor(a: Artist) {
		await api(`/music/artists/${a.id}`, { method: "PATCH", body: { monitor: a.monitor === "none" ? "all" : "none" } });
		qc.invalidateQueries({ queryKey: ["music"] });
	}
</script>

<div class="flex flex-col">
	<p class="truncate px-4 pt-4 text-sm text-fg-muted md:hidden">{metaLine}</p>

	<LibraryToolbar
		statusLabel={i18n.filter_status()}
		{tabs}
		tab={status}
		onTabChange={(k) => (status = k)}
		{facets}
		{query}
		onQueryChange={(q) => (query = q)}
		placeholder={i18n.music_filter_placeholder()}
		sorts={SORTS}
		{sort}
		onSortChange={(k) => (sort = k)}
		onReset={reset}
	/>

	<div class="hidden w-full flex-wrap items-baseline justify-between gap-2 px-6 pb-2 pt-4 font-mono text-[11px] text-fg-subtle md:flex">
		<div>{i18n.music_visible_of({ visible: String(items.length), total: String(counts?.total ?? 0) })}</div>
		<div>
			{#if counts}{i18n.music_releases_total({ count: counts.releases.toLocaleString() })}{/if}{#if lastScan} · {i18n.movies_last_scan({ when: lastScan })}{/if}
		</div>
	</div>

	{#if listQuery.isLoading}
		<span class="sr-only" role="status">{i18n.common_loading()}</span>
		<div class="border-t border-border"><SkeletonList variant="media-row" count={6} /></div>
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
		<div class="border-t border-border pb-6 md:mt-1">
			{#each items as a (a.id)}
				<ArtistRow
					artist={a}
					canEdit={auth.canAddDirectly}
					onSearch={() => searchMissing(a)}
					onMonitor={() => toggleMonitor(a)}
				/>
			{/each}
		</div>
	{/if}
</div>
