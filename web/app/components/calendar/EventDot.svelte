<script lang="ts">
	import { cn } from "../../lib/cn";
	import type { CalendarKind } from "../../lib/calendar";

	// The dot encodes kind, never state — see dotToken. Its colours live in
	// `--kind-*` so they can't be mistaken for a status.
	let {
		kind,
		size = "sm",
	}: { kind: CalendarKind; size?: "sm" | "md" } = $props();
</script>

<span
	class={cn(
		"dot inline-block shrink-0 rounded-full",
		size === "md" ? "h-2 w-2" : "h-1.5 w-1.5",
	)}
	style:--c="var(--kind-{kind})"
	aria-hidden="true"
></span>

<style>
	.dot {
		background-color: var(--c);
	}
</style>
