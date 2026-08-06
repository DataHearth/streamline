<script lang="ts">
	import { Plus } from "@lucide/svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type Counts = { running: number; review: number };

	let {
		counts,
		onNewScan,
	}: { counts: Counts; onNewScan: () => void } = $props();

	// Two compact numeric cards, matching the prototype: a plain RUNNING count
	// and an amber-tinted REVIEW count that pulls attention to scans awaiting a
	// human decision.
	const CHIPS: { key: keyof Counts; label: string; warn: boolean }[] = [
		{ key: "running", label: i18n.lc_running(), warn: false },
		{ key: "review", label: i18n.lc_review(), warn: true },
	];
</script>

<header
	class="flex flex-col gap-4 md:flex-row md:flex-wrap md:items-end md:justify-between"
>
	<div>
		<h1 class="text-2xl font-bold tracking-tight text-fg">{i18n.imports_label()}</h1>
		<p class="mt-1 text-sm text-fg-muted">
			{i18n.imports_intro()}
		</p>
	</div>

	<div class="flex flex-col gap-3 md:flex-row md:flex-wrap md:items-center md:gap-4">
		<ul
			class="flex items-stretch gap-3 md:flex-wrap md:gap-4"
			aria-label={i18n.imports_scan_summary()}
		>
			{#each CHIPS as chip (chip.key)}
				<li
					class="stat flex-1 rounded-md border border-border bg-bg-elevated px-3.5 py-1.5 text-center md:flex-none"
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
			class="inline-flex h-11 w-full items-center justify-center gap-1.5 rounded-md bg-accent px-3.5 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring md:h-auto md:w-auto md:py-2"
		>
			<Plus size={16} aria-hidden="true" />
			{i18n.imports_new_scan()}
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
