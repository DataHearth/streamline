<script lang="ts">
	import { untrack } from "svelte";
	import type { QueueEntry } from "@lib/types";
	import { formatSpeed, formatEta } from "@lib/format";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// onShowStatus turns the count tiles into filter shortcuts; shownStatus is the
	// lone status filter in effect, so its tile reads as pressed.
	let {
		items,
		onShowStatus,
		shownStatus = null,
	}: {
		items: QueueEntry[];
		onShowStatus?: (status: string) => void;
		shownStatus?: string | null;
	} = $props();

	const SPARK_KEY = "streamline:activity-spark";
	const SPARK_MAX = 60;

	function loadBuffer(): number[] {
		if (typeof sessionStorage === "undefined") return [];
		try {
			const raw = sessionStorage.getItem(SPARK_KEY);
			const arr = raw ? (JSON.parse(raw) as unknown) : [];
			return Array.isArray(arr) ? arr.map(Number).filter(Number.isFinite) : [];
		} catch {
			return [];
		}
	}

	let buffer = $state<number[]>(loadBuffer());

	let active = $derived(
		items.filter((i) => i.status === "downloading").length,
	);
	let importing = $derived(
		items.filter((i) => i.status === "importing").length,
	);
	let held = $derived(items.filter((i) => i.status === "held").length);
	let aggregate = $derived(
		items.reduce((s, i) => s + (i.download_speed ?? 0), 0),
	);
	let minEta = $derived.by(() => {
		const etas = items
			.map((i) => i.eta ?? 0)
			.filter((e) => e > 0);
		return etas.length ? Math.min(...etas) : 0;
	});

	// Push the freshly-observed aggregate speed onto the rolling buffer each
	// time the queue snapshot changes. Real samples only — no synthetic data.
	// untrack: depend on `aggregate` only, never on `buffer` (reading +
	// writing the same $state in one effect would self-trigger a loop).
	$effect(() => {
		const sample = aggregate;
		untrack(() => {
			const next = [...buffer, sample].slice(-SPARK_MAX);
			buffer = next;
			if (typeof sessionStorage !== "undefined") {
				try {
					sessionStorage.setItem(SPARK_KEY, JSON.stringify(next));
				} catch {
					// storage full / unavailable — sparkline is best-effort.
				}
			}
		});
	});

	let sparkPoints = $derived.by(() => {
		if (buffer.length < 2) return "";
		const max = Math.max(...buffer, 1);
		const w = 240;
		const h = 40;
		const step = w / (SPARK_MAX - 1);
		return buffer
			.map((v, idx) => {
				const x = idx * step;
				const y = h - (v / max) * h;
				return `${x.toFixed(1)},${y.toFixed(1)}`;
			})
			.join(" ");
	});
</script>

{#snippet countTile(
	value: number,
	label: string,
	status: string,
	tone = "text-fg",
)}
	{@const on = shownStatus === status}
	<button
		type="button"
		onclick={() => onShowStatus?.(status)}
		disabled={!onShowStatus}
		aria-pressed={on}
		class="-m-2 rounded-md p-2 text-left transition enabled:hover:bg-surface disabled:cursor-default"
	>
		<div class="text-2xl font-bold tabular-nums {tone}">{value}</div>
		<div
			class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] {on
				? 'text-accent-text'
				: 'text-fg-faint'}"
		>
			{label}
		</div>
	</button>
{/snippet}

<!-- The tiles and the sparkline are the md-and-up reading. Below that the four
     numbers collapse into TouchStatLine's single row: the sparkline needs width
     to say anything, and 390px isn't it. -->
<div
	class="mb-4 hidden grid-cols-2 items-center gap-4 rounded-lg border border-border bg-bg-elevated px-5 py-4 {held > 0
		? 'sm:grid-cols-5 lg:grid-cols-[repeat(5,auto)_1fr]'
		: 'sm:grid-cols-4 lg:grid-cols-[repeat(4,auto)_1fr]'} md:grid"
>
	{@render countTile(active, i18n.common_active(), "downloading")}
	{#if held > 0}
		{@render countTile(held, i18n.status_held(), "held", "text-status-held")}
	{/if}
	<div>
		<div class="text-2xl font-bold tabular-nums text-status-downloading">
			{formatSpeed(aggregate) || "—"}
		</div>
		<div
			class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint"
		>
			{i18n.torrent_aggregate_down()}
		</div>
	</div>
	{@render countTile(importing, i18n.activity_importing(), "importing")}
	<div>
		<div class="text-2xl font-bold tabular-nums text-fg">
			{formatEta(minEta) || "—"}
		</div>
		<div
			class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint"
		>
			{i18n.activity_next_eta()}
		</div>
	</div>
	<div
		class="col-span-2 h-10 lg:col-span-1 {held > 0 ? 'sm:col-span-5' : 'sm:col-span-4'}"
	>
		{#if sparkPoints}
			<svg
				viewBox="0 0 240 40"
				preserveAspectRatio="none"
				class="h-full w-full"
				aria-hidden="true"
			>
				<polyline
					points={sparkPoints}
					fill="none"
					stroke="var(--status-downloading)"
					stroke-width="1.5"
					vector-effect="non-scaling-stroke"
				/>
			</svg>
		{:else}
			<div class="h-px w-full bg-border" aria-hidden="true"></div>
		{/if}
	</div>
</div>
