<script lang="ts" module>
	import type { EpisodeDisplayStatus } from "../../lib/status";

	// Episode statuses map onto the shared status color tokens. "unaired" and
	// "skipped" have no dedicated status pill, so they borrow neutral tones.
	const STATUS_META: Record<
		EpisodeDisplayStatus,
		{ label: string; token: string; live?: boolean }
	> = {
		available: { label: "Available", token: "available" },
		wanted: { label: "Wanted", token: "wanted" },
		missing: { label: "Missing", token: "missing" },
		downloading: { label: "Downloading", token: "downloading", live: true },
		paused: { label: "Paused", token: "paused" },
		unaired: { label: "Unaired", token: "missing" },
		skipped: { label: "Skipped", token: "paused" },
	};
</script>

<script lang="ts">
	import { Bookmark, Info, Search, Trash2 } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { episodeStatus } from "../../lib/status";
	import Modal from "../modals/Modal.svelte";
	import { formatDateShort, formatDateTime, formatRelative } from "../../lib/dates";
	import { formatBytes } from "../../lib/format";
	import type { Episode, SeriesType } from "../../lib/types";

	let {
		episodes,
		seasonNumber,
		seriesType,
		seasonMonitored,
		onMonitorEpisode,
		onManualSearch,
		onDeleteFile,
	}: {
		episodes: Episode[];
		seasonNumber: number;
		seriesType: SeriesType;
		seasonMonitored: boolean;
		onMonitorEpisode: (ep: Episode) => void;
		onManualSearch: (ep: Episode) => void;
		onDeleteFile: (ep: Episode) => void;
	} = $props();

	function pad(n: number): string {
		return String(n).padStart(2, "0");
	}
	function epCode(ep: Episode): string {
		if (seriesType === "daily") return `#${ep.number}`;
		return `S${pad(seasonNumber)}E${pad(ep.number)}`;
	}

	let detail = $state<Episode | null>(null);
	let detailRows = $derived(
		detail
			? [
					{ label: "Episode", value: epCode(detail) },
					{
						label: "Absolute #",
						value: detail.absolute_number ? `#${detail.absolute_number}` : "—",
					},
					{ label: "Air date", value: formatDateTime(detail.air_date) || "—" },
					{
						label: "Monitored",
						value: detail.monitored ? "Yes" : "No",
					},
					{ label: "Quality", value: detail.quality || "—" },
					{ label: "Size", value: formatBytes(detail.size) },
				]
			: [],
	);
</script>

<div
	class="overflow-hidden rounded-lg border border-border bg-bg-elevated/70 backdrop-blur-md"
