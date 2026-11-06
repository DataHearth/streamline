<script lang="ts">
	import { fmtPct, share, type MeterSlice } from "@lib/stats";

	// A stacked bar and its legend, one slice per row with its detail. The bar
	// is decoration (aria-hidden); the legend carries every figure, so the
	// colours never have to be read alone.
	let {
		slices,
		label,
		total,
	}: {
		slices: MeterSlice[];
		label: string;
		total?: number;
	} = $props();

	let sum = $derived(total ?? slices.reduce((a, s) => a + s.value, 0));
</script>

<div class="min-w-0">
	<div class="flex h-2 w-full gap-px overflow-hidden rounded-full bg-white/[0.06]" aria-hidden="true">
		{#each slices as s (s.key)}
			{#if s.value > 0}
				<div class="h-full" style:width="{share(s.value, sum)}%" style:background={s.color}></div>
			{/if}
		{/each}
	</div>

	<ul aria-label={label} class="mt-3 flex flex-col gap-1.5">
		{#each slices as s (s.key)}
			<li class="flex min-w-0 items-center gap-2.5 text-[13px]">
				<span class="h-2 w-2 shrink-0 rounded-full" style:background={s.color} aria-hidden="true"></span>
				<span class="min-w-0 flex-1 truncate text-fg-muted">{s.label}</span>
				{#if s.detail}
					<span class="shrink-0 font-mono text-[11.5px] tabular-nums text-fg-subtle">{s.detail}</span>
				{/if}
				<span class="w-12 shrink-0 text-right font-mono text-[12px] tabular-nums text-fg">
					{fmtPct(s.value, sum)}
				</span>
			</li>
		{/each}
	</ul>
</div>
