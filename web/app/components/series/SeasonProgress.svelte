<script lang="ts">
	import { episodeStatus } from "../../lib/status";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import type { EpisodeDisplayStatus } from "../../lib/status";
	import type { Season } from "../../lib/types";

	let { season }: { season: Season } = $props();

	type Seg = { key: string; count: number; token: string; label: string };

	// Prefer the per-episode breakdown (detail view, where episodes are loaded)
	// so the bar differentiates downloading/paused; fall back to the rollup
	// counts in list views that ship only the season summary.
	let segments = $derived.by<Seg[]>(() => {
		const eps = season.episodes ?? [];
		if (eps.length > 0) {
			const n = (s: EpisodeDisplayStatus) =>
				eps.filter((e) => episodeStatus(e) === s).length;
			return [
				{ key: "available", count: n("available"), token: "available", label: i18n.lc_available() },
				{ key: "downloading", count: n("downloading"), token: "downloading", label: i18n.lc_downloading() },
				{ key: "paused", count: n("paused"), token: "paused", label: i18n.lc_paused() },
				{ key: "wanted", count: n("wanted"), token: "wanted", label: i18n.lc_wanted() },
				{ key: "missing", count: n("missing"), token: "missing", label: i18n.lc_missing() },
				{ key: "unaired", count: n("unaired"), token: "missing", label: i18n.lc_unaired() },
			];
		}
		// Rollup-only fallback: season.missing counts monitored fileless episodes,
		// which is the "wanted" bucket — the unmonitored split needs episodes.
		return [
			{ key: "available", count: season.available ?? 0, token: "available", label: i18n.lc_available() },
			{ key: "wanted", count: season.missing ?? 0, token: "wanted", label: i18n.lc_wanted() },
			{ key: "unaired", count: season.unaired ?? 0, token: "missing", label: i18n.lc_unaired() },
		];
	});

	let total = $derived(
		Math.max(
			segments.reduce((acc, s) => acc + s.count, 0),
			season.total ?? 0,
			1,
		),
	);
	let visible = $derived(segments.filter((s) => s.count > 0));
	let summary = $derived(
		visible.map((s) => `${s.count} ${s.label}`).join(", ") || "no episodes",
	);
</script>

<div
	class="flex h-1 w-full overflow-hidden rounded-full bg-white/[0.06]"
	role="img"
	aria-label="Season progress: {summary}"
>
	{#each visible as s (s.key)}
		<span
			class="seg h-full first:rounded-l-full last:rounded-r-full"
			style:width="{(s.count / total) * 100}%"
			style:--c="var(--status-{s.token})"
			title="{s.count} {s.label}"
		></span>
	{/each}
</div>

<style>
	.seg {
		background-color: var(--c);
	}
	/* Hairline separators so adjacent segments stay legible. */
	.seg:not(:last-child) {
		box-shadow: 1px 0 0 var(--bg-deep);
	}
</style>
