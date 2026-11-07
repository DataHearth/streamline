<script lang="ts">
	import type { Delta } from "@lib/stats";
	import { cn } from "@lib/cn";

	// One headline number. The label is text, so it is fg-subtle like every
	// eyebrow (faint is for icons and separators only). `tone` colours the value with a status token
	// when the number is itself a state (failed, pending); most stay fg.
	let {
		label,
		value,
		sub = "",
		delta = null,
		size = "md",
		tone = "",
	}: {
		label: string;
		value: string;
		sub?: string;
		delta?: Delta | null;
		size?: "lg" | "md" | "sm";
		tone?: string;
	} = $props();

	const DELTA_CLS = {
		good: "text-status-available",
		bad: "text-status-failed",
		flat: "text-fg-subtle",
	};
</script>

<div class="min-w-0">
	<div
		class={cn(
			"truncate font-mono uppercase text-fg-subtle",
			size === "sm" ? "text-[10px] tracking-[0.12em]" : "text-[10.5px] tracking-[0.14em]",
		)}
	>
		{label}
	</div>
	<div class={cn("flex min-w-0 items-baseline gap-2", size === "sm" ? "mt-1" : "mt-1.5")}>
		<span
			class={cn(
				"truncate font-semibold tabular-nums tracking-[-0.01em]",
				size === "lg" && "text-[30px] leading-[1.15]",
				size === "md" && "text-[22px] leading-[1.2]",
				size === "sm" && "text-[16px] leading-[1.25]",
				!tone && "text-fg",
			)}
			style:color={tone ? `var(--status-${tone})` : undefined}
		>
			{value}
		</span>
		{#if delta}
			<span class={cn("shrink-0 font-mono text-[11.5px] tabular-nums", DELTA_CLS[delta.tone])}>{delta.text}</span>
		{/if}
	</div>
	{#if sub}
		<div class="mt-1 text-[12px] leading-[1.4] text-fg-subtle">{sub}</div>
	{/if}
</div>
