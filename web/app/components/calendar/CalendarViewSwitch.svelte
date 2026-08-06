<script lang="ts">
	import { cn } from "../../lib/cn";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// Phone only. The tablet stacks the month grid and the agenda in one scroll
	// (there is room); a 390px viewport has to pick one, so it picks per tap.
	export type CalendarView = "month" | "upcoming";

	let {
		view = "month",
		onViewChange,
	}: { view?: CalendarView; onViewChange: (v: CalendarView) => void } = $props();

	const cell =
		"inline-flex shrink-0 items-center justify-center whitespace-nowrap rounded-sm px-2.5 py-1.5 text-[12.5px] font-medium transition";
	const on = "bg-accent-soft text-accent-text";
	const off = "text-fg-subtle hover:text-fg";
</script>

<div
	class="flex shrink-0 items-center gap-0.5 rounded-md border border-border bg-bg-elevated p-[3px]"
	role="group"
	aria-label={i18n.calendar_view()}
>
	<button
		type="button"
		onclick={() => onViewChange("month")}
		aria-pressed={view === "month"}
		class={cn(cell, view === "month" ? on : off)}
	>
		{i18n.calendar_month()}
	</button>
	<button
		type="button"
		onclick={() => onViewChange("upcoming")}
		aria-pressed={view === "upcoming"}
		class={cn(cell, view === "upcoming" ? on : off)}
	>
		{i18n.series_upcoming()}
	</button>
</div>
