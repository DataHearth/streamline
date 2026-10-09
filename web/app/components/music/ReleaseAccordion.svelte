<script lang="ts">
	import { Bookmark, ChevronRight, Radar, Search } from "@lucide/svelte";
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
		onSearchTrack,
		onDeleteTrack,
	}: {
		releases: Release[];
		openId: number | undefined;
		onToggle: (id: number) => void;
		canEdit?: boolean;
		onSearch: (r: Release) => void;
		onManualSearch: (r: Release) => void;
		onMonitor: (r: Release) => void;
		onSearchTrack?: (t: Track) => void;
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
					<LabelPill token={pill.token} label={pill.label} variant="translucent" live={pill.live} />
					<span class="font-mono text-[11px] text-fg-muted">{releaseFacts(r).join(" · ")}</span>
				</div>
				{#if canEdit}
					<div class="mt-3 flex gap-2">
						{#if !up}
							<button
								type="button"
								onclick={() => onSearch(r)}
								class="inline-flex h-11 min-w-0 flex-1 items-center justify-center gap-2 rounded-lg bg-accent text-[14px] font-semibold text-fg-on-accent"
							>
								<Radar size={16} aria-hidden="true" />
								{i18n.music_search_now()}
							</button>
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
						<button
							type="button"
							onclick={() => onMonitor(r)}
							aria-pressed={r.monitored}
							aria-label={r.monitored ? i18n.action_stop_monitoring() : i18n.action_monitor()}
							class={cn(
								"grid h-11 w-11 shrink-0 place-items-center rounded-lg border border-border bg-bg-elevated",
								r.monitored ? "text-accent-text" : "text-fg-subtle",
							)}
						>
							<Bookmark size={16} fill={r.monitored ? "currentColor" : "none"} aria-hidden="true" />
						</button>
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
							onSearch={onSearchTrack}
							onDeleteFile={onDeleteTrack}
						/>
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

{#if groups.albums.length > 0}
	<p class="px-4 pb-2 pt-5 font-mono text-[10.5px] uppercase tracking-[0.12em] text-fg-faint">
		{i18n.music_group_albums()} · {groups.albums.length}
	</p>
	{#each groups.albums as r (r.id)}
		{@render row(r)}
	{/each}
{/if}
{#if groups.others.length > 0}
	<p class="px-4 pb-2 pt-5 font-mono text-[10.5px] uppercase tracking-[0.12em] text-fg-faint">
		{i18n.music_group_others()} · {groups.others.length}
	</p>
	{#each groups.others as r (r.id)}
		{@render row(r)}
	{/each}
{/if}
