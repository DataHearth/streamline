<script lang="ts">
	import { onDestroy, tick } from "svelte";
	import { scale } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { Info, TriangleAlert } from "@lucide/svelte";
	import ProgressBar from "../shared/ProgressBar.svelte";
	import { cn } from "../../lib/cn";
	import { formatBytes, formatSpeed } from "../../lib/format";
	import type {
		MovieCounts,
		TVShowCounts,
		QueueEntry,
		DiskUsage,
	} from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const movieCount = (n: number) =>
		n === 1
			? i18n.dash_movie_count_one({ count: n })
			: i18n.dash_movie_count_other({ count: n });

	const seriesCount = (n: number) =>
		n === 1
			? i18n.dash_series_count_one({ count: n })
			: i18n.dash_series_count_other({ count: n });

	let {
		counts,
		seriesCounts,
		monitoredMovies,
		monitoredSeries,
		queue,
		disks,
	}: {
		counts?: MovieCounts;
		seriesCounts?: TVShowCounts;
		monitoredMovies?: number;
		monitoredSeries?: number;
		queue: QueueEntry[];
		disks: { label: string; path: string; usage?: DiskUsage }[];
	} = $props();

	// Bytes/sec across everything actually moving. Paused and importing entries
	// carry a stale rate, so only downloading counts toward the figure.
	function sumSpeed(items: QueueEntry[]): number {
		let total = 0;
		for (const q of items) {
			if (q.status !== "downloading") continue;
			total += q.download_speed ?? 0;
		}
		return total;
	}

	let speed = $derived(sumSpeed(queue));
	let speedText = $derived(formatSpeed(speed));

	// Counted off the same queue the speed and the Live queue panel below read,
	// not off `counts.downloading` + `downloading_episodes`: those are media-row
	// statuses, which drift from the records actually in flight (a replace grab
	// leaves its episode `available`), so the tile read 1 beside a two-item
	// queue. One record is one download here — a season pack counts once.
	let downloading = $derived(
		queue.filter((q) => q.status === "downloading").length,
	);

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
	// "347.9 GiB" at 28px is wider than a quarter-width tile, so the unit rides
	// smaller on the baseline and the value keeps clear of the ⓘ button.
	let freeParts = $derived(/^([\d.,]+)\s*(\S+)$/.exec(freeText));
	let diskPct = $derived.by(() => {
		if (volumes.length === 0) return 0;
		const avg = volumes.reduce((n, u) => n + u.pct, 0) / volumes.length;
		return Math.max(0, Math.min(1, avg / 100));
	});

	// Per-volume breakdown lives behind an ⓘ disclosure rather than under the
	// free-space figure: two paths pushed the tile taller than its three
	// siblings, and the detail is a lookup, not a glance.
	// Opens on hover of the ⓘ and sits above the tile. The short leave delay
	// covers the 12px the pointer crosses between trigger and panel; click pins
	// it open so the paths can be selected without holding the hover.
	let diskHover = $state(false);
	let diskPinned = $state(false);
	let diskOpen = $derived(diskHover || diskPinned);
	let diskBtnEl = $state<HTMLButtonElement | null>(null);
	let diskPanelEl = $state<HTMLElement | null>(null);
	let leaveTimer: ReturnType<typeof setTimeout> | undefined;

	function diskEnter() {
		clearTimeout(leaveTimer);
		diskHover = true;
	}
	function diskLeave() {
		clearTimeout(leaveTimer);
		leaveTimer = setTimeout(() => (diskHover = false), 140);
	}

	onDestroy(() => {
		clearTimeout(leaveTimer);
		window.removeEventListener("scroll", placeDisk, true);
		window.removeEventListener("resize", placeDisk);
	});

	// Prefer opening upward, but the strip sits inside a scrolling <main>: when
	// the tile is near the top of that viewport there is no room above and the
	// panel would be clipped out of sight, so it flips below.
	let diskAbove = $state(true);
	const DISK_GAP = 6;
	function placeDisk() {
		if (!diskBtnEl) return;
		const card = diskBtnEl.parentElement;
		if (!card) return;
		const scroller = card.closest("main") ?? document.documentElement;
		const top =
			card.getBoundingClientRect().top - scroller.getBoundingClientRect().top;
		diskAbove = top >= (diskPanelEl?.offsetHeight ?? 105) + DISK_GAP;
	}

	$effect(() => {
		if (!diskOpen) {
			window.removeEventListener("scroll", placeDisk, true);
			window.removeEventListener("resize", placeDisk);
			return;
		}
		placeDisk();
		tick().then(placeDisk);
		window.addEventListener("scroll", placeDisk, true);
		window.addEventListener("resize", placeDisk);
		return () => {
			window.removeEventListener("scroll", placeDisk, true);
			window.removeEventListener("resize", placeDisk);
		};
	});

	$effect(() => {
		if (!diskOpen) return;
		const onDown = (e: MouseEvent) => {
			const t = e.target as Node;
			if (diskBtnEl?.contains(t) || diskPanelEl?.contains(t)) return;
			diskPinned = false;
			diskHover = false;
		};
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") {
				diskBtnEl?.focus();
				clearTimeout(leaveTimer);
				diskPinned = false;
				diskHover = false;
			}
		};
		document.addEventListener("mousedown", onDown);
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("mousedown", onDown);
			document.removeEventListener("keydown", onKey);
		};
	});

	let failedCount = $derived(counts?.failed ?? 0);
	let monitoredTotal = $derived(
		monitoredMovies !== undefined || monitoredSeries !== undefined
			? (monitoredMovies ?? 0) + (monitoredSeries ?? 0)
			: undefined,
	);
	let titleTotal = $derived(
		counts || seriesCounts
			? (counts?.total ?? 0) + (seriesCounts?.total ?? 0)
			: undefined,
	);
