<script lang="ts">
	import { cn } from "../../lib/cn";
	import SeasonProgress from "./SeasonProgress.svelte";
	import type { Season } from "../../lib/types";

	let {
		seasons,
		selected,
		onSelect,
	}: {
		seasons: Season[];
		selected: number;
		onSelect: (n: number) => void;
	} = $props();

	function pad(n: number): string {
		return String(n).padStart(2, "0");
	}
</script>

<div
	class="grid gap-3 grid-cols-[repeat(auto-fill,minmax(220px,1fr))]"
>
	{#each seasons as s (s.number)}
		{@const complete = (s.missing ?? 0) === 0 && (s.unaired ?? 0) === 0}
		<button
			type="button"
			onclick={() => onSelect(s.number)}
			aria-current={selected === s.number ? "true" : undefined}
			class={cn(
				"flex flex-col gap-3 rounded-lg border p-4 text-left transition hover:border-border-strong",
				selected === s.number
					? "border-accent/60 bg-accent-soft"
					: "border-border bg-bg-elevated",
			)}
		>
			<div class="flex items-center justify-between">
				<span class="font-mono text-xs font-semibold tracking-wide text-fg">
					{s.number === 0 ? "SPECIALS" : `SEASON ${pad(s.number)}`}
				</span>
			</div>
			{#if s.name && s.number !== 0}
				<span class="truncate text-sm text-fg-muted">{s.name}</span>
			{/if}
			<div class="flex items-baseline gap-1">
				<span class="font-mono text-2xl font-semibold tabular text-fg">
					{s.available ?? 0}
				</span>
				<span class="font-mono text-sm text-fg-faint">/{s.total ?? 0}</span>
				<span class="ml-1 font-mono text-[10px] uppercase tracking-wide text-fg-faint">
					episodes
				</span>
			</div>
			<SeasonProgress season={s} height={1.5} />
			<div class="flex flex-wrap items-center gap-x-2 font-mono text-[11px]">
				{#if (s.missing ?? 0) > 0}
					<span class="text-status-wanted">{s.missing} missing</span>
				{/if}
				{#if (s.unaired ?? 0) > 0}
					<span class="text-fg-faint">{s.unaired} unaired</span>
				{/if}
				{#if complete}
					<span class="text-status-available">complete</span>
				{/if}
			</div>
		</button>
	{/each}
</div>
