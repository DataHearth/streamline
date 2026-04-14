<script lang="ts">
	import { Plus } from "@lucide/svelte";

	type Counts = { running: number; review: number };

	let {
		counts,
		onNewScan,
	}: { counts: Counts; onNewScan: () => void } = $props();

	// Two compact numeric cards, matching the prototype: a plain RUNNING count
	// and an amber-tinted REVIEW count that pulls attention to scans awaiting a
	// human decision.
	const CHIPS: { key: keyof Counts; label: string; warn: boolean }[] = [
		{ key: "running", label: "running", warn: false },
		{ key: "review", label: "review", warn: true },
	];
</script>

<header class="flex flex-wrap items-end justify-between gap-4">
	<div>
		<h1 class="text-2xl font-bold tracking-tight text-fg">Imports</h1>
		<p class="mt-1 text-sm text-fg-muted">
			Scan a directory and adopt media files into your library.
		</p>
	</div>

	<div class="flex flex-wrap items-center gap-4">
		<ul class="flex flex-wrap items-stretch gap-4" aria-label="Recent scan summary">
			{#each CHIPS as chip (chip.key)}
				<li
					class="stat rounded-md border border-border bg-bg-elevated px-3.5 py-1.5 text-center"
					class:warn={chip.warn}
				>
					<div
						class="stat-num font-mono text-xl font-bold tabular-nums leading-[1.1] text-fg"
					>
						{counts[chip.key]}
					</div>
					<div
						class="mt-0.5 font-mono text-[9.5px] uppercase tracking-[0.14em] text-fg-faint"
					>
						{chip.label}
					</div>
				</li>
			{/each}
		</ul>

		<button
			type="button"
			onclick={onNewScan}
			class="inline-flex items-center gap-1.5 rounded-md bg-accent px-3.5 py-2 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
		>
			<Plus size={16} aria-hidden="true" />
			New scan
		</button>
	</div>
</header>

<style>
	.stat.warn {
		border-color: color-mix(in oklab, var(--status-wanted) 30%, var(--border));
		background: color-mix(in oklab, var(--status-wanted) 6%, var(--bg-elevated));
	}
	.stat.warn .stat-num {
		color: var(--status-wanted);
	}
</style>
