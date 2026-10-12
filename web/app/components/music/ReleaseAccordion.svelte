<script lang="ts">
	import { ChevronRight, Radar, Search } from "@lucide/svelte";
	import MonitorToggle from "@components/shared/MonitorToggle.svelte";
	import { slide } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { cn } from "@lib/cn";
	import { formatDateShort } from "@lib/dates";
	import LabelPill from "@components/shared/LabelPill.svelte";
	import ReleaseCover from "./ReleaseCover.svelte";
	import ReleaseMark from "./ReleaseMark.svelte";
	import Tracklist from "./Tracklist.svelte";
	import ReleaseCredits from "./ReleaseCredits.svelte";
	import { qualityNote, releaseFacts, releaseGroups, releaseLine, releasePill, releaseTypeLabel, type Release, type Track } from "@lib/music-books";
	import { albumPosterUrl } from "@lib/posters";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Phone shape of the discography, the way SeasonAccordion is for seasons:
	// one release open at a time, its tracks inside it. An open release offers
	// both searches — Search now (automatic), and Manual search as the icon
	// beside it, the way the book page's pinned bar carries it.
	let {
		releases,
		openId,
		onToggle,
		canEdit = false,
		onSearch,
		onManualSearch,
		onMonitor,
		monitorPending = false,
		onManualSearchTrack,
		onDeleteTrack,
	}: {
		releases: Release[];
		openId: number | undefined;
		onToggle: (id: number) => void;
		canEdit?: boolean;
		onSearch: (r: Release) => void;
		onManualSearch: (r: Release) => void;
		onMonitor: (r: Release) => void;
		// The monitor write is in flight.
		monitorPending?: boolean;
		// A track is never grabbed on its own: its manual search lists the
		// releases of its album.
		onManualSearchTrack?: (r: Release) => void;
		onDeleteTrack?: (t: Track) => void;
	} = $props();

	let groups = $derived(releaseGroups(releases));
</script>

{#snippet row(r: Release)}
	{@const open = r.id === openId}
	{@const up = r.status === "upcoming"}
	{@const pill = releasePill(r)}
	<div class={cn("border-b border-border", open && "bg-surface")}>
		<button
			type="button"
			onclick={() => onToggle(r.id)}
			aria-expanded={open}
			class="flex min-h-11 w-full items-center gap-3 px-4 py-3 text-left"
		>
			<div class="w-12 shrink-0">
				<ReleaseCover
					src={albumPosterUrl(r.id)}
					dim={up || (r.status === "wanted" && r.tracks_have === 0)}
					dashed={up}
					class="rounded"
				/>
			</div>
			<div class="min-w-0 flex-1">
				<p class="truncate text-[14px] font-medium text-fg">{r.title}</p>
				<p class="truncate font-mono text-[11px] text-fg-subtle">
					{up
						? [releaseTypeLabel(r.type), formatDateShort(r.release_date)].filter(Boolean).join(" · ")
						: releaseLine(r)}
				</p>
			</div>
			<ReleaseMark release={r} />
			<ChevronRight size={16} class={cn("shrink-0 text-fg-subtle transition", open && "rotate-90")} aria-hidden="true" />
		</button>
		{#if open}
			<div class="px-4 pb-4" transition:slide={{ duration: 200, easing: cubicOut }}>
				<div class="flex flex-wrap items-center gap-2">
					{#if pill}
						<LabelPill token={pill.token} label={pill.label} variant="translucent" live={pill.live} />
					{/if}
					<span class="font-mono text-[11px] text-fg-muted">{releaseFacts(r).join(" · ")}</span>
				</div>
				{#if r.status === "failed"}
					<div class="mt-2 text-[12.5px] leading-snug">
						{#if r.failure_reason}
							<p class="font-mono text-[11px] text-status-failed">{r.failure_reason}</p>
						{/if}
						<p class="mt-1 text-fg-muted">{i18n.music_import_failed()}</p>
					</div>
				{/if}
				{#if canEdit}
					<div class="mt-3 flex gap-2">
						{#if !up && r.status !== "failed"}
							<button
								type="button"
								onclick={() => onSearch(r)}
								class="inline-flex h-11 min-w-0 flex-1 items-center justify-center gap-2 rounded-lg bg-accent text-[14px] font-semibold text-fg-on-accent"
							>
								<Radar size={16} aria-hidden="true" />
								{i18n.music_search_now()}
							</button>
						{/if}
						{#if !up}
							<button
								type="button"
								onclick={() => onManualSearch(r)}
								aria-label={i18n.action_manual_search()}
								title={i18n.action_manual_search()}
								class="grid h-11 w-11 shrink-0 place-items-center rounded-lg border border-border-strong bg-bg-elevated text-fg-muted transition active:bg-surface"
							>
								<Search size={16} aria-hidden="true" />
							</button>
						{/if}
						<MonitorToggle monitored={r.monitored} onToggle={() => onMonitor(r)} pending={monitorPending} />
						{#if up}
							<p class="self-center text-[12.5px] leading-snug text-fg-muted">
								{r.monitored ? i18n.music_upcoming_monitored() : i18n.music_upcoming_unmonitored()}
							</p>
						{/if}
					</div>
				{/if}
				<div class="mt-3">
					{#if r.tracks?.length}
						<Tracklist
							tracks={r.tracks}
							release={r}
							qualityNote={qualityNote(r)}
							compact
							markMissing={r.status === "wanted" && r.tracks_have > 0}
							{canEdit}
							onManualSearch={onManualSearchTrack ? () => onManualSearchTrack(r) : undefined}
							onDeleteFile={onDeleteTrack}
						/>
					{:else if r.tracks_pending}
						<p class="rounded-lg border border-dashed border-border-strong px-4 py-6 text-center text-[13px] text-fg-muted">
							{i18n.music_tracks_fetching()}
						</p>
					{:else}
						<p class="rounded-lg border border-dashed border-border-strong px-4 py-6 text-center text-[13px] text-fg-muted">
							{i18n.music_tracklist_unannounced()}
						</p>
					{/if}
				</div>
				<div class="mt-5">
					<ReleaseCredits release={r} scope="accordion" />
				</div>
			</div>
		{/if}
	</div>
{/snippet}

{#each groups as g (g.key)}
	<p class="px-4 pb-2 pt-5 font-mono text-[10.5px] uppercase tracking-[0.12em] text-fg-faint">
		{g.label} · {g.releases.length}
	</p>
	{#each g.releases as r (r.id)}
		{@render row(r)}
	{/each}
{/each}
