<script lang="ts">
	import { cn } from "../../lib/cn";
	import { missingEpisodes } from "../../lib/status";
	import SeasonProgress from "./SeasonProgress.svelte";
	import type { Season } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		seasons,
		selected,
		onSelect,
		vertical = false,
	}: {
		seasons: Season[];
		selected: number;
		onSelect: (n: number) => void;
		// The tablet two-pane layout stacks the strip down the left instead of
		// scrolling it across the top.
		vertical?: boolean;
	} = $props();

	function pad(n: number): string {
		return String(n).padStart(2, "0");
	}
</script>

<div
	class={vertical
		? "flex flex-col gap-2"
		: "flex gap-2 overflow-x-auto pb-1"}
>
	{#each seasons as s (s.number)}
		{@const active = selected === s.number}
		{@const missing = missingEpisodes(s.episodes ?? [])}
		<button
			type="button"
			onclick={() => onSelect(s.number)}
			aria-current={active ? "true" : undefined}
			class={cn(
				"flex flex-col gap-1.5 rounded-lg border p-3 text-left transition",
				vertical ? "w-full" : "w-48 shrink-0",
				active
					? "border-accent/60 bg-accent-soft"
					: "border-border bg-bg-elevated hover:border-border-strong",
			)}
		>
			<div class="font-mono text-xs font-semibold text-fg">
				{s.number === 0 ? i18n.series_specials_caps() : i18n.season_number({ number: pad(s.number) })}
			</div>
			{#if s.name && s.number !== 0}
				<div class="truncate text-[11px] text-fg-subtle">{s.name}</div>
			{/if}
			<div class="font-mono text-[11px] text-fg-muted">
				<span class="text-fg">{s.available ?? 0}</span>/{s.total ?? 0}
				{#if (s.missing ?? 0) > 0}
					<span class="text-status-wanted">· {s.missing} wanted</span>
				{/if}
				{#if missing > 0}
					<span class="text-status-missing">· {missing} missing</span>
				{/if}
				{#if (s.unaired ?? 0) > 0}
					<span class="text-fg-faint">· {s.unaired} future</span>
				{/if}
			</div>
			<SeasonProgress season={s} />
		</button>
	{/each}
</div>
