<script lang="ts">
	import type { Season } from "../../lib/types";

	let {
		season,
		height = 4,
	}: {
		season: Season;
		height?: 1 | 1.5 | 2 | 4;
	} = $props();

	type Seg = { key: string; count: number; token: string; label: string };

	// Prefer the per-episode breakdown (detail view, where episodes are loaded)
	// so the bar differentiates downloading/paused; fall back to the rollup
	// counts in list views that ship only the season summary.
	let segments = $derived.by<Seg[]>(() => {
		const eps = season.episodes ?? [];
		if (eps.length > 0) {
			const n = (s: string) => eps.filter((e) => e.status === s).length;
			return [
				{ key: "available", count: n("available"), token: "available", label: "available" },
				{ key: "downloading", count: n("downloading"), token: "downloading", label: "downloading" },
				{ key: "paused", count: n("paused"), token: "paused", label: "paused" },
				{ key: "wanted", count: n("wanted"), token: "wanted", label: "missing" },
				{ key: "unaired", count: n("unaired"), token: "missing", label: "unaired" },
			];
		}
		return [
			{ key: "available", count: season.available ?? 0, token: "available", label: "available" },
			{ key: "wanted", count: season.missing ?? 0, token: "wanted", label: "missing" },
			{ key: "unaired", count: season.unaired ?? 0, token: "missing", label: "unaired" },
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
	class="seg-track flex w-full overflow-hidden rounded-full bg-white/[0.06]"
	style:--h="{height}px"
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
	.seg-track {
		height: var(--h);
	}
	.seg {
		background-color: var(--c);
	}
	/* Hairline separators so adjacent segments stay legible. */
	.seg:not(:last-child) {
		box-shadow: 1px 0 0 var(--bg-deep);
	}
</style>