</script>

<!-- Four across only from lg. In the tablet band the content column is 682px,
     which makes a quarter-width tile 161px — narrower than the phone's 173px,
     and too narrow for the movie/series sub-line. -->
<section
	aria-label={i18n.dash_library_stats()}
	class="grid grid-cols-2 gap-3 lg:grid-cols-4"
>
	<div
		class="relative overflow-hidden rounded-lg border border-border bg-bg-elevated px-4 py-[18px] md:px-5"
	>
		<div class="font-mono text-[28px] font-bold tabular leading-none tracking-tight">
			{titleTotal ?? "—"}
		</div>
		<div class="mt-2 text-[11px] uppercase tracking-[0.1em] text-fg-subtle">
			{i18n.dash_titles()}
		</div>
		<div
			class="mt-1.5 truncate font-mono text-[11px] text-fg-muted md:text-[11.5px]"
		>
			{movieCount(counts?.total ?? 0)} · {seriesCount(seriesCounts?.total ?? 0)}
		</div>
	</div>

	<div
		class="relative overflow-hidden rounded-lg border border-border bg-bg-elevated px-4 py-4 md:px-5"
	>
		<div
			class="font-mono text-3xl font-bold tabular leading-none tracking-tight text-status-downloading"
		>
			{downloading}
		</div>
		<div
			class="mt-2 flex items-center gap-1.5 text-[11px] uppercase tracking-[0.1em] text-fg-subtle"
		>
			{i18n.status_downloading()}
			<span
				aria-hidden="true"
				class="inline-block h-1.5 w-1.5 rounded-full bg-status-downloading motion-safe:animate-pulse"
			></span>
		</div>
		<div class="mt-1.5 font-mono text-[11.5px] text-fg-muted">
			{speedText ? `↓ ${speedText}` : i18n.lc_idle()}
		</div>
	</div>

	<!-- Failed titles ride along here — the strip is a fixed four across (2×2 on
	     mobile) and a fifth tile would orphan onto a second row at every
	     breakpoint. Hidden at zero so a healthy library carries no noise. -->
	<div
		class="relative overflow-hidden rounded-lg border border-border bg-bg-elevated px-4 py-[18px] md:px-5"
	>
		<div class="font-mono text-[28px] font-bold tabular leading-none tracking-tight">
			{monitoredTotal ?? "—"}
		</div>
		<div class="mt-2 text-[11px] uppercase tracking-[0.1em] text-fg-subtle">
			{i18n.monitor_monitored()}
		</div>
		<div
			class="mt-1.5 flex items-baseline gap-2.5 font-mono text-[11px] md:text-[11.5px]"
		>
			<span class="truncate text-fg-muted">
				{movieCount(monitoredMovies ?? 0)} · {seriesCount(monitoredSeries ?? 0)}
			</span>
			{#if failedCount > 0}
				<a
					href="/movies?status=failed"
					class="inline-flex shrink-0 items-center gap-1 text-status-failed underline-offset-2 transition hover:underline"
				>
					<TriangleAlert size={12} aria-hidden="true" />
					{failedCount} failed
				</a>
			{/if}
		</div>
	</div>

	<div
		class="relative rounded-lg border border-border bg-bg-elevated px-4 py-[18px] md:px-5"
	>
		{#if volumes.length > 0}
			<button
				bind:this={diskBtnEl}
				type="button"
				aria-label={i18n.dash_free_space()}
				aria-expanded={diskOpen}
				onclick={() => (diskPinned = !diskPinned)}
				onpointerenter={diskEnter}
				onpointerleave={diskLeave}
				onfocus={diskEnter}
				onblur={diskLeave}
				class={cn(
					"absolute right-1.5 top-1.5 z-20 grid h-10 w-10 lg:h-7 lg:w-7 place-items-center rounded-md transition hover:bg-surface hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
					diskOpen ? "bg-surface text-fg" : "text-fg-subtle",
				)}
			>
				<Info size={13} aria-hidden="true" />
			</button>
			{#if diskOpen}
				<dl
					bind:this={diskPanelEl}
					in:scale={{ duration: 140, start: 0.94, opacity: 0, easing: cubicOut }}
					out:scale={{ duration: 100, start: 0.96, opacity: 0, easing: cubicOut }}
					style:transform-origin={diskAbove ? "bottom right" : "top right"}
					onpointerenter={diskEnter}
					onpointerleave={diskLeave}
					class={cn(
						"absolute right-2 z-30 w-60 space-y-2.5 rounded-md border border-border-strong bg-bg-elevated p-3 shadow-4",
						diskAbove ? "bottom-full mb-1.5" : "top-full mt-1.5",
					)}
				>
					{#each probed as d (d.label)}
						<div class="min-w-0">
							<div class="flex items-baseline justify-between gap-3">
								<dt
									class="shrink-0 text-[11px] uppercase tracking-[0.1em] text-fg-subtle"
								>
									{d.label}
								</dt>
								<dd
									class="shrink-0 whitespace-nowrap font-mono text-[11.5px] text-fg"
								>
									{d.usage?.free} free
								</dd>
							</div>
							<dd
								class="mt-0.5 truncate font-mono text-[10px] text-fg-faint"
								title={d.path}
							>
								{d.path}
							</dd>
						</div>
					{/each}
				</dl>
			{/if}
		{/if}
		<div
			class="flex items-baseline gap-1 pr-7 font-mono text-[28px] font-bold tabular leading-none tracking-tight"
		>
			{#if volumes.length === 0}
				—
			{:else if freeParts}
				<span class="truncate">{freeParts[1]}</span>
				<span class="shrink-0 text-[13px] font-semibold text-fg-muted">
					{freeParts[2]}
				</span>
			{:else}
				<span class="truncate">{freeText}</span>
			{/if}
		</div>
		<div class="mt-2 text-[11px] uppercase tracking-[0.1em] text-fg-subtle">
			{i18n.dash_free()}
		</div>
		{#if volumes.length > 0}
			<div class="mt-2.5">
				<ProgressBar value={diskPct} status="available" height={2} />
			</div>
		{/if}
	</div>
</section>
