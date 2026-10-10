<script lang="ts">
	import { cn } from "@lib/cn";
	import ReleaseCover from "./ReleaseCover.svelte";
	import ReleaseMark from "./ReleaseMark.svelte";
	import { releaseGroups, releaseLine, releaseTypeLabel, type Release } from "@lib/music-books";
	import { albumPosterUrl } from "@lib/posters";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The artist page's left column from md: releases grouped the way seasons
	// are, the selected one opening on the right.
	let {
		releases,
		selectedId,
		onSelect,
	}: { releases: Release[]; selectedId: number | undefined; onSelect: (r: Release) => void } = $props();

	let groups = $derived(releaseGroups(releases));
</script>

{#snippet item(r: Release)}
	{@const sel = r.id === selectedId}
	{@const up = r.status === "upcoming"}
	<li>
		<button
			type="button"
			onclick={() => onSelect(r)}
			aria-current={sel ? "true" : undefined}
			class={cn(
				"flex w-full items-center gap-3 rounded-lg px-2.5 py-2 text-left transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
				sel ? "bg-accent-soft ring-1 ring-accent-line" : "hover:bg-surface",
			)}
		>
			<div class="w-10 shrink-0">
				<ReleaseCover
					src={albumPosterUrl(r.id)}
					dim={up || (r.status === "wanted" && r.tracks_have === 0)}
					dashed={up}
					class="rounded"
				/>
			</div>
			<div class="min-w-0 flex-1">
				<p class={cn("truncate text-[13px] font-medium", sel ? "text-accent-text" : "text-fg")}>{r.title}</p>
				<p class="truncate font-mono text-[10.5px] text-fg-subtle">
					{up ? releaseTypeLabel(r.type) : releaseLine(r)}
				</p>
			</div>
			<ReleaseMark release={r} />
		</button>
	</li>
{/snippet}

<nav aria-label={i18n.music_discography()} class="flex flex-col">
	{#if groups.albums.length > 0}
		<p class="px-2.5 pb-1 font-mono text-[10px] uppercase tracking-[0.12em] text-fg-faint">
			{i18n.music_group_albums()} · {groups.albums.length}
		</p>
		<ul class="flex flex-col gap-0.5">
			{#each groups.albums as r (r.id)}
				{@render item(r)}
			{/each}
		</ul>
	{/if}
	{#if groups.others.length > 0}
		<p class="px-2.5 pb-1 pt-4 font-mono text-[10px] uppercase tracking-[0.12em] text-fg-faint">
			{i18n.music_group_others()} · {groups.others.length}
		</p>
		<ul class="flex flex-col gap-0.5">
			{#each groups.others as r (r.id)}
				{@render item(r)}
			{/each}
		</ul>
	{/if}
</nav>
