<script lang="ts">
	import { Bookmark, ChevronRight, Info, Search, Trash2 } from "@lucide/svelte";
	import KebabMenu, { type KebabItem } from "../shared/KebabMenu.svelte";
	import EpisodeDetailModal from "./EpisodeDetailModal.svelte";
	import { slide } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { cn } from "../../lib/cn";
	import { episodeStatus, missingEpisodes } from "../../lib/status";
	import { formatBytes } from "../../lib/format";
	import { episodeMedia } from "../../lib/media-info";
	import { formatDateShort } from "../../lib/dates";
	import type { Episode, Season, SeriesType } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

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
		onSearchSeason,
		onDeleteFile,
		onDeleteSeasonFiles,
	}: {
		seasons: Season[];
		selected: number;
		onSelect: (n: number) => void;
		seriesType: SeriesType;
		seasonLabel?: string;
		onMonitorSeason: (s: Season) => void;
		onMonitorEpisode: (ep: Episode) => void;
		onManualSearch: (ep: Episode) => void;
		onSearchSeason: (s: Season) => void;
		onDeleteFile: (ep: Episode) => void;
		// Season scope: wipes every file under it and reverts those episodes to
		// wanted. The season keeps existing — this is not "delete the season".
		onDeleteSeasonFiles: (s: Season) => void;
	} = $props();

	// Collapse is local, expand is shared. Pushing "nothing open" up into the
	// route's selectedSeason would leave the md+ two-pane with no season to show
	// on a resize, so only a real selection is reported.
	let closed = $state(false);
	let openSeason = $derived(closed ? -1 : selected);
	function toggle(n: number) {
		if (openSeason === n) {
			closed = true;
			return;
		}
		closed = false;
		onSelect(n);
	}

	// The same sheet the desktop table opens, so "info" means one thing.
	let detail = $state<Episode | null>(null);
	let detailCode = $state("");
	function openDetail(s: Season, ep: Episode) {
		detailCode = epCode(s, ep);
		detail = ep;
	}

	// Three actions behind one ⋯ instead of a single contextual button: the row
	// used to show Search OR Delete OR a dot, so which action existed depended on
	// state and details were unreachable at this width entirely.
	function epMenu(s: Season, ep: Episode): KebabItem[] {
		const st = episodeStatus(ep);
		const hasFile = (ep.size ?? 0) > 0;
		return [
			{
				key: "info",
				label: i18n.series_episode_details(),
				icon: Info,
				onSelect: () => openDetail(s, ep),
			},
			{
				key: "search",
				label: i18n.action_manual_search_ellipsis(),
				icon: Search,
				disabled: st === "unaired",
				title: st === "unaired" ? i18n.series_not_aired_yet() : undefined,
				onSelect: () => onManualSearch(ep),
			},
			{
				key: "delete",
				label: i18n.action_delete_file(),
				icon: Trash2,
				danger: true,
				dividerBefore: true,
				disabled: !hasFile,
				title: hasFile ? undefined : i18n.series_no_file_on_disk(),
				onSelect: () => onDeleteFile(ep),
			},
		];
	}
	function seasonFiles(s: Season): Episode[] {
		return (s.episodes ?? []).filter((e) => (e.size ?? 0) > 0);
	}

	function pad(n: number): string {
		return String(n).padStart(2, "0");
	}
	function seasonName(s: Season): string {
		return s.number === 0 ? i18n.series_specials() : `${seasonLabel} ${pad(s.number)}`;
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
			const parts = [episodeMedia(ep), formatBytes(ep.size, "")].filter(Boolean);
			return parts.join(" · ") || "available";
		}
		if (st === "downloading") return "downloading";
		if (st === "importing") return "importing";
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
		importing: "bg-status-grabbing",
		paused: "bg-status-paused",
		unaired: "bg-fg-faint",
		skipped: "bg-status-paused",
	};
</script>

<div class="overflow-hidden rounded-lg border border-border bg-bg-elevated/60">
	{#each seasons as s (s.number)}
		{@const open = openSeason === s.number}
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
					onclick={() => toggle(s.number)}
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
						? i18n.a11y_stop_monitoring_season({ season: seasonName(s) })
						: i18n.a11y_monitor_season({ season: seasonName(s) })}
					class={cn(
						"grid h-11 w-11 lg:h-9 lg:w-9 shrink-0 place-items-center rounded-lg border transition",
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

				<button
					type="button"
					onclick={() => onSearchSeason(s)}
					aria-label="Search releases for {seasonName(s)}"
					class="grid h-11 w-11 lg:h-9 lg:w-9 shrink-0 place-items-center rounded-lg border border-border bg-bg-elevated text-fg-subtle transition active:bg-accent-soft active:text-accent-text"
				>
					<Search size={15} aria-hidden="true" />
				</button>

				<button
					type="button"
					disabled={seasonFiles(s).length === 0}
					onclick={() => onDeleteSeasonFiles(s)}
					aria-label="Delete all files in {seasonName(s)}"
					class="grid h-11 w-11 lg:h-9 lg:w-9 shrink-0 place-items-center rounded-lg border border-border bg-bg-elevated text-fg-subtle transition active:bg-status-failed/10 active:text-status-failed disabled:opacity-35"
				>
					<Trash2 size={15} aria-hidden="true" />
				</button>

				<button
					type="button"
					onclick={() => toggle(s.number)}
					aria-expanded={open}
					aria-label="{open ? 'Collapse' : 'Expand'} {seasonName(s)}"
					class="grid h-9 w-6 shrink-0 place-items-center"
				>
					<ChevronRight
						size={18}
						class={cn(
							"transition",
							open ? "rotate-90 text-accent-text" : "text-fg-faint",
						)}
						aria-hidden="true"
					/>
				</button>
			</div>

			{#if open}
				<ul transition:slide={{ duration: 180, easing: cubicOut }}>
					{#each s.episodes ?? [] as ep (ep.id)}
						{@const st = episodeStatus(ep)}
						<li
							class="flex items-center gap-2.5 border-t border-border bg-bg-deep px-3 py-2.5"
						>
							<button
								type="button"
								disabled={ep.status === "unaired" && !s.monitored}
								onclick={() => onMonitorEpisode(ep)}
								aria-pressed={ep.monitored}
								aria-label={ep.monitored
									? i18n.action_stop_monitoring_episode()
									: i18n.action_monitor_episode()}
								class={cn(
									"grid h-11 w-11 shrink-0 place-items-center rounded-md transition disabled:opacity-40 lg:h-8 lg:w-8",
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
							<span
								class={cn("h-1.5 w-1.5 shrink-0 rounded-full", DOT[st])}
								aria-hidden="true"
							></span>
							<span class="shrink-0 [&_button]:h-9 [&_button]:w-9">
								<KebabMenu items={epMenu(s, ep)} variant="bar" />
							</span>
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	{/each}
</div>

<EpisodeDetailModal
	episode={detail}
	code={detailCode}
	onClose={() => (detail = null)}
	{onManualSearch}
	{onDeleteFile}
/>
