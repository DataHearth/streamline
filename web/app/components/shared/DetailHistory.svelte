<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { Activity, ChevronRight } from "@lucide/svelte";
	import { link } from "@lib/routes";
	import { DOWNLOAD_CHIP, DOWNLOAD_CHIP_ARROW } from "@lib/title-downloads";
	import { api, errorText } from "@lib/api";
	import { cn } from "@lib/cn";
	import {
		EVENT_MARKS,
		eventSubject,
		monitoringDetail,
	} from "@lib/activity-event";
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
	// The download column exists only when some event still has its record: a
	// title with none keeps the four columns it had.
	let linked = $derived(events.some((e) => e.download_id));
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
			{@const scope = eventSubject(event).detail}
			<li
				class={cn(
					"grid items-center gap-4 border-b border-border px-4 py-3 last:border-b-0",
					linked ? "grid-cols-[28px_1fr_auto_auto_auto]" : "grid-cols-[28px_1fr_auto_auto]",
				)}
			>
				<span
					class={`grid h-6 w-6 place-items-center rounded-sm ${mark.bg} ${mark.fg}`}
				>
					<mark.icon size={13} aria-hidden="true" />
				</span>
				<div class="min-w-0">
					<div class="font-mono text-[10px] tracking-[0.1em] text-fg-subtle">
						{mark.label}
					</div>
					{#if scope}
						<div class="mt-0.5 truncate font-mono text-[11.5px] text-fg">
							{scope}
						</div>
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
				<!-- The download the event came from, while its record still exists:
				     the events feed's chip, opening the same detail on Activity. -->
				{#if event.download_id}
					<a
						href={link.download(event.download_id)}
						title={i18n.activity_download_open()}
						class="group/dl touch-hit -my-[15px] inline-flex cursor-pointer lg:my-0"
					>
						<span class={DOWNLOAD_CHIP}>
							{i18n.activity_download_link()}
							<span class={DOWNLOAD_CHIP_ARROW}><ChevronRight size={12} aria-hidden="true" /></span>
						</span>
					</a>
				{:else if linked}
					<span aria-hidden="true"></span>
				{/if}
			</li>
		{/each}
	</ul>
{/if}
