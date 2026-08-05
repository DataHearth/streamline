<script lang="ts" module>
	export type TouchStat = { value: string; label: string; color?: string };
</script>

<script lang="ts">
	// Below md the four stat tiles and the sparkline collapse to one hairline-split
	// line. The sparkline needs width to say anything, so it starts at md; these
	// numbers are the part that still reads at 390px.
	let { stats }: { stats: TouchStat[] } = $props();
</script>

<div
	class="mt-3 flex items-center rounded-lg border border-border bg-bg-elevated px-3 py-2 md:hidden"
>
	{#each stats as s, i (s.label)}
		<div
			class="min-w-0 flex-1"
			class:border-l={i > 0}
			class:border-border={i > 0}
			class:pl-3={i > 0}
		>
			<div
				class="truncate font-mono text-[15px] font-semibold tabular-nums"
				style:color={s.color ?? "var(--fg)"}
			>
				{s.value}
			</div>
			<div
				class="mt-px truncate text-[9px] font-medium uppercase tracking-[0.13em] text-fg-faint"
			>
				{s.label}
			</div>
		</div>
	{/each}
</div>
