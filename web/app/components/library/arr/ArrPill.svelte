<script lang="ts">
	import { cn } from "@lib/cn";

	// StatusPill's labels are fixed per kind ("Available", "Failed"); the wizard
	// needs the same three hues carrying its own words ("Found", "Not found",
	// "Added"), so it draws the translucent variant itself.
	type Props = {
		tone: "ok" | "bad" | "neutral";
		label: string;
		title?: string;
	};

	let { tone, label, title }: Props = $props();
</script>

<span
	{title}
	class={cn(
		"inline-flex max-w-full shrink-0 items-center gap-1 whitespace-nowrap rounded-full border px-2 py-0.5 text-[11px] font-semibold tracking-[0.02em]",
		tone === "ok" &&
			"border-status-available/25 bg-status-available/15 text-status-available",
		tone === "bad" &&
			"border-status-failed/25 bg-status-failed/15 text-status-failed",
		tone === "neutral" && "border-border bg-bg-hover text-fg-muted",
	)}
>
	<span
		aria-hidden="true"
		class={cn(
			"h-1.5 w-1.5 shrink-0 rounded-full",
			tone === "ok" && "bg-status-available",
			tone === "bad" && "bg-status-failed",
			tone === "neutral" && "bg-fg-faint",
		)}
	></span>
	<span class="truncate">{label}</span>
</span>
