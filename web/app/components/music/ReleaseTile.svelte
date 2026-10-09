<script lang="ts">
	import { cn } from "@lib/cn";
	import { formatDateShort } from "@lib/dates";
	import LabelPill from "@components/shared/LabelPill.svelte";
	import ReleaseCover from "./ReleaseCover.svelte";
	import { releaseTypeLabel, releaseYear, type Release } from "@lib/music-books";
	import { albumPosterUrl } from "@lib/posters";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// One release in an artist row. A cover with no pill is complete; the pill
	// only appears for what still needs something.
	let { release: r, href, width }: { release: Release; href: string; width: number } = $props();

	let up = $derived(r.status === "upcoming");
	let none = $derived(r.status === "wanted" && r.tracks_have === 0);
	let badge = $derived.by(() => {
		if (up) return { token: "unaired", label: formatDateShort(r.release_date), live: false };
		if (r.status === "wanted")
			return {
				token: "wanted",
				label: r.tracks_have > 0 ? `${r.tracks_have}/${r.track_count}` : i18n.status_wanted(),
				live: false,
			};
		if (r.status === "downloading")
			return { token: "downloading", label: `${Math.round(r.progress ?? 0)}%`, live: true };
		return null;
	});
</script>

<a
	{href}
	class="group/tile block shrink-0 rounded-md focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
	style:width="{width}px"
	title={r.title}
>
	<div class="relative overflow-hidden rounded-md transition group-hover/tile:brightness-110">
		<ReleaseCover src={albumPosterUrl(r.id)} dim={up || none} dashed={up} />
		{#if badge}
			<span class="absolute left-1.5 top-1.5">
				<LabelPill token={badge.token} label={badge.label} live={badge.live} />
			</span>
		{/if}
		{#if r.status === "downloading"}
			<div class="absolute inset-x-0 bottom-0 h-0.5 bg-white/10">
				<div class="h-full bg-status-downloading" style:width="{r.progress ?? 0}%"></div>
			</div>
		{/if}
	</div>
	<p class={cn("mt-1.5 truncate text-[12.5px] font-medium", up || none ? "text-fg-muted" : "text-fg")}>
		{r.title}
	</p>
	<p class="truncate font-mono text-[10.5px] text-fg-subtle">
		{up ? releaseTypeLabel(r.type) : `${releaseYear(r)} · ${releaseTypeLabel(r.type)}`}
	</p>
</a>
