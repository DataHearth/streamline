<script lang="ts">
	import { TriangleAlert } from "@lucide/svelte";
	import ProgressBar from "../shared/ProgressBar.svelte";
	import { formatBytes } from "../../lib/format";
	import type { MovieCounts, QueueItem, DiskUsage } from "../../lib/types";

	let {
		counts,
		seriesTotal,
		monitoredTotal,
		queue,
		disks,
	}: {
		counts?: MovieCounts;
		seriesTotal?: number;
		monitoredTotal?: number;
		queue: QueueItem[];
		disks: { label: string; path: string; usage?: DiskUsage }[];
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

	let probed = $derived(disks.filter((d) => d.usage));

	// Movie and series paths usually share one mount, so summing their free bytes
	// would report the same space twice. Two probes reporting an identical
	// total/free pair are treated as one volume.
	// ponytail: string match stands in for the device id; swap in a statfs fsid
	// from the backend if two genuinely distinct volumes ever collide here.
	let volumes = $derived.by(() => {
		const seen = new Map<string, DiskUsage>();
		for (const d of probed) {
			const u = d.usage;
			if (u) seen.set(`${u.total}|${u.free}`, u);
		}
		return [...seen.values()];
	});

	// A single volume reuses the server's own string so the tile doesn't render
	// 45.8 GiB as "46 GB"; only a genuine multi-volume sum needs reformatting.
	let freeText = $derived(
		volumes.length === 1
			? (volumes[0]?.free ?? "—")
			: formatBytes(
					volumes.reduce((n, u) => n + (u.free_bytes ?? 0), 0),
					"—",
				),
	);
	let diskPct = $derived.by(() => {
		if (volumes.length === 0) return 0;
		const avg =
			volumes.reduce((n, u) => n + u.pct, 0) / volumes.length;
		return Math.max(0, Math.min(1, avg / 100));
	});

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
	let failedCount = $derived(counts?.failed ?? 0);
	let titleTotal = $derived(
		counts || seriesTotal !== undefined
			? (counts?.total ?? 0) + (seriesTotal ?? 0)
			: undefined,
	);
</script>

{#snippet spark()}
	{#if sparkPath}
		<svg
			viewBox="0 0 100 24"
			preserveAspectRatio="none"
			aria-hidden="true"
			class="pointer-events-none absolute right-3 bottom-3 h-5 w-20 text-accent opacity-65"
		>
			<path d={sparkPath} fill="none" stroke="currentColor" stroke-width="1.4" />
		</svg>
	{/if}
{/snippet}

<section
	aria-label="Library stats"
	class="grid grid-cols-2 gap-3 md:grid-cols-4"
>
	<div
		class="relative overflow-hidden rounded-lg border border-border bg-bg-elevated px-5 py-[18px]"
	>
		<div class="font-mono text-[28px] font-bold tabular leading-none tracking-tight">
			{titleTotal ?? "—"}
		</div>
		<div class="mt-2 text-[11px] uppercase tracking-[0.1em] text-fg-subtle">
			Titles
		</div>
		<div class="mt-1.5 font-mono text-[11.5px] text-fg-muted">
			{counts?.total ?? 0}
			movie{(counts?.total ?? 0) === 1 ? "" : "s"} · {seriesTotal ?? 0} series
		</div>
		{@render spark()}
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

	<!-- Failed titles ride along here — the strip is a fixed four across (2×2 on
	     mobile) and a fifth tile would orphan onto a second row at every
	     breakpoint. Hidden at zero so a healthy library carries no noise. -->
	<div
		class="relative overflow-hidden rounded-lg border border-border bg-bg-elevated px-5 py-[18px]"
	>
		<div class="font-mono text-[28px] font-bold tabular leading-none tracking-tight">
			{monitoredTotal ?? "—"}
		</div>
		<div
			class="mt-2 text-[11px] uppercase tracking-[0.1em] text-fg-subtle"
		>
			Monitored
		</div>
		{@render spark()}
		{#if failedCount > 0}
			<div class="mt-1.5">
				<a
					href="/movies?status=failed"
					class="inline-flex items-center gap-1 font-mono text-[11.5px] text-status-failed underline-offset-2 transition hover:underline"
				>
					<TriangleAlert size={12} aria-hidden="true" />
					{failedCount} failed
				</a>
			</div>
		{/if}
	</div>

	<div
		class="relative overflow-hidden rounded-lg border border-border bg-bg-elevated px-5 py-[18px]"
	>
		<div class="font-mono text-[28px] font-bold tabular leading-none tracking-tight">
			{volumes.length > 0 ? freeText : "—"}
		</div>
		<div class="mt-2 text-[11px] uppercase tracking-[0.1em] text-fg-subtle">
			Free
		</div>
		{#if volumes.length > 1}
			<dl class="mt-1.5 space-y-0.5">
				{#each probed as d (d.label)}
					<div class="flex min-w-0 items-baseline gap-1.5">
						<dt class="shrink-0 font-mono text-[10px] text-fg-subtle">
							{d.label}
						</dt>
						<dd
							class="truncate font-mono text-[10px] text-fg-faint"
							title={d.path}
						>
							{d.usage?.free} · {d.path}
						</dd>
					</div>
				{/each}
			</dl>
		{/if}
		{#if volumes.length > 0}
			<div class="mt-2.5">
				<ProgressBar value={diskPct} status="available" height={2} />
			</div>
		{/if}
	</div>
</section>
