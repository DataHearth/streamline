<script lang="ts">
	import { Bookmark, Info, Search, Trash2 } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { dragScroll } from "../../lib/drag-scroll";
	import { episodeStatus } from "../../lib/status";
	import EpisodeDetailModal, {
		STATUS_META,
	} from "./EpisodeDetailModal.svelte";
	import { formatDateShort, formatRelative } from "../../lib/dates";
	import { formatBytes } from "../../lib/format";
	import type { Episode, SeriesType } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

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
</script>

<!-- overflow-x-auto, not hidden: inside the tablet two-pane the table can be
     wider than its cell, and clipping put the Actions column out of reach. The
     column hiding below is container-based for the same reason — a viewport media
     query says nothing about how much room this pane has — and the thresholds
     leave Actions visible at the ~460px the tablet pane gives it. dragScroll so
     any remaining overflow is reachable with a pointer: below lg the app hides
     every scrollbar. -->
<div
	use:dragScroll
	class="@container overflow-x-auto overflow-y-hidden rounded-lg border border-border bg-bg-elevated/70 backdrop-blur-md"
>
	<table class="w-full text-sm">
		<thead
			class="bg-bg-elevated/95 text-[10px] uppercase tracking-[0.12em] text-fg-faint"
		>
			<tr class="border-b border-border">
				<th scope="col" class="w-10 px-2 py-2.5" aria-hidden="true"></th>
				<th scope="col" class="w-28 px-3 py-2.5 text-left font-medium">#</th>
				<th scope="col" class="px-3 py-2.5 text-left font-medium">{i18n.common_title()}</th>
				<th
					scope="col"
					class="hidden w-36 px-3 py-2.5 text-left font-medium @xl:table-cell"
				>
					{i18n.series_air_date()}
				</th>
				<th scope="col" class="w-28 px-3 py-2.5 text-left font-medium">{i18n.common_status()}</th>
				<th
					scope="col"
					class="hidden w-20 px-3 py-2.5 text-right font-medium @lg:table-cell"
				>
					{i18n.common_size()}
				</th>
				<th scope="col" class="w-20 px-2 py-2.5 text-right font-medium">
					{i18n.common_actions()}
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
							aria-label={ep.monitored ? i18n.action_stop_monitoring_episode() : i18n.action_monitor_episode()}
							title={ep.monitored ? i18n.action_stop_monitoring() : i18n.action_monitor()}
							class={cn(
								"grid h-11 w-11 place-items-center rounded-md transition focus:outline-none focus:ring-2 focus:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-40 lg:h-7 lg:w-7",
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
					<!-- max-w-0 + w-full, not min-w-0: the table lays out auto, where a
					     cell sizes to its content and min-width is ignored, so a long
					     title widened the column and scrolled the whole table sideways.
					     Zero max-width leaves the cell taking only what the fixed
					     columns don't, which is what lets truncate engage. -->
					<td class="w-full max-w-0 px-3 py-2.5">
						<button
							type="button"
							onclick={() => (detail = ep)}
							title={ep.title || i18n.series_episode_details()}
							class="block max-w-full truncate rounded-sm text-left text-fg transition hover:text-accent-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
						>
							{ep.title || "TBA"}
						</button>
					</td>
					<td
						class="hidden whitespace-nowrap px-3 py-2.5 font-mono text-xs text-fg-muted @xl:table-cell"
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
						class="hidden px-3 py-2.5 text-right font-mono text-xs tabular text-fg-muted @lg:table-cell"
					>
						{formatBytes(ep.size)}
					</td>
					<td class="px-2 py-2.5">
						<div class="flex items-center justify-end gap-0.5">
							<button
								type="button"
								onclick={() => (detail = ep)}
								aria-label="Details for {epCode(ep)}"
								title={i18n.common_details()}
								class="grid h-11 w-11 place-items-center rounded-md text-fg-subtle transition hover:bg-surface hover:text-fg focus-visible:ring-2 focus-visible:ring-accent-ring lg:h-7 lg:w-7"
							>
								<Info size={14} aria-hidden="true" />
							</button>
							{#if ep.status !== "unaired"}
								<button
									type="button"
									onclick={() => onManualSearch(ep)}
									aria-label="Manual search for {epCode(ep)}"
									title={i18n.action_manual_search()}
									class="grid h-11 w-11 place-items-center rounded-md text-fg-subtle transition hover:bg-surface hover:text-fg focus-visible:ring-2 focus-visible:ring-accent-ring lg:h-7 lg:w-7"
								>
									<Search size={14} aria-hidden="true" />
								</button>
							{/if}
							{#if (ep.size ?? 0) > 0}
								<button
									type="button"
									onclick={() => onDeleteFile(ep)}
									aria-label="Delete file for {epCode(ep)}"
									title={i18n.action_delete_file()}
									class="grid h-11 w-11 place-items-center rounded-md text-fg-subtle transition hover:bg-status-failed/10 hover:text-status-failed focus-visible:ring-2 focus-visible:ring-accent-ring lg:h-7 lg:w-7"
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

<EpisodeDetailModal
	episode={detail}
	code={detail ? epCode(detail) : ""}
	onClose={() => (detail = null)}
	{onManualSearch}
	{onDeleteFile}
/>
