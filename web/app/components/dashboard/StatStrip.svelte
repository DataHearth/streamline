<script lang="ts">
	import ProgressBar from "../shared/ProgressBar.svelte";
	import type { MovieCounts, QueueItem, DiskUsage } from "../../lib/types";

	let {
		counts,
		queue,
		disk,
	}: {
		counts?: MovieCounts;
		queue: QueueItem[];
		disk?: DiskUsage;
	} = $props();

	function sumSpeed(items: QueueItem[]): number {
		let total = 0;
		for (const q of items) {
			if (q.status !== "downloading" || !q.speed) continue;
			const n = parseFloat(q.speed);
			if (!Number.isNaN(n)) total += n;
		}
		return total;
	}

	let speed = $derived(sumSpeed(queue));
	let diskPct = $derived(
		typeof disk?.pct === "number" ? Math.max(0, Math.min(1, disk.pct / 100)) : 0,
	);

	// buildSparkline maps the cumulative library trend into the 100×24 viewBox.
	// min→max fills the available height; a flat or empty series (e.g. 0 movies)
	// renders as a straight line along the baseline instead of a fake slope.
	function buildSparkline(points: number[] | undefined): string {
		if (!points || points.length < 2) return "";
		const top = 3;
		const bottom = 21;
		const min = Math.min(...points);
		const max = Math.max(...points);
		const range = max - min || 1;
		const last = points.length - 1;
		return points
			.map((v, i) => {
				const x = (i / last) * 100;
				const y = bottom - ((v - min) / range) * (bottom - top);
				return `${i === 0 ? "M" : "L"}${x.toFixed(2)},${y.toFixed(2)}`;
			})
			.join(" ");
	}

	let sparkPath = $derived(buildSparkline(counts?.trend));
</script>

<section
	aria-label="Library stats"
	class="grid grid-cols-2 gap-3 md:grid-cols-4"
>
	<div
		class="relative overflow-hidden rounded-lg border border-border bg-bg-elevated px-5 py-[18px]"
	>
		<div class="font-mono text-[28px] font-bold tabular leading-none tracking-tight">
			{counts?.total ?? "—"}
		</div>
		<div
			class="mt-2 text-[11px] uppercase tracking-[0.1em] text-fg-subtle"
		>
			Movies
		</div>
		{#if sparkPath}
			<svg
				viewBox="0 0 100 24"
				preserveAspectRatio="none"
				aria-hidden="true"
				class="pointer-events-none absolute right-3 bottom-3 h-5 w-20 text-accent opacity-65"
			>
				<path
					d={sparkPath}
					fill="none"
					stroke="currentColor"
					stroke-width="1.4"
				/>
			</svg>
		{/if}
	</div>

	<div
		class="relative overflow-hidden rounded-lg border border-border bg-bg-elevated px-5 py-4"
	>
		<div
			class="font-mono text-3xl font-bold tabular leading-none tracking-tight text-status-downloading"
		>
			{counts?.downloading ?? "—"}
		</div>
		<div
			class="mt-2 flex items-center gap-1.5 text-[11px] uppercase tracking-[0.1em] text-fg-subtle"
		>
			Downloading
			<span
				aria-hidden="true"
				class="inline-block h-1.5 w-1.5 rounded-full bg-status-downloading motion-safe:animate-pulse"
			></span>
		</div>
		<div class="mt-1.5 font-mono text-[11.5px] text-fg-muted">
			↓ {speed.toFixed(1)} MB/s
		</div>
	</div>

	<div
		class="relative overflow-hidden rounded-lg border border-border bg-bg-elevated px-5 py-[18px]"
	>
		<div class="font-mono text-[28px] font-bold tabular leading-none tracking-tight">
			{counts?.wanted ?? "—"}
		</div>
		<div
			class="mt-2 text-[11px] uppercase tracking-[0.1em] text-fg-subtle"
		>
			Wanted
		</div>
	</div>

	<div
		class="relative overflow-hidden rounded-lg border border-border bg-bg-elevated px-5 py-[18px]"
	>
		<div class="font-mono text-[28px] font-bold tabular leading-none tracking-tight">
			{disk?.free ?? "—"}
		</div>
		<div
			class="mt-2 text-[11px] uppercase tracking-[0.1em] text-fg-subtle"
		>
			Free
		</div>
		{#if disk}
			<div class="mt-2.5">
				<ProgressBar value={diskPct} status="available" height={2} />
			</div>
		{/if}
	</div>
</section>
