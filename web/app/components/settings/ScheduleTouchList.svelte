<script lang="ts">
	import { groupSchedules } from "../../lib/schedules-touch";
	import type { Schedule } from "../../lib/types";
	import ScheduleTouchRow from "./ScheduleTouchRow.svelte";

	let {
		items,
		descriptions,
		onMenu,
	}: {
		items: Schedule[];
		descriptions: Record<string, string>;
		onMenu: (s: Schedule) => void;
	} = $props();

	let groups = $derived(groupSchedules(items));
</script>

<div class="lg:hidden">
	{#each groups as g (g.key)}
		<div class="flex items-center gap-2.5 pb-2 pt-5 first:pt-4">
			<h2
				class="font-mono text-[11px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
			>
				{g.label}
			</h2>
			{#if g.count}
				<span class="font-mono text-[11px] tabular-nums text-fg-faint">
					{g.rows.length}
				</span>
			{/if}
			<span class="h-px flex-1 bg-border" aria-hidden="true"></span>
		</div>
		<ul
			class="-mx-4 divide-y divide-border border-y border-border bg-bg-elevated sm:mx-0 sm:overflow-hidden sm:rounded-lg sm:border"
		>
			{#each g.rows as s (s.name)}
				<li>
					<ScheduleTouchRow
						row={s}
						description={descriptions[s.name]}
						{onMenu}
					/>
				</li>
			{/each}
		</ul>
	{/each}
</div>
