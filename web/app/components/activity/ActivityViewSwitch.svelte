<script lang="ts" module>
	export type ActivitySwitchView = "queue" | "history" | "events";
</script>

<script lang="ts">
	import { cn } from "../../lib/cn";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// Queue, History and Events are three readings of one page, so this switches
	// between them in place. Torrents is NOT a fourth cell: it's a separate route,
	// and the nav (sidebar from md, the Activity sheet on phone) is where you go
	// between pages.
	type View = ActivitySwitchView;

	let {
		view = "queue",
		counts,
		onViewChange,
	}: {
		view?: View;
		counts: { queue?: number; history?: number; events?: number };
		onViewChange: (v: View) => void;
	} = $props();

	let cells = $derived<{ key: View; label: string; count?: number }[]>([
		{ key: "queue", label: i18n.activity_queue(), count: counts.queue },
		{ key: "history", label: i18n.activity_history(), count: counts.history },
		{ key: "events", label: i18n.activity_events(), count: counts.events },
	]);

	const cell =
		"inline-flex shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-sm px-2.5 py-1.5 text-[12.5px] font-medium transition md:px-3";
	const on = "bg-accent-soft text-accent-text";
	const off = "text-fg-subtle hover:text-fg";
	const num = "font-mono text-[10.5px] tabular-nums";
</script>

<div
	class="flex shrink-0 items-center gap-0.5 rounded-md border border-border bg-bg-elevated p-[3px]"
	role="group"
	aria-label={i18n.activity_view()}
>
	{#each cells as c (c.key)}
		<button
			type="button"
			onclick={() => onViewChange(c.key)}
			aria-pressed={view === c.key}
			class={cn(cell, view === c.key ? on : off)}
		>
			{c.label}
			{#if c.count !== undefined}
				<span class={cn(num, view === c.key ? "opacity-80" : "text-fg-faint")}>
					{c.count}
				</span>
			{/if}
		</button>
	{/each}
</div>
