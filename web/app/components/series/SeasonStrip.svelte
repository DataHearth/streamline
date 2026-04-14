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

<div class="flex gap-2 overflow-x-auto pb-1">
	{#each seasons as s (s.number)}
		{@const active = selected === s.number}
		<button
			type="button"
			onclick={() => onSelect(s.number)}
			aria-current={active ? "true" : undefined}
			class={cn(
				"flex w-40 shrink-0 flex-col gap-1.5 rounded-lg border p-3 text-left transition",
				active
					? "border-accent/60 bg-accent-soft"
					: "border-border bg-bg-elevated hover:border-border-strong",
			)}
		>
			<div class="font-mono text-xs font-semibold text-fg">
				{s.number === 0 ? "SPECIALS" : `Season ${pad(s.number)}`}
			</div>
			{#if s.name && s.number !== 0}
				<div class="truncate text-[11px] text-fg-subtle">{s.name}</div>
			{/if}
			<div class="font-mono text-[11px] text-fg-muted">
				<span class="text-fg">{s.available ?? 0}</span>/{s.total ?? 0}
				{#if (s.missing ?? 0) > 0}
					<span class="text-status-wanted">· {s.missing} miss</span>
				{/if}
				{#if (s.unaired ?? 0) > 0}
					<span class="text-fg-faint">· {s.unaired} fut</span>
				{/if}
			</div>
			<SeasonProgress season={s} height={1} />
		</button>
	{/each}
</div>
