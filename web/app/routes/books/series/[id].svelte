<script lang="ts">
	import { onMount } from "svelte";
	import { createQuery, useQueryClient } from "@tanstack/svelte-query";
	import { params } from "@roxi/routify";
	import { Bookmark, Search } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { toast } from "@lib/toast";
	import { cn } from "@lib/cn";
	import { formatDate, formatDateShort, formatRelative } from "@lib/dates";
	import MediaHero from "@components/shared/MediaHero.svelte";
	import MonitorSelect from "@components/shared/MonitorSelect.svelte";
	import StatusPill from "@components/shared/StatusPill.svelte";
	import LabelPill from "@components/shared/LabelPill.svelte";
	import Skeleton from "@components/shared/Skeleton.svelte";
	import BookCover from "@components/books/BookCover.svelte";
	import LibraryActions from "@components/shared/LibraryActions.svelte";
	import PeopleGrid from "@components/shared/PeopleGrid.svelte";
	import InfoCard, { type InfoRow } from "@components/shared/InfoCard.svelte";
	import {
		bookPeople,
		bookRoleLabel,
		kindLabel,
		languageName,
		personHref,
		personPhoto,
		volumeLabel,
		type BookSeries,
		type SeriesMonitor,
	} from "@lib/music-books";
	import { bookPosterUrl } from "@lib/posters";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Where a stacked BD or manga card leads: monitoring and edition are set
	// once for the whole series, and the volumes sit in a grid.
	let id = $state("");
	onMount(() => params.subscribe((p) => (id = p.id ?? "")));

	const seriesQuery = createQuery<BookSeries>(() => ({
		queryKey: ["books", "series", id],
		queryFn: () => api<BookSeries>(`/books/series/${id}`),
		enabled: !!id,
	}));
	let s = $derived(seriesQuery.data);
	let canEdit = $derived(auth.canAddDirectly);
	let out = $derived(s ? s.volumes.filter((v) => v.status !== "upcoming") : []);
	let have = $derived(out.filter((v) => v.status === "available").length);
	let wanted = $derived(out.filter((v) => v.status === "wanted").length);
	let next = $derived(s?.volumes.find((v) => v.status === "upcoming"));
	let tab = $state<string>("overview");

	const MONITOR: { key: SeriesMonitor; label: string }[] = [
		{ key: "all", label: i18n.books_monitor_volumes_all() },
		{ key: "future", label: i18n.books_monitor_volumes_future() },
		{ key: "none", label: i18n.books_monitor_none() },
	];

	const qc = useQueryClient();
	const invalidate = () => qc.invalidateQueries({ queryKey: ["books"] });
	async function patch(body: Record<string, string>) {
		await api(`/books/series/${id}`, { method: "PATCH", body });
		invalidate();
	}
	let lastMonitor: SeriesMonitor = "all";
	function toggleMonitor() {
		if (!s) return;
		if (s.monitor === "none") patch({ monitor: lastMonitor });
		else {
			lastMonitor = s.monitor;
			patch({ monitor: "none" });
		}
	}
	async function searchMissing() {
		if (!s) return;
		await api(`/books/series/${id}/search`, { method: "POST" });
		toast.ok(i18n.music_search_started({ title: s.title }));
	}

	let meta = $derived.by(() => {
		if (!s) return [];
		const p = [s.author, i18n.books_volumes_out({ count: String(out.length) })];
		if (next?.release_date) p.push(i18n.books_next_volume({ n: String(next.number), date: formatDate(next.release_date) }));
		return p;
	});
	const caps = "font-mono text-[11px] uppercase tracking-[0.08em] text-fg-muted";
	const h3 = "font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint";

	// The first volume's poster is the series' cover.
	let coverSrc = $derived(s?.volumes[0] ? bookPosterUrl(s.volumes[0].id) : undefined);
	let people = $derived(
		(s ? bookPeople(s.contributors ?? []) : []).map((x) => ({
			key: `${x.role}:${x.person.name}`,
			name: x.person.name,
			photo: personPhoto(x.person, x.role),
			href: personHref(x.person),
			role: bookRoleLabel(x.role),
			note: x.languages.map((l) => languageName(l, true)).join(", "),
		})),
	);
	let detailRows = $derived.by<InfoRow[]>(() => {
		if (!s) return [];
		const rows: InfoRow[] = [
			{ label: i18n.common_type(), value: kindLabel(s.kind), mono: false },
			{ label: i18n.common_status(), value: s.ongoing ? i18n.books_ongoing() : i18n.books_completed(), mono: false },
			{ label: i18n.lookup_first_published(), value: String(s.since) },
			{ label: i18n.books_fact_published(), value: String(out.length) },
		];
		if (next) rows.push({ label: i18n.books_fact_next_volume(), value: [volumeLabel(next.number), formatDateShort(next.release_date)].filter(Boolean).join(" · "), mono: false });
		if (s.original_title) rows.push({ label: i18n.books_fact_original_title(), value: s.original_title, mono: false });
		if (s.rating) rows.push({ label: i18n.books_fact_rating(), value: `★ ${s.rating.toFixed(1)}` });
		if (s.hardcover_id)
			rows.push({ label: "Hardcover", value: [{ text: String(s.hardcover_id), href: `https://hardcover.app/series/${s.hardcover_id}`, external: true }], mono: true });
		return rows;
	});
	let libraryRows = $derived.by<InfoRow[]>(() => {
		if (!s) return [];
		const rows: InfoRow[] = [
			{ label: i18n.quality_profile(), value: s.quality_profile || i18n.quality_server_default() },
			{ label: i18n.action_monitor(), value: MONITOR.find((o) => o.key === s.monitor)?.label ?? "", mono: false },
			{ label: i18n.books_edition(), value: s.edition, mono: false },
			{ label: i18n.books_volumes(), value: `${have} / ${out.length}` },
		];
		if (wanted) rows.push({ label: i18n.status_wanted(), value: String(wanted), tone: "text-status-wanted" });
		rows.push({ label: i18n.common_added(), value: formatRelative(s.added_at) });
		return rows;
	});
