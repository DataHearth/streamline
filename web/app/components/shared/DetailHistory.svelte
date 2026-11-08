<script lang="ts">
	import { DOWNLOAD_CHIP, DOWNLOAD_CHIP_ARROW } from "@lib/title-downloads";
	import { createQuery } from "@tanstack/svelte-query";
	import { Activity, ChevronRight } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import {
		EVENT_MARKS,
		eventDownloadHref,
		eventSubject,
		monitoringDetail,
	} from "@lib/activity-event";
	import { targetHref } from "@lib/routes";
	import { formatRelative, formatDateTime } from "@lib/dates";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import type { ActivityEvent, ActivityList } from "@lib/types";

	// Exactly one owner: the page's own title. A series, an artist and a book
	// series also take in the rows of their episodes, albums and volumes.
	let {
		movieId,
		seriesId,
		artistId,
		bookId,
		bookSeriesId,
	}: {
		movieId?: number;
		seriesId?: number;
		artistId?: number;
		bookId?: number;
		bookSeriesId?: number;
	} = $props();

	let filter = $derived(
		movieId !== undefined
			? `movie_id=${movieId}`
			: seriesId !== undefined
				? `series_id=${seriesId}`
				: artistId !== undefined
					? `artist_id=${artistId}`
					: bookId !== undefined
						? `book_id=${bookId}`
						: `book_series_id=${bookSeriesId}`,
	);

	const q = createQuery<ActivityList>(() => ({
		queryKey: ["detail-history", filter],
		queryFn: () => api<ActivityList>(`/activity?${filter}&limit=50`),
	}));



	function release(event: ActivityEvent): string {
		const p = event.payload ?? {};
		const v = p.release_title;
		return typeof v === "string" ? v : "";
	}

	function size(event: ActivityEvent): string {
		const p = event.payload ?? {};
		const v = p.size;
		return typeof v === "string" ? v : "";
	}

	let events = $derived(q.data?.events ?? []);
</script>

{#if q.isLoading}
	<ul aria-hidden="true" class="flex flex-col gap-2">
		{#each Array(5) as _}
			<li
				class="h-14 animate-pulse rounded-md bg-bg-card/50 motion-reduce:animate-none"
			></li>
		{/each}
	</ul>
{:else if q.isError}
	<div
		role="alert"
		class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-10 text-center text-sm text-status-failed"
	>
		{errorText(q.error, i18n.detail_history_failed())}
	</div>
{:else if events.length === 0}
	<div
		class="rounded-lg border border-dashed border-border bg-bg-elevated/40 py-12 text-center"
	>
		<Activity
			size={28}
			class="mx-auto mb-3 text-fg-faint"
			aria-hidden="true"
		/>
		<p class="text-sm font-medium text-fg">{i18n.common_no_history()}</p>
		<p class="mt-1 text-xs text-fg-muted">
			{i18n.activity_history_help()}
		</p>
	</div>
{:else}
	<!-- biome-ignore lint/a11y/noRedundantRoles: Safari's VoiceOver drops the list role from a list-style: none list -->
	<ul role="list" class="overflow-hidden rounded-lg border border-border bg-bg-elevated">
		{#each events as event (event.id)}
			{@const mark = EVENT_MARKS[event.type] ?? {
				icon: Activity,
				bg: "bg-surface-2",
				fg: "text-fg-muted",
				label: event.type.toUpperCase(),
			}}
			{@const rel = release(event) || monitoringDetail(event)}
			{@const sz = size(event)}
			<!-- The subject's title is the page we are already on, so only its
			     qualifier carries anything here: which episode, which season, how
			     many — or which album, which volume, which slot. Without it every
			     scan, import and completion on a show read as a bare event name
			     repeated down the list. -->
			{@const subject = eventSubject(event)}
			{@const part = subject.detail}
			<!-- The part an event is about (the episode, the season, the album,
			     the volume) is on this page or one step below it; the download it
			     came from is on Activity. -->
			{@const partHref = targetHref(subject.part)}
			{@const downloadHref = eventDownloadHref(event)}
			<li
				class="grid grid-cols-[28px_1fr_auto_auto_auto] items-center gap-4 border-b border-border px-4 py-3 last:border-b-0"
			>
				<span
					class={`grid h-6 w-6 place-items-center rounded-sm ${mark.bg} ${mark.fg}`}
				>
					<mark.icon size={13} aria-hidden="true" />
				</span>
				<div class="min-w-0">
					<div class="font-mono text-[10px] tracking-[0.1em] text-fg-faint">
						{mark.label}
					</div>
					{#if part}
						{#if partHref}
							<a
								href={partHref}
								class="mt-0.5 block truncate font-mono text-[11.5px] text-fg underline-offset-2 hover:text-accent-text hover:underline"
							>
								{part}
							</a>
						{:else}
							<div class="mt-0.5 truncate font-mono text-[11.5px] text-fg">
								{part}
							</div>
						{/if}
					{/if}
					{#if rel}
						<div class="mt-0.5 truncate font-mono text-[11.5px] text-fg-muted">
							{rel}
						</div>
					{/if}
				</div>
				<time
					datetime={event.created_at}
					title={formatDateTime(event.created_at)}
					class="font-mono text-[11px] text-fg-subtle"
				>
					{formatRelative(event.created_at)}
				</time>
				<span class="font-mono text-[11px] text-fg-subtle">
					{sz || ""}
				</span>
				{#if downloadHref}
					<a
						href={downloadHref}
						title={i18n.detail_open_download()}
						class={DOWNLOAD_CHIP}
					>
						{i18n.detail_download_link()}
						<span class={DOWNLOAD_CHIP_ARROW}><ChevronRight size={12} aria-hidden="true" /></span>
					</a>
				{:else}
					<span aria-hidden="true"></span>
				{/if}
			</li>
		{/each}
	</ul>
{/if}
