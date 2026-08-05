<script lang="ts">
	import { cn } from "../../lib/cn";

	// Queue and History are two readings of one page, so this switches between them
	// in place. Torrents is NOT a third cell: it's a separate route, and the nav
	// (sidebar from md, the Activity sheet on phone) is where you go between pages.
	type View = "queue" | "history";

	let {
		view = "queue",
		counts,
		onViewChange,
	}: {
		view?: View;
		counts: { queue?: number; history?: number };
		onViewChange: (v: View) => void;
	} = $props();

	const cell =
		"inline-flex shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-sm px-2.5 py-1.5 text-[12.5px] font-medium transition md:px-3";
	const on = "bg-accent-soft text-accent-text";
	const off = "text-fg-subtle hover:text-fg";
	const num = "font-mono text-[10.5px] tabular-nums";
</script>

<div
	class="flex shrink-0 items-center gap-0.5 rounded-md border border-border bg-bg-elevated p-[3px]"
	role="group"
	aria-label="Activity view"
>
	<button
		type="button"
		onclick={() => onViewChange("queue")}
		aria-pressed={view === "queue"}
		class={cn(cell, view === "queue" ? on : off)}
	>
		Queue
		{#if counts.queue !== undefined}
			<span class={cn(num, view === "queue" ? "opacity-80" : "text-fg-faint")}>
				{counts.queue}
			</span>
		{/if}
	</button>
	<button
		type="button"
		onclick={() => onViewChange("history")}
		aria-pressed={view === "history"}
		class={cn(cell, view === "history" ? on : off)}
	>
		History
		{#if counts.history !== undefined}
			<span class={cn(num, view === "history" ? "opacity-80" : "text-fg-faint")}>
				{counts.history}
			</span>
		{/if}
	</button>
</div>