</script>

{#if seriesQuery.isLoading || !id}
	<span class="sr-only" role="status">{i18n.common_loading()}</span>
	<div class="px-4 pt-6 md:px-8">
		<Skeleton w="80px" h={28} />
		<div class="mt-8 flex flex-col items-center gap-6 md:flex-row md:items-end">
			<div class="aspect-[2/3] w-40 animate-pulse rounded-lg bg-white/[0.06] md:w-[170px]"></div>
			<div class="flex w-full flex-col gap-3">
				<Skeleton w="60%" h={36} />
				<Skeleton w="40%" h={14} />
			</div>
		</div>
	</div>
{:else if seriesQuery.isError || !s}
	<div class="px-4 pt-6 md:px-8">
		<div class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center">
			<p class="text-sm font-semibold text-status-failed">{i18n.books_load_series_failed()}</p>
			<p class="mt-1 text-xs text-fg-subtle">{errorText(seriesQuery.error, i18n.common_unknown_error())}</p>
		</div>
	</div>
{:else}
	<div class={cn(canEdit && wanted > 0 && "pb-24 md:pb-0")}>
		<MediaHero
			backdrop={coverSrc}
			backHref="/books"
			backLabel={i18n.books_label()}
			cols="md:grid-cols-[170px_1fr] lg:grid-cols-[200px_1fr]"
			artWrap="w-40"
			labelId="series-title"
			title={s.title}
			originalTitle={s.original_title}
			{meta}
			overview={s.overview}
		>
			{#snippet art()}
				<div class="shadow-[0_24px_48px_rgb(0_0_0_/0.5)]">
					<BookCover src={coverSrc} alt={i18n.common_poster_alt({ title: s.title })} />
				</div>
			{/snippet}
			{#snippet pills()}
				<StatusPill status={s.status} size="md" variant="translucent" />
				<span class={caps}>{kindLabel(s.kind)}</span>
				<span class="text-fg-faint" aria-hidden="true">·</span>
				<span class={caps}>{s.ongoing ? i18n.books_ongoing() : i18n.books_completed()}</span>
				<span class="text-fg-faint" aria-hidden="true">·</span>
				<span class={caps}>{i18n.music_since({ year: String(s.since) })}</span>
			{/snippet}
			{#snippet extra()}
				<div class="mt-5 max-w-[560px]">
					<div class="mb-2 font-mono text-xs text-fg-muted">
						<span class="text-fg">{have}</span> / {out.length}
						<span class="text-fg-subtle">{i18n.books_volumes_word()}</span>
						{#if wanted}
							<span class="text-fg-faint"> · </span><span class="text-status-wanted">{i18n.nav_count_wanted_other({ count: String(wanted) })}</span>
						{/if}
					</div>
					<div class="flex h-[3px] w-full overflow-hidden rounded-full bg-white/[0.08]" aria-hidden="true">
						<div class="bg-status-available" style:width="{out.length ? (have / out.length) * 100 : 0}%"></div>
					</div>
				</div>
			{/snippet}
			{#snippet actions()}
				{#if canEdit}
					<!-- Two lines from md, as on the book page: what the series follows
					     (label, select, bookmark), then the search and the menu. With
					     nothing to search the menu stays on the first line. One flow in
					     DOM order, so a phone keeps label over select + bookmark + menu;
					     the search lives in the pinned bar there. The edition is a
					     setting, not an action, and keeps its own line. -->
					<div class="flex flex-wrap items-center gap-2 md:gap-x-2.5 md:gap-y-3">
						<span class="basis-full font-mono text-[10.5px] uppercase tracking-[0.1em] text-fg-subtle md:basis-auto">{i18n.action_monitor()}</span>
						<MonitorSelect value={s.monitor} options={MONITOR} onChange={(v) => patch({ monitor: v })} label={i18n.action_monitor()} class="flex-1 md:flex-none" />
						<button
							type="button"
							onclick={toggleMonitor}
							aria-pressed={s.monitor !== "none"}
							aria-label={s.monitor !== "none" ? i18n.action_stop_monitoring() : i18n.action_monitor()}
							class={cn(
								"grid h-11 w-11 shrink-0 place-items-center rounded-lg border border-border bg-bg-elevated/80 transition hover:border-border-strong",
								s.monitor !== "none" ? "text-accent-text" : "text-fg-subtle",
							)}
						>
							<Bookmark size={16} fill={s.monitor !== "none" ? "currentColor" : "none"} aria-hidden="true" />
						</button>
						{#if wanted > 0}
							<span class="hidden h-0 basis-full md:block" aria-hidden="true"></span>
							<button
								type="button"
								onclick={searchMissing}
								class="hidden h-11 items-center gap-2 rounded-lg bg-accent px-4 text-[14px] font-semibold text-fg-on-accent transition hover:bg-accent-hover md:inline-flex"
							>
								<Search size={16} aria-hidden="true" />
								{i18n.music_search_missing()}
							</button>
						{/if}
						<LibraryActions
							base={`/books/series/${s.id}`}
							title={s.title}
							media="books"
							queryKey="books"
							backHref="/books"
							hasFiles={have > 0}
							profile={s.quality_profile}
							removeBody={i18n.books_series_remove_body()}
							filesLabel={i18n.books_series_delete_files_label()}
						/>
					</div>
					<div class="flex flex-col gap-2 md:flex-row md:items-center md:gap-2.5">
						<span class="font-mono text-[10.5px] uppercase tracking-[0.1em] text-fg-subtle">{i18n.books_edition()}</span>
						<MonitorSelect
							value={s.edition}
							options={s.editions.map((e) => ({ key: e, label: e }))}
							onChange={(v) => patch({ edition: v })}
							label={i18n.books_edition()}
							class="w-full md:w-auto"
						/>
					</div>
				{/if}
			{/snippet}
		</MediaHero>

		<nav class="sticky top-16 z-10 border-b border-border bg-bg-deep/70 px-4 backdrop-blur-md saturate-150 md:px-8">
			<div class="flex w-full gap-0.5">
				{#each [{ key: "overview", label: i18n.common_overview(), n: "" }, { key: "volumes", label: i18n.books_volumes(), n: `${have}/${out.length}` }] as t (t.key)}
					{@const active = tab === t.key}
					<button
						type="button"
						onclick={() => (tab = t.key)}
						aria-current={active ? "page" : undefined}
						class={cn("relative -mb-px shrink-0 px-3 py-3.5 text-[13px] font-medium transition md:px-4", active ? "text-fg" : "text-fg-subtle hover:text-fg")}
					>
						<span>{t.label}</span>
						{#if t.n}<span class="ml-1.5 font-mono text-[11px] text-fg-faint">{t.n}</span>{/if}
						{#if active}<span aria-hidden="true" class="absolute inset-x-3 -bottom-px h-0.5 rounded-t-sm bg-accent"></span>{/if}
					</button>
				{/each}
			</div>
		</nav>

		{#if tab === "volumes"}
			<ol class="grid grid-cols-4 gap-x-2.5 gap-y-4 px-4 py-5 sm:grid-cols-5 md:grid-cols-6 md:gap-x-3 md:gap-y-5 md:px-8 md:py-6 lg:grid-cols-8">
				{#each s.volumes as v (v.number)}
					{@const up = v.status === "upcoming"}
					{@const missing = v.status === "wanted"}
					<li>
						<div class="relative">
							<BookCover src={bookPosterUrl(v.id)} dim={up || missing} dashed={up} class="rounded-md" />
							{#if up && v.release_date}
								<span class="absolute bottom-1.5 left-1.5"><LabelPill token="unaired" label={formatDateShort(v.release_date)} /></span>
							{:else if missing}
								<span class="absolute bottom-1.5 left-1.5"><LabelPill token="wanted" label={i18n.status_wanted()} /></span>
							{/if}
						</div>
						<p class={cn("mt-1.5 font-mono text-[11px]", missing ? "text-status-wanted" : up ? "text-fg-subtle" : "text-fg-muted")}>
							{volumeLabel(v.number)}
						</p>
					</li>
				{/each}
			</ol>
		{:else}
			<div class="grid gap-6 px-4 py-6 md:grid-cols-[1fr_260px] md:gap-7 md:px-8 lg:grid-cols-[1fr_320px] lg:gap-10">
				<section class="min-w-0" aria-labelledby="series-synopsis">
					<h2 id="series-synopsis" class={h3}>{i18n.detail_synopsis()}</h2>
					<p class="mt-3 max-w-[720px] text-sm leading-relaxed text-fg-muted [text-wrap:pretty]">
						{s.overview ?? i18n.music_no_overview()}
					</p>
					{#if people.length > 0}
						<div class="my-5 h-px bg-border"></div>
						<h3 class="mb-3 {h3}">{i18n.books_contributors()}</h3>
						<PeopleGrid {people} dense />
					{/if}
				</section>
				<aside class="flex min-w-0 flex-col gap-4">
					<InfoCard id="series-details" title={i18n.common_details()} rows={detailRows} />
					<InfoCard id="series-library" title={i18n.nav_library()} rows={libraryRows} />
				</aside>
			</div>
		{/if}

		{#if canEdit && wanted > 0}
			<div
				class="fixed inset-x-0 bottom-[calc(env(safe-area-inset-bottom)+3.5rem)] z-30 flex items-center gap-2 border-t border-border bg-bg-elevated/95 px-3 pb-4 pt-2.5 backdrop-blur-md md:hidden"
			>
				<button
					type="button"
					onclick={searchMissing}
					class="inline-flex h-11 min-w-0 flex-1 items-center justify-center gap-2 rounded-lg bg-accent text-[14px] font-semibold text-fg-on-accent"
				>
					<Search size={16} aria-hidden="true" />
					{i18n.music_search_missing()}
				</button>
			</div>
		{/if}
	</div>
{/if}
