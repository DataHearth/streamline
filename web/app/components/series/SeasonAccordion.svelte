<script lang="ts">
	import { Bookmark, ChevronRight, Search, Trash2 } from "@lucide/svelte";
	import { slide } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { cn } from "../../lib/cn";
	import { episodeStatus, missingEpisodes } from "../../lib/status";
	import { formatBytes } from "../../lib/format";
	import { formatDateShort } from "../../lib/dates";
	import type { Episode, Season, SeriesType } from "../../lib/types";

	// Phone shape for the episodes tab. The desktop pair — a horizontal season
	// strip above a seven-column table — needs width this viewport doesn't have,
	// and a five-season show turns the strip into a second scroll direction.
	// Here every season is a row carrying its own progress, one open at a time.
	let {
		seasons,
		selected,
		onSelect,
		seriesType,
		seasonLabel = "Season",
		onMonitorSeason,
		onMonitorEpisode,
		onManualSearch,
		onDeleteFile,
	}: {
		seasons: Season[];
		selected: number;
		onSelect: (n: number) => void;
		seriesType: SeriesType;
		seasonLabel?: string;
		onMonitorSeason: (s: Season) => void;
		onMonitorEpisode: (ep: Episode) => void;
		onManualSearch: (ep: Episode) => void;
		onDeleteFile: (ep: Episode) => void;
	} = $props();

	function pad(n: number): string {
		return String(n).padStart(2, "0");
	}
	function seasonName(s: Season): string {
		return s.number === 0 ? "Specials" : `${seasonLabel} ${pad(s.number)}`;
	}
	function epCode(s: Season, ep: Episode): string {
		if (seriesType === "daily") return `#${ep.number}`;
		return `E${pad(ep.number)}`;
	}
	function seasonBytes(s: Season): number {
		return (s.episodes ?? []).reduce((n, ep) => n + (ep.size ?? 0), 0);
	}
	function pct(s: Season): number {
		const total = s.total ?? 0;
		if (total === 0) return 0;
		return Math.round(((s.available ?? 0) / total) * 100);
	}

	// One line per episode, and it says the thing that matters for the state it
	// is in: what the file is, how far a download has got, or what a wanted
	// episode has to search against.
	function epLine(ep: Episode): string {
		const st = episodeStatus(ep);
		if (st === "available") {
			const parts = [ep.quality, formatBytes(ep.size, "")].filter(Boolean);
			return parts.join(" · ") || "available";
		}
		if (st === "downloading") return "downloading";
		if (st === "unaired")
			return ep.air_date ? `airs ${formatDateShort(ep.air_date)}` : "unaired";
		if (st === "skipped") return "not monitored";
		if (st === "paused") return "paused";
		if (st === "missing") return "missing on disk";
		return ep.air_date ? `wanted · aired ${formatDateShort(ep.air_date)}` : "wanted";
	}
	const DOT: Record<string, string> = {
		available: "bg-status-available",
		wanted: "bg-status-wanted",
		missing: "bg-status-missing",
		downloading: "bg-status-downloading",
		paused: "bg-status-paused",
		unaired: "bg-fg-faint",
		skipped: "bg-status-paused",
	};
</script>

