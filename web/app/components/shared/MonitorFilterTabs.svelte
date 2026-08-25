<script lang="ts">
	import { Eye, EyeOff, Layers } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import type { MonitorFilter } from "../../lib/types";

	let {
		value,
		monitoredCount,
		unmonitoredCount,
		onChange,
	}: {
		value: MonitorFilter;
		monitoredCount: number;
		unmonitoredCount: number;
		onChange: (v: MonitorFilter) => void;
	} = $props();

	let options = $derived<
		{ key: MonitorFilter; label: string; count?: number }[]
	>([
		{ key: "all", label: i18n.common_all() },
		{
			key: "monitored",
			label: i18n.monitor_monitored(),
			count: monitoredCount,
		},
		{
			key: "unmonitored",
			label: i18n.monitor_unmonitored(),
			count: unmonitoredCount,
		},
	]);
</script>

<!-- A group, not a toggle: the boolean it replaced could reach "monitored" and
     "everything", so an unmonitored-only library was unreachable — and a
     three-way cycle on one button hides the states it can reach. -->
<div
	class="flex shrink-0 items-center gap-0.5 rounded-md border border-border bg-bg-elevated p-1"
	role="group"
	aria-label={i18n.filter_show()}
>
	{#each options as opt (opt.key)}
		{@const on = value === opt.key}
		<button
			type="button"
			onclick={() => onChange(opt.key)}
			aria-pressed={on}
			class={cn(
				"inline-flex h-7 items-center gap-1.5 whitespace-nowrap rounded-sm px-2.5 text-[12.5px] font-medium transition",
				on
					? "bg-accent-soft text-accent-text"
					: "text-fg-muted hover:text-fg",
			)}
		>
			{#if opt.key === "monitored"}
				<Eye size={13} aria-hidden="true" />
			{:else if opt.key === "unmonitored"}
				<EyeOff size={13} aria-hidden="true" />
			{:else}
				<Layers size={13} aria-hidden="true" />
			{/if}
			{opt.label}
			{#if opt.count !== undefined}
				<span
					class={cn(
						"rounded-sm bg-white/[0.04] px-1.5 py-px font-mono text-[10px] tabular",
						on ? "text-accent-text" : "text-fg-faint",
					)}
				>
					{opt.count}
				</span>
			{/if}
		</button>
	{/each}
</div>
