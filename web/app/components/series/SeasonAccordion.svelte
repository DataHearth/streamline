<script lang="ts">
	import MonitorToggle from "@components/shared/MonitorToggle.svelte";
	import SeasonProgress from "./SeasonProgress.svelte";
	import { auth } from "@lib/auth.svelte";
	import { ChevronRight, Info, Search, Trash2 } from "@lucide/svelte";
	import DownloadLink from "@components/shared/DownloadLink.svelte";
	import LabelPill from "@components/shared/LabelPill.svelte";
	import { DOWNLOAD_CHIP_ARROW } from "@lib/title-downloads";
	import KebabMenu, { type KebabItem } from "@components/shared/KebabMenu.svelte";
	import EpisodeDetailModal from "./EpisodeDetailModal.svelte";
	import { slide } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { cn } from "@lib/cn";
	import {
		episodeStatus,
		episodeTitle,
		missingEpisodes,
		seasonHeadline,
		seasonTallies,
	} from "@lib/status";
	import { formatBytes } from "@lib/format";
	import { episodeMedia } from "@lib/media-info";
	import { formatDateShort } from "@lib/dates";
	import type { Episode, Season, SeriesType } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Phone shape for the episodes tab. The desktop pair — a horizontal season
	// strip above a seven-column table — needs width this viewport doesn't have,
	// and a five-season show turns the strip into a second scroll direction.
	// Here every season is a row carrying its own progress, one open at a time.
	let {
		seasons,
		selected,
		onSelect,
		seriesType,
		onMonitorSeason,
		onMonitorEpisode,
		pendingSeason = null,
		pendingEpisode = null,
		onManualSearch,
		onSearchSeason,
		onDeleteFile,
		onDeleteSeasonFiles,
		showMonitored = true,
		seasonDownload,
		episodeDownload,
	}: {
		seasons: Season[];
		selected: number;
		onSelect: (n: number) => void;
		seriesType: SeriesType;
		onMonitorSeason: (s: Season) => void;
		onMonitorEpisode: (ep: Episode) => void;
		// The season number / episode id whose monitor write is in flight.
		pendingSeason?: number | null;
		pendingEpisode?: number | null;
		onManualSearch: (ep: Episode) => void;
		onSearchSeason: (s: Season) => void;
		onDeleteFile: (ep: Episode) => void;
		// Season scope: wipes every file under it and reverts those episodes to
		// wanted. The season keeps existing — this is not "delete the season".
		onDeleteSeasonFiles: (s: Season) => void;
		showMonitored?: boolean;
		// Where a season's in-flight count links: its one download, or the queue
		// filtered on the season when it waits on several.
		seasonDownload?: (s: Season) => { href: string; label: string } | undefined;
		// The download an episode is waiting on, which its status line links.
		episodeDownload?: (ep: Episode) => string | undefined;
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

	// Three actions behind one ⋯ instead of a single contextual button, which
	// would show Search OR Delete OR a dot, so which action exists would depend on
	// state and details would be unreachable at this width entirely.
	function epMenu(s: Season, ep: Episode): KebabItem[] {
		const st = episodeStatus(ep, showMonitored);
		const hasFile = (ep.size ?? 0) > 0;
		const info = {
			key: "info",
			label: i18n.series_episode_details(),
			icon: Info,
			onSelect: () => openDetail(s, ep),
		};
		if (!auth.canAddDirectly) return [info];
		return [
			info,
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
		return s.number === 0
			? i18n.series_specials()
			: i18n.series_season_n({ n: pad(s.number) });
	}
	function epCode(s: Season, ep: Episode): string {
		if (seriesType === "daily") return `#${ep.number}`;
		return `E${pad(ep.number)}`;
	}
	function seasonBytes(s: Season): number {
		return (s.episodes ?? []).reduce((n, ep) => n + (ep.size ?? 0), 0);
	}

	// One line per episode, and it says the thing that matters for the state it
	// is in: what the file is, how far a download has got, or what a wanted
	// episode has to search against.
	function epLine(ep: Episode): string {
		const st = episodeStatus(ep, showMonitored);
		if (st === "available") {
			const parts = [
				ep.missing_from_disk && i18n.file_missing_from_disk(),
				episodeMedia(ep),
				formatBytes(ep.size, ""),
			].filter(Boolean);
			return parts.join(" · ") || i18n.ep_state_available();
		}
		if (st === "downloading") return i18n.ep_state_downloading();
		if (st === "importing") return i18n.ep_state_importing();
		if (st === "unaired")
			return ep.air_date
				? i18n.ep_airs_on({ date: formatDateShort(ep.air_date) })
				: i18n.ep_state_unaired();
		if (st === "skipped") return i18n.ep_state_not_monitored();
		if (st === "paused") return i18n.ep_state_paused();
		if (st === "missing") return i18n.ep_state_missing();
		return ep.air_date
			? i18n.ep_wanted_aired({ date: formatDateShort(ep.air_date) })
			: i18n.ep_state_wanted();
	}
	// The status line's colour, as a link or not.
	const EP_LINE: Record<string, string> = {
		wanted: "text-status-wanted",
		downloading: "text-status-downloading",
	};
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
		{@const missing = missingEpisodes(s.episodes ?? [], showMonitored)}
		{@const tally = seasonTallies(s, showMonitored)}
		{@const headline = seasonHeadline(tally, missing)}
		<!-- With a download behind it, the in-flight headline ("3 downloading")
		     moves to a line under the row and is the link there, said once: the
		     row is a button, which cannot hold a link. -->
		{@const seasonDl = tally.downloading > 0 || tally.importing > 0 ? seasonDownload?.(s) : undefined}
		<section class="border-b border-border last:border-b-0">
			<div class={cn("transition", open && "bg-surface")}>
			<div class="flex items-center gap-3 px-3 py-3">
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
							{#if headline && !seasonDl}
								<span class="whitespace-nowrap"
									>· <span class={headline.cls}>{headline.text}</span></span
								>
							{/if}
						</span>
						<span class="mt-2 block">
							<SeasonProgress season={s} {showMonitored} />
						</span>
					</span>
				</button>

				{#if auth.canAddDirectly}
					<MonitorToggle
						monitored={s.monitored}
						onToggle={() => onMonitorSeason(s)}
						pending={pendingSeason === s.number}
						size="sm"
						startLabel={i18n.a11y_monitor_season({ season: seasonName(s) })}
						stopLabel={i18n.a11y_stop_monitoring_season({ season: seasonName(s) })}
					/>

					<button
						type="button"
						onclick={() => onSearchSeason(s)}
						aria-label={i18n.series_search_releases_for({ season: seasonName(s) })}
						class="grid h-11 w-11 lg:h-9 lg:w-9 shrink-0 place-items-center rounded-lg border border-border bg-bg-elevated text-fg-subtle transition active:bg-accent-soft active:text-accent-text"
					>
						<Search size={15} aria-hidden="true" />
					</button>

					<button
						type="button"
						disabled={seasonFiles(s).length === 0}
						onclick={() => onDeleteSeasonFiles(s)}
						aria-label={i18n.series_delete_all_in({ season: seasonName(s) })}
						class="grid h-11 w-11 lg:h-9 lg:w-9 shrink-0 place-items-center rounded-lg border border-border bg-bg-elevated text-fg-subtle transition active:bg-status-failed/10 active:text-status-failed disabled:opacity-35"
					>
						<Trash2 size={15} aria-hidden="true" />
					</button>
				{/if}

				<button
					type="button"
					onclick={() => toggle(s.number)}
					aria-expanded={open}
					aria-label={open
						? i18n.series_collapse_season({ season: seasonName(s) })
						: i18n.series_expand_season({ season: seasonName(s) })}
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

			{#if seasonDl && headline}
				<div class="-mt-3 px-3">
					<DownloadLink href={seasonDl.href} label={seasonDl.label} class="my-0">
						<LabelPill
							token={tally.downloading > 0 ? "downloading" : "importing"}
							label={headline.text}
							live
						/>
					</DownloadLink>
				</div>
			{/if}
			</div>

			{#if open}
				<ul transition:slide={{ duration: 180, easing: cubicOut }}>
					{#each s.episodes ?? [] as ep (ep.id)}
						{@const st = episodeStatus(ep, showMonitored)}
						{@const dl = episodeDownload?.(ep)}
						<li
							class="flex items-center gap-2.5 border-t border-border bg-bg-deep px-3 py-2.5"
						>
							<MonitorToggle
								monitored={ep.monitored}
								onToggle={() => onMonitorEpisode(ep)}
								pending={pendingEpisode === ep.id}
								disabled={(ep.status === "unaired" && !s.monitored) || !auth.canAddDirectly}
								variant="ghost"
								size="row"
								startLabel={i18n.action_monitor_episode()}
								stopLabel={i18n.action_stop_monitoring_episode()}
							/>
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
									{episodeTitle(ep)}
								</span>
								{#if dl}
									<!-- The negative margin takes back touch-hit's padding: the
									     tap target grows over the row, the row does not. -->
									<a
										href={dl}
										title={i18n.activity_download_open()}
										class={cn(
											"group/dl touch-hit -mb-[15px] -mt-[13px] inline-flex max-w-full cursor-pointer items-center gap-1 font-mono text-[10.5px] underline-offset-2 hover:underline",
											EP_LINE[st] ?? "text-fg-subtle",
										)}
									>
										<span class="truncate">{epLine(ep)}</span>
										<span class={DOWNLOAD_CHIP_ARROW}><ChevronRight size={12} aria-hidden="true" /></span>
										<span class="sr-only">{i18n.activity_download_open()}</span>
									</a>
								{:else}
									<span
										class={cn(
											"mt-0.5 block truncate font-mono text-[10.5px]",
											EP_LINE[st] ?? "text-fg-subtle",
										)}
									>
										{epLine(ep)}
									</span>
								{/if}
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
	{showMonitored}
/>