<div class="overflow-hidden rounded-lg border border-border bg-bg-elevated/60">
	{#each seasons as s (s.number)}
		{@const open = selected === s.number}
		{@const missing = missingEpisodes(s.episodes ?? [])}
		{@const wanted = s.missing ?? 0}
		<section class="border-b border-border last:border-b-0">
			<div
				class={cn(
					"flex items-center gap-3 px-3 py-3 transition",
					open && "bg-surface",
				)}
			>
				<button
					type="button"
					onclick={() => onSelect(s.number)}
					aria-expanded={open}
					class="flex min-w-0 flex-1 items-center gap-3 text-left"
				>
					<span class="min-w-0 flex-1">
						<span
							class={cn(
								"block font-mono text-[13px] font-semibold tracking-tight",
								open ? "text-accent-text" : "text-fg",
							)}
						>
							{seasonName(s)}
						</span>
						<span class="mt-1 block font-mono text-[10.5px] text-fg-subtle">
							{s.available ?? 0}/{s.total ?? 0}
							{#if seasonBytes(s) > 0}
								· {formatBytes(seasonBytes(s), "")}
							{/if}
							{#if wanted > 0}
								· <span class="text-status-wanted">{wanted} wanted</span>
							{/if}
							{#if missing > 0}
								· <span class="text-status-missing">{missing} missing</span>
							{/if}
						</span>
						<span
							class="mt-2 block h-[3px] overflow-hidden rounded-full bg-surface-2"
							aria-hidden="true"
						>
							<span
								class={cn(
									"block h-full rounded-full",
									wanted > 0 ? "bg-status-wanted" : "bg-status-available",
								)}
								style:width="{pct(s)}%"
							></span>
						</span>
					</span>
				</button>

				<button
					type="button"
					onclick={() => onMonitorSeason(s)}
					aria-pressed={s.monitored}
					aria-label={s.monitored
						? `Stop monitoring ${seasonName(s)}`
						: `Monitor ${seasonName(s)}`}
					class={cn(
						"grid h-9 w-9 shrink-0 place-items-center rounded-lg border transition",
						s.monitored
							? "border-accent-line bg-accent-soft text-accent-text"
							: "border-border bg-bg-elevated text-fg-subtle",
					)}
				>
					<Bookmark
						size={16}
						fill={s.monitored ? "currentColor" : "none"}
						aria-hidden="true"
					/>
				</button>

				<ChevronRight
					size={18}
					class={cn(
						"shrink-0 transition",
						open ? "rotate-90 text-accent-text" : "text-fg-faint",
					)}
					aria-hidden="true"
				/>
			</div>

			{#if open}
				<ul transition:slide={{ duration: 180, easing: cubicOut }}>
					{#each s.episodes ?? [] as ep (ep.id)}
						{@const st = episodeStatus(ep)}
						{@const searchable = st !== "available" && st !== "unaired"}
						<li
							class="flex items-center gap-2.5 border-t border-border bg-bg-deep px-3 py-2.5"
						>
							<button
								type="button"
								disabled={ep.status === "unaired" && !s.monitored}
								onclick={() => onMonitorEpisode(ep)}
								aria-pressed={ep.monitored}
								aria-label={ep.monitored
									? "Stop monitoring episode"
									: "Monitor episode"}
								class={cn(
									"grid h-8 w-8 shrink-0 place-items-center rounded-md transition disabled:opacity-40",
									ep.monitored ? "text-accent-text" : "text-fg-faint",
								)}
							>
								<Bookmark
									size={14}
									fill={ep.monitored ? "currentColor" : "none"}
									aria-hidden="true"
								/>
							</button>
							<span
								class="w-9 shrink-0 font-mono text-[11px] font-semibold text-fg-subtle"
							>
								{epCode(s, ep)}
							</span>
							<span class="min-w-0 flex-1">
								<span
									class={cn(
										"block truncate text-[13.5px] font-medium",
										st === "unaired" ? "text-fg-subtle" : "text-fg",
									)}
								>
									{ep.title || "TBA"}
								</span>
								<span
									class={cn(
										"mt-0.5 block truncate font-mono text-[10.5px]",
										st === "wanted"
											? "text-status-wanted"
											: st === "downloading"
												? "text-status-downloading"
												: "text-fg-subtle",
									)}
								>
									{epLine(ep)}
								</span>
							</span>
							{#if searchable}
								<button
									type="button"
									onclick={() => onManualSearch(ep)}
									aria-label="Manual search for {epCode(s, ep)}"
									class="grid h-9 w-9 shrink-0 place-items-center rounded-lg text-fg-subtle transition active:bg-surface"
								>
									<Search size={16} aria-hidden="true" />
								</button>
							{:else if (ep.size ?? 0) > 0}
								<button
									type="button"
									onclick={() => onDeleteFile(ep)}
									aria-label="Delete file for {epCode(s, ep)}"
									class="grid h-9 w-9 shrink-0 place-items-center rounded-lg text-fg-faint transition active:bg-status-failed/10 active:text-status-failed"
								>
									<Trash2 size={15} aria-hidden="true" />
								</button>
							{:else}
								<span
									class={cn("h-1.5 w-1.5 shrink-0 rounded-full", DOT[st])}
									aria-hidden="true"
								></span>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	{/each}
</div>