>
	<table class="w-full text-sm">
		<thead
			class="bg-bg-elevated/95 text-[10px] uppercase tracking-[0.12em] text-fg-faint"
		>
			<tr class="border-b border-border">
				<th scope="col" class="w-10 px-2 py-2.5" aria-hidden="true"></th>
				<th scope="col" class="w-28 px-3 py-2.5 text-left font-medium">#</th>
				<th scope="col" class="px-3 py-2.5 text-left font-medium">Title</th>
				<th
					scope="col"
					class="hidden w-36 px-3 py-2.5 text-left font-medium md:table-cell"
				>
					Air date
				</th>
				<th scope="col" class="w-28 px-3 py-2.5 text-left font-medium">Status</th>
				<th
					scope="col"
					class="hidden w-24 px-3 py-2.5 text-left font-medium sm:table-cell"
				>
					Quality
				</th>
				<th
					scope="col"
					class="hidden w-20 px-3 py-2.5 text-right font-medium sm:table-cell"
				>
					Size
				</th>
				<th scope="col" class="w-20 px-2 py-2.5 text-right font-medium">
					Actions
				</th>
			</tr>
		</thead>
		<tbody>
			{#each episodes as ep (ep.id)}
				{@const meta = STATUS_META[episodeStatus(ep)]}
				{@const monitorDisabled = ep.status === "unaired" && !seasonMonitored}
				<tr
					class={cn(
						"group border-b border-border last:border-b-0 transition hover:bg-surface",
						ep.status === "unaired" && "opacity-70",
					)}
				>
					<td class="px-2 py-2.5">
						<button
							type="button"
							disabled={monitorDisabled}
							onclick={() => onMonitorEpisode(ep)}
							aria-pressed={ep.monitored}
							aria-label={ep.monitored ? "Stop monitoring episode" : "Monitor episode"}
							title={ep.monitored ? "Stop monitoring" : "Monitor"}
							class={cn(
								"grid h-7 w-7 place-items-center rounded-md transition focus:outline-none focus:ring-2 focus:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-40",
								ep.monitored
									? "text-accent-text"
									: "text-fg-subtle hover:text-fg",
							)}
						>
							<Bookmark
								size={14}
								fill={ep.monitored ? "currentColor" : "none"}
								aria-hidden="true"
							/>
						</button>
					</td>
					<td class="px-3 py-2.5 font-mono text-xs tabular text-fg-muted">
						{epCode(ep)}
						{#if ep.absolute_number && seriesType !== "anime"}
							<span class="text-fg-faint">· #{ep.absolute_number}</span>
						{/if}
					</td>
					<td class="min-w-0 px-3 py-2.5">
						<button
							type="button"
							onclick={() => (detail = ep)}
							title="Episode details"
							class="block max-w-full truncate rounded-sm text-left text-fg transition hover:text-accent-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
						>
							{ep.title || "TBA"}
						</button>
					</td>
					<td
						class="hidden px-3 py-2.5 font-mono text-xs text-fg-muted md:table-cell"
					>
						{#if ep.air_date}
							{formatDateShort(ep.air_date)}
							<span class="block text-fg-faint">{formatRelative(ep.air_date)}</span>
						{:else}
							—
						{/if}
					</td>
					<td class="px-3 py-2.5">
						<span
							class="ep-pill inline-flex items-center gap-1 whitespace-nowrap rounded-full px-2 py-0.5 text-[10.5px] font-semibold"
							style:--c={`var(--status-${meta.token})`}
						>
							<span
								class={cn(
									"dot h-1.5 w-1.5 shrink-0 rounded-full",
									meta.live && "motion-safe:animate-pulse",
								)}
							></span>
							{meta.label}
						</span>
					</td>
					<td
						class="hidden px-3 py-2.5 font-mono text-xs text-fg-muted sm:table-cell"
					>
						{ep.quality || "—"}
					</td>
					<td
						class="hidden px-3 py-2.5 text-right font-mono text-xs tabular text-fg-muted sm:table-cell"
					>
						{formatBytes(ep.size)}
					</td>
					<td class="px-2 py-2.5">
						<div class="flex items-center justify-end gap-0.5">
							<button
								type="button"
								onclick={() => (detail = ep)}
								aria-label="Details for {epCode(ep)}"
								title="Details"
								class="grid h-7 w-7 place-items-center rounded-md text-fg-subtle transition hover:bg-surface hover:text-fg focus-visible:ring-2 focus-visible:ring-accent-ring"
							>
								<Info size={14} aria-hidden="true" />
							</button>
							{#if ep.status !== "unaired"}
								<button
									type="button"
									onclick={() => onManualSearch(ep)}
									aria-label="Manual search for {epCode(ep)}"
									title="Manual search"
									class="grid h-7 w-7 place-items-center rounded-md text-fg-subtle transition hover:bg-surface hover:text-fg focus-visible:ring-2 focus-visible:ring-accent-ring"
								>
									<Search size={14} aria-hidden="true" />
								</button>
							{/if}
							{#if (ep.size ?? 0) > 0}
								<button
									type="button"
									onclick={() => onDeleteFile(ep)}
									aria-label="Delete file for {epCode(ep)}"
									title="Delete file"
									class="grid h-7 w-7 place-items-center rounded-md text-fg-subtle transition hover:bg-status-failed/10 hover:text-status-failed focus-visible:ring-2 focus-visible:ring-accent-ring"
								>
									<Trash2 size={14} aria-hidden="true" />
								</button>
							{/if}
						</div>
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
</div>

{#if detail}
	{@const meta = STATUS_META[episodeStatus(detail)]}
	<Modal
		open={true}
		title={detail.title || "TBA"}
		size="xl"
		onClose={() => (detail = null)}
	>
		<div class="flex flex-wrap items-center gap-2">
			<span
				class="ep-pill inline-flex items-center gap-1 whitespace-nowrap rounded-full px-2 py-0.5 text-[10.5px] font-semibold"
				style:--c={`var(--status-${meta.token})`}
			>
				<span
					class={cn(
						"dot h-1.5 w-1.5 shrink-0 rounded-full",
						meta.live && "motion-safe:animate-pulse",
					)}
				></span>
				{meta.label}
			</span>
			{#if detail.air_date}
				<span class="text-xs text-fg-subtle">
					{formatRelative(detail.air_date)}
				</span>
			{/if}
		</div>

		{#if detail.overview}
			<p class="mt-3 text-sm leading-relaxed text-fg-muted">{detail.overview}</p>
		{/if}

		<dl class="mt-4 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2.5 text-sm">
			{#each detailRows as row (row.label)}
				<dt class="text-fg-subtle">{row.label}</dt>
				<dd class="text-right font-mono text-xs tabular text-fg-muted">
					{row.value}
				</dd>
			{/each}
		</dl>

		{#if detail.path}
			<dl class="mt-4 border-t border-border pt-3">
				<dt class="text-sm text-fg-subtle">File</dt>
				<dd
					class="mt-1 break-all font-mono text-xs text-fg-muted"
					title={detail.path}
				>
					{detail.path}
				</dd>
			</dl>
		{/if}

		{#snippet footer()}
			{#if (detail?.size ?? 0) > 0}
				<button
					type="button"
					onclick={() => {
						const ep = detail;
						detail = null;
						if (ep) onDeleteFile(ep);
					}}
					class="inline-flex h-9 items-center gap-1.5 rounded-md border border-border bg-bg-elevated px-3.5 text-sm font-medium text-fg-muted transition hover:border-status-failed/40 hover:bg-status-failed/10 hover:text-status-failed"
				>
					<Trash2 size={15} aria-hidden="true" />
					Delete file
				</button>
			{/if}
			{#if detail && detail.status !== "unaired"}
				<button
					type="button"
					onclick={() => {
						const ep = detail;
						detail = null;
						if (ep) onManualSearch(ep);
					}}
					class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-3.5 text-sm font-medium text-fg-on-accent transition hover:bg-accent-hover"
				>
					<Search size={15} aria-hidden="true" />
					Manual search
				</button>
			{/if}
		{/snippet}
	</Modal>
{/if}

<style>
	.ep-pill {
		background-color: color-mix(in srgb, var(--c) 15%, transparent);
		color: var(--c);
	}
	.ep-pill .dot {
		background-color: var(--c);
	}
</style>
