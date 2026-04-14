<script lang="ts">
	import { cn } from "../../lib/cn";

	type Status =
		| "wanted"
		| "available"
		| "grabbing"
		| "missing"
		| "downloading"
		| "failed";

	let {
		status,
		size = "sm",
	}: { status: Status; size?: "sm" | "md" } = $props();
</script>

<span
	class={cn(
		"dot inline-block shrink-0 rounded-full",
		size === "md" ? "h-2 w-2" : "h-1.5 w-1.5",
	)}
	style:--c="var(--status-{status})"
	aria-hidden="true"
></span>

<style>
	.dot {
		background-color: var(--c);
	}
</style>
