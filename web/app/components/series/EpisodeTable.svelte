<script lang="ts" module>
	import type { EpisodeStatus } from "../../lib/types";

	// Episode statuses map onto the shared status color tokens. "unaired" and
	// "skipped" have no dedicated status pill, so they borrow neutral tones.
	const STATUS_META: Record<
		EpisodeStatus,
		{ label: string; token: string; live?: boolean }
	> = {
		available: { label: "Available", token: "available" },
		wanted: { label: "Wanted", token: "wanted" },
		downloading: { label: "Downloading", token: "downloading", live: true },
		paused: { label: "Paused", token: "paused" },
		unaired: { label: "Unaired", token: "missing" },
		skipped: { label: "Skipped", token: "paused" },
	};
</script>

<script lang="ts">
	import { Bookmark, Search, Trash2 } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { formatDateShort, formatRelative } from "../../lib/dates";
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
	function formatBytes(bytes: number | null | undefined): string {
		if (!bytes || bytes <= 0) return "—";
		const gb = bytes / 1_073_741_824;
		if (gb >= 1) return `${gb.toFixed(1)} GB`;
		return `${(bytes / 1_048_576).toFixed(0)} MB`;
	}
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
				{@const meta = STATUS_META[ep.status]}
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
						<span class="block truncate text-fg">{ep.title || "TBA"}</span>
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

<style>
	.ep-pill {
		background-color: color-mix(in srgb, var(--c) 15%, transparent);
		color: var(--c);
	}
	.ep-pill .dot {
		background-color: var(--c);
	}
</style>
