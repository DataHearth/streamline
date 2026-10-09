<script lang="ts">
	import { cn } from "@lib/cn";

	// StatusPill's drawing with a caller-given label — "11/12", "Downloading ·
	// 64%", "Upcoming" — for the counts and states StatusKind's fixed labels do
	// not cover. `token` is a --status-* name.
	let {
		token,
		label,
		size = "sm",
		variant = "solid",
		live = false,
	}: {
		token: string;
		label: string;
		size?: "sm" | "md";
		variant?: "solid" | "translucent";
		live?: boolean;
	} = $props();
</script>

<span
	data-variant={variant}
	class={cn(
		"pill inline-flex items-center whitespace-nowrap rounded-full border font-semibold tracking-[0.02em]",
		size === "sm" ? "gap-1 px-1.5 py-[1px] text-[10px]" : "gap-1.5 px-2 py-0.5 text-[11px]",
	)}
	style:--c="var(--status-{token})"
>
	<span
		class={cn(
			"dot shrink-0 rounded-full",
			size === "sm" ? "h-[5px] w-[5px]" : "h-1.5 w-1.5",
			live && "motion-safe:animate-pulse",
		)}
	></span>
	<span>{label}</span>
</span>

<style>
	.pill[data-variant="solid"] {
		background-color: var(--c);
		border-color: var(--c);
		color: var(--bg-deep);
	}
	.pill[data-variant="solid"] .dot {
		background-color: var(--bg-deep);
	}
	.pill[data-variant="translucent"] {
		background-color: color-mix(in srgb, var(--c) 15%, transparent);
		border-color: color-mix(in srgb, var(--c) 25%, transparent);
		color: var(--c);
	}
	.pill[data-variant="translucent"] .dot {
		background-color: var(--c);
	}
</style>
