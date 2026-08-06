<script lang="ts">
	import { cn } from "../../lib/cn";
	import {
		buildMonthGrid,
		dayLabel,
		eventsForDay,
		isSameDay,
		resolveWeekStart,
		weekdayLabels,
		type CalendarEvent,
	} from "../../lib/calendar";
	import EventDot from "./EventDot.svelte";

	// The phone's month view. Below md a cell is ~48px wide, which is enough for
	// a date and a few dots and nothing else — the titles live in the day panel
	// underneath, so nothing here has to truncate.
	let {
		year,
		month0,
		events,
		selected,
		onSelect,
	}: {
		year: number;
		month0: number;
		events: CalendarEvent[];
		selected: Date;
		onSelect: (date: Date) => void;
	} = $props();

	const weekStart = resolveWeekStart();
	const labels = weekdayLabels(weekStart, "narrow");
	const today = new Date();
	const MAX_DOTS = 3;

	let grid = $derived(buildMonthGrid(year, month0, weekStart));
</script>

<div class="rounded-lg border border-border bg-bg-elevated p-2">
	<div class="grid grid-cols-7" aria-hidden="true">
		{#each labels as label, i (i)}
			<span
				class="pb-1.5 text-center font-mono text-[9.5px] uppercase tracking-[0.14em] text-fg-faint"
			>
				{label}
			</span>
		{/each}
	</div>

	<div class="grid grid-cols-7 gap-0.5">
		{#each grid as week, w (w)}
			{#each week as cell (cell.date.toISOString())}
				{@const evs = eventsForDay(events, cell.date)}
				{@const isCurrent = isSameDay(cell.date, today)}
				{@const isSelected = isSameDay(cell.date, selected)}
				<button
					type="button"
					onclick={() => onSelect(cell.date)}
					aria-pressed={isSelected}
					aria-label="{dayLabel(cell.date)}, {evs.length} release{evs.length ===
					1
						? ''
						: 's'}"
					class={cn(
						"relative flex min-h-[46px] flex-col items-center gap-1.5 rounded-md py-[7px] transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
						isSelected ? "bg-accent-soft" : "hover:bg-surface",
					)}
				>
					<span
						class={cn(
							"font-mono text-[13px] font-semibold leading-none tabular",
							!cell.inMonth
								? "text-fg-faint opacity-60"
								: isSelected || isCurrent
									? "text-accent-text"
									: "text-fg-muted",
						)}
					>
						{cell.date.getDate()}
					</span>
					<span class="flex h-[5px] items-center gap-[3px]">
						{#each evs.slice(0, MAX_DOTS) as e (e.id)}
							<EventDot status={e.status} />
						{/each}
						{#if evs.length > MAX_DOTS}
							<span class="font-mono text-[8px] leading-none text-fg-faint">
								+{evs.length - MAX_DOTS}
							</span>
						{/if}
					</span>
					{#if isCurrent || isSelected}
						<span
							class={cn(
								"pointer-events-none absolute inset-0 rounded-md border",
								isSelected ? "border-accent" : "border-accent-line",
							)}
							aria-hidden="true"
						></span>
					{/if}
				</button>
			{/each}
		{/each}
	</div>
</div>
