<script lang="ts">
	import { Bookmark, Radar, Search } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import LabelPill from "@components/shared/LabelPill.svelte";
	import ReleaseCover from "./ReleaseCover.svelte";
	import Tracklist from "./Tracklist.svelte";
	import ReleaseCredits from "./ReleaseCredits.svelte";
	import { qualityNote, releaseFacts, releasePill, releaseTypeLabel, releaseYear, type Release, type Track } from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The selected release, right of the list from md. Two searches, as on the
	// series and book pages: Search now hands the release to the automatic
	// search, Manual search lists the results to pick one. Monitoring is per
	// release too.
	let {
		release: r,
		canEdit = false,
		onSearch,
		onManualSearch,
		onMonitor,
		onSearchTrack,
		onDeleteTrack,
	}: {
		release: Release;
		canEdit?: boolean;
		onSearch: () => void;
		onManualSearch: () => void;
		onMonitor: () => void;
		onSearchTrack?: (t: Track) => void;
		onDeleteTrack?: (t: Track) => void;
	} = $props();

	let up = $derived(r.status === "upcoming");
	let pill = $derived(releasePill(r));
	let facts = $derived(releaseFacts(r));
	const caps = "font-mono text-[11px] uppercase tracking-[0.08em] text-fg-muted";
</script>

<section aria-labelledby="release-title" class="min-w-0">
	<div class="flex items-start gap-4 rounded-lg border border-border bg-bg-elevated/70 p-4 lg:gap-5">
		<div class="w-[88px] shrink-0 lg:w-[112px]">
			<ReleaseCover src={r.cover_url} dim={up} dashed={up} />
		</div>
		<!-- A container, so Manual search can drop its label where this column
		     is narrow (tablet, and lg until ~1180px) instead of wrapping the
		     bookmark onto a line of its own. -->
		<div class="@container min-w-0 flex-1">
			<div class="flex flex-wrap items-center gap-2">
				<LabelPill token={pill.token} label={pill.label} size="md" variant="translucent" live={pill.live} />
				<span class={caps}>{releaseTypeLabel(r.type)}</span>
				{#if !up}
					<span class="text-fg-faint" aria-hidden="true">·</span>
					<span class={caps}>{releaseYear(r)}</span>
				{/if}
				{#if r.label}
					<span class="text-fg-faint" aria-hidden="true">·</span>
					<span class={caps}>{r.label}</span>
				{/if}
			</div>
			<h2 id="release-title" class="mt-2 text-xl font-bold tracking-tight text-fg lg:text-2xl">{r.title}</h2>
			<div class="mt-1.5 flex flex-wrap items-center gap-2 font-mono text-xs text-fg-muted">
				{#each facts as f, i (i)}
					{#if i > 0}<span class="text-fg-faint" aria-hidden="true">·</span>{/if}
					<span>{f}</span>
				{/each}
			</div>
			{#if canEdit}
				<!-- gap-1.5: at 1024 the column is 265 and French needs 179 + 36 + 36
				     plus the gaps, which gap-2 took to 267. -->
				<div class="mt-4 flex flex-wrap items-center gap-1.5">
					{#if !up}
						<button
							type="button"
							onclick={onSearch}
							class="inline-flex h-11 items-center gap-2 rounded-lg bg-accent px-3 text-[13px] font-semibold text-fg-on-accent transition hover:bg-accent-hover lg:h-9"
						>
							<Radar size={15} aria-hidden="true" />
							{i18n.music_search_now()}
						</button>
						<button
							type="button"
							onclick={onManualSearch}
							aria-label={i18n.action_manual_search()}
							title={i18n.action_manual_search()}
							class="inline-flex h-11 min-w-11 items-center justify-center gap-2 rounded-lg border border-border-strong bg-white/[0.06] px-2 text-[13px] font-medium text-fg transition hover:bg-white/[0.12] lg:h-9 lg:min-w-9 @md:px-3"
						>
							<Search size={15} aria-hidden="true" />
							<span class="hidden @md:inline">{i18n.action_manual_search()}</span>
						</button>
					{/if}
					<button
						type="button"
						onclick={onMonitor}
						aria-pressed={r.monitored}
						aria-label={r.monitored ? i18n.action_stop_monitoring() : i18n.action_monitor()}
						title={r.monitored ? i18n.action_stop_monitoring() : i18n.action_monitor()}
						class={cn(
							"grid h-11 w-11 place-items-center rounded-lg border border-border bg-bg-elevated/80 transition hover:border-border-strong lg:h-9 lg:w-9",
							r.monitored ? "text-accent-text" : "text-fg-subtle",
						)}
					>
						<Bookmark size={15} fill={r.monitored ? "currentColor" : "none"} aria-hidden="true" />
					</button>
					{#if up}
						<span class="text-[13px] text-fg-muted">
							{r.monitored ? i18n.music_upcoming_monitored() : i18n.music_upcoming_unmonitored()}
						</span>
					{/if}
				</div>
			{/if}
		</div>
	</div>

	<div class="mt-4">
		{#if r.tracks?.length}
			<Tracklist
				tracks={r.tracks}
				release={r}
				qualityNote={qualityNote(r)}
				markMissing={r.status === "wanted" && r.tracks_have > 0}
				{canEdit}
				onSearch={onSearchTrack}
				onDeleteFile={onDeleteTrack}
			/>
		{:else}
			<div class="rounded-lg border border-dashed border-border-strong px-6 py-10 text-center">
				<p class="text-[14px] font-medium text-fg-muted">{i18n.music_tracklist_unannounced()}</p>
				<p class="mt-1 text-[12.5px] text-fg-subtle">{i18n.music_tracklist_unannounced_hint()}</p>
			</div>
		{/if}
	</div>

	<div class="mt-6">
		<ReleaseCredits release={r} />
	</div>
</section>
