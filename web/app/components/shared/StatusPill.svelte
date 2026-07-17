<script lang="ts" module>
	export type StatusKind =
		| "downloading"
		| "grabbing"
		| "available"
		| "wanted"
		| "missing"
		| "failed"
		| "paused"
		| "seeding"
		| "completed"
		| "fetching"
		| "stalled";

	const LABELS: Record<StatusKind, string> = {
		downloading: "Downloading",
		grabbing: "Grabbing",
		available: "Available",
		wanted: "Wanted",
		missing: "Missing",
		failed: "Failed",
		paused: "Paused",
		seeding: "Seeding",
		completed: "Completed",
		fetching: "Fetching metadata",
		stalled: "Stalled",
	};
</script>

<script lang="ts">
	import { cn } from "../../lib/cn";

	let {
		status,
		size = "md",
		live = false,
		variant = "solid",
	}: {
		status: StatusKind;
		size?: "sm" | "md";
		live?: boolean;
		variant?: "solid" | "translucent";
	} = $props();

	let label = $derived(LABELS[status]);
</script>

<span
	data-variant={variant}
	class={cn(
		"pill inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border font-semibold tracking-[0.02em]",
		size === "sm" ? "px-1.5 py-[1px] text-[10px] gap-1" : "px-2 py-0.5 text-[11px]",
	)}
	style:--c="var(--status-{status})"
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
