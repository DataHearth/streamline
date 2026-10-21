<script lang="ts" module>
	// onClick makes a stat a filter shortcut; pressed marks the one in effect.
	export type TouchStat = {
		value: string;
		label: string;
		color?: string;
		onClick?: () => void;
		pressed?: boolean;
	};
</script>

<script lang="ts">
	// Below md the four stat tiles and the sparkline collapse to one hairline-split
	// line. The sparkline needs width to say anything, so it starts at md; these
	// numbers are the part that still reads at 390px.
	let { stats }: { stats: TouchStat[] } = $props();
</script>

{#snippet body(s: TouchStat)}
	<div
		class="truncate font-mono text-[15px] font-semibold tabular-nums"
		style:color={s.color ?? "var(--fg)"}
	>
		{s.value}
	</div>
	<div
		class="mt-px truncate text-[9px] font-medium uppercase tracking-[0.13em] {s.pressed
			? 'text-accent-text'
			: 'text-fg-faint'}"
	>
		{s.label}
	</div>
{/snippet}

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
			{#if s.onClick}
				<button
					type="button"
					onclick={s.onClick}
					aria-pressed={s.pressed ?? false}
					class="-my-1 block w-full min-w-0 rounded-md py-1 text-left transition active:bg-surface"
				>
					{@render body(s)}
				</button>
			{:else}
				{@render body(s)}
			{/if}
		</div>
	{/each}
</div>
