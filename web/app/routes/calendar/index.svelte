<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { ChevronLeft, ChevronRight, TriangleAlert } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import type { UpcomingList } from "../../lib/types";
	import {
		dayLabel,
		eventsForDay,
		filterEvents,
		gridRange,
		isSameDay,
		next30Range,
		resolveWeekStart,
		upcomingEvents,
		type CalendarFilter,
	} from "../../lib/calendar";
	import { monthSwipe } from "../../lib/calendar-swipe";
	import type { CalendarView } from "../../components/calendar/CalendarViewSwitch.svelte";
	import MonthGrid from "../../components/calendar/MonthGrid.svelte";
	import DotGrid from "../../components/calendar/DotGrid.svelte";
	import AgendaList from "../../components/calendar/AgendaList.svelte";
	import EventRow from "../../components/calendar/EventRow.svelte";
	import Next30Panel from "../../components/calendar/Next30Panel.svelte";
	import CalendarFilterSwitch from "../../components/calendar/CalendarFilter.svelte";
	import CalendarViewSwitch from "../../components/calendar/CalendarViewSwitch.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import { getLocale } from "../../lib/paraglide/runtime.js";

	const today = new Date();
	let year = $state(today.getFullYear());
	let month0 = $state(today.getMonth());
	let selected = $state(new Date(today.getFullYear(), today.getMonth(), today.getDate()));
	let filter = $state<CalendarFilter>("all");
	let view = $state<CalendarView>("month");

	const weekStart = resolveWeekStart();

	const monthLabel = $derived(
		new Date(year, month0).toLocaleString(getLocale(), {
			month: "long",
			year: "numeric",
		}),
	);
	// The phone header shares its line with the filter switch, and "September
	// 2026" does not fit next to it at 390px.
	const monthLabelShort = $derived(
		new Date(year, month0).toLocaleString(getLocale(), {
			month: "short",
			year: "numeric",
		}),
	);

	const gridQuery = createQuery<UpcomingList>(() => {
		const { from, to } = gridRange(year, month0, weekStart);
		return {
			queryKey: ["calendar", "grid", year, month0],
			queryFn: () =>
				api<UpcomingList>(
					`/calendar/upcoming?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
				),
		};
	});

	const upcomingQuery = createQuery<UpcomingList>(() => ({
		queryKey: ["calendar", "upcoming", 30],
		queryFn: () => {
			const { from, to } = next30Range();
			return api<UpcomingList>(
				`/calendar/upcoming?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
			);
		},
	}));

	let gridEvents = $derived(filterEvents(upcomingEvents(gridQuery.data), filter));
	let upcomingAll = $derived(upcomingEvents(upcomingQuery.data));
	let agendaEvents = $derived(filterEvents(upcomingAll, filter));
	let dayEvents = $derived(eventsForDay(gridEvents, selected));

	// Keep the selected day inside the month on screen: after a swipe, today if
	// this is today's month, otherwise the first.
	function syncSelection() {
		if (selected.getFullYear() === year && selected.getMonth() === month0) return;
		selected =
			today.getFullYear() === year && today.getMonth() === month0
				? new Date(year, month0, today.getDate())
				: new Date(year, month0, 1);
	}

	function shift(delta: number) {
		let m = month0 + delta;
		let y = year;
		if (m < 0) {
			m = 11;
			y--;
		}
		if (m > 11) {
			m = 0;
			y++;
		}
		month0 = m;
		year = y;
		syncSelection();
	}

	function jumpToday() {
		year = today.getFullYear();
		month0 = today.getMonth();
		selected = new Date(today.getFullYear(), today.getMonth(), today.getDate());
	}

	// Tapping a bleed-in cell moves the month with it, so the grid never shows a
	// selection it isn't the month for.
	function select(date: Date) {
		selected = date;
		if (date.getFullYear() !== year || date.getMonth() !== month0) {
			year = date.getFullYear();
			month0 = date.getMonth();
		}
	}

	const navIcon =
		"grid h-9 w-9 place-items-center rounded-md border border-border-strong text-fg-muted transition-colors hover:bg-surface hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring";
	const todayBtn =
		"h-9 rounded-md border border-border-strong px-4 text-sm text-fg transition-colors hover:bg-surface focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring";
</script>

<div
	class="flex flex-col px-4 py-5 md:px-6 md:py-6 lg:h-[calc(100dvh-4rem)] lg:min-h-0 lg:overflow-hidden"
	use:monthSwipe={{
		onPrev: () => shift(-1),
		onNext: () => shift(1),
		disabled: view === "upcoming",
	}}
>
	<!-- Phone: the view switch takes the heading line, the month label drops to
	     the second row beside the filter. Two rows fit; three did not. -->
	<div class="flex items-center gap-2 md:hidden">
		<CalendarViewSwitch {view} onViewChange={(v) => (view = v)} />
		<span class="flex-1"></span>
		{#if view === "month"}
			<button
				type="button"
				onclick={() => shift(-1)}
				aria-label={i18n.calendar_previous_month()}
				class={navIcon}
			>
				<ChevronLeft size={16} aria-hidden="true" />
			</button>
			<button type="button" onclick={jumpToday} class={todayBtn}>{i18n.common_today()}</button>
			<button
				type="button"
				onclick={() => shift(1)}
				aria-label={i18n.calendar_next_month()}
				class={navIcon}
			>
				<ChevronRight size={16} aria-hidden="true" />
			</button>
		{/if}
	</div>
	<div class="mt-3 flex items-center justify-between gap-3 md:hidden">
		<h1 class="truncate text-[17px] font-semibold tracking-[-0.02em] text-fg">
			{view === "month" ? monthLabelShort : i18n.dash_next_30_days()}
		</h1>
		<CalendarFilterSwitch {filter} onChange={(f) => (filter = f)} />
	</div>

	<header class="hidden flex-wrap items-center justify-between gap-4 md:flex">
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-fg">{monthLabel}</h1>
			<p class="mt-1 text-sm text-fg-muted">
				{i18n.calendar_intro()}
			</p>
		</div>
		<div class="flex items-center gap-2">
			<CalendarFilterSwitch {filter} onChange={(f) => (filter = f)} />
			<button
				type="button"
				onclick={() => shift(-1)}
				aria-label={i18n.calendar_previous_month()}
				class={navIcon}
			>
				<ChevronLeft size={16} aria-hidden="true" />
			</button>
			<button type="button" onclick={jumpToday} class={todayBtn}>{i18n.common_today()}</button>
			<button
				type="button"
				onclick={() => shift(1)}
				aria-label={i18n.calendar_next_month()}
				class={navIcon}
			>
				<ChevronRight size={16} aria-hidden="true" />
			</button>
		</div>
	</header>

	{#if gridQuery.isError}
		<div
			role="alert"
			class="mt-4 flex items-center gap-2 rounded-md border border-status-failed/40 bg-status-failed/10 px-3 py-2 text-sm text-status-failed"
		>
			<TriangleAlert size={15} aria-hidden="true" />
			<span>{i18n.calendar_load_failed()}</span>
			<button
				type="button"
				onclick={() => gridQuery.refetch()}
				class="ml-auto rounded px-2 py-0.5 font-medium underline-offset-2 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
			>
				{i18n.common_retry()}
			</button>
		</div>
	{/if}

	<!-- Phone -->
	<div class="mt-4 md:hidden">
		{#if view === "month"}
			<DotGrid {year} {month0} events={gridEvents} {selected} onSelect={select} />
			<div class="mt-5 flex items-baseline justify-between gap-3 px-1">
				<h2 class="text-sm font-semibold tracking-[-0.01em] text-fg">
					{dayLabel(selected)}
					{#if isSameDay(selected, today)}
						<span class="ml-1 font-mono text-[10px] uppercase tracking-[0.12em] text-accent-text">
							{i18n.common_today()}
						</span>
					{/if}
				</h2>
				<span class="shrink-0 font-mono text-[10.5px] text-fg-faint">
					{dayEvents.length === 1
						? i18n.calendar_release_count_one({ count: dayEvents.length })
						: i18n.calendar_release_count_other({ count: dayEvents.length })}
				</span>
			</div>
			{#if dayEvents.length > 0}
				<div class="mt-1.5 flex flex-col">
					{#each dayEvents as event (event.id)}
						<EventRow {event} />
					{/each}
				</div>
			{:else}
				<p class="mt-3 px-1 text-[13px] text-fg-muted">
					{i18n.calendar_nothing_this_day()}
				</p>
			{/if}
		{:else}
			<AgendaList events={agendaEvents} />
		{/if}
	</div>

	<!-- Tablet and desktop: one grid. The tablet folds the agenda in below it;
	     from lg the movies-only panel takes the second column instead — and the
	     whole page is clamped to the viewport there, so the grid and the panel
	     divide the leftover height rather than scrolling the page. -->
	<div
		class="mt-4 hidden gap-4 md:grid lg:min-h-0 lg:flex-1 lg:grid-cols-[1fr_320px]"
	>
		<MonthGrid {year} {month0} events={gridEvents} />
		<div class="hidden lg:grid lg:min-h-0">
			<Next30Panel events={upcomingAll} />
		</div>
		<section class="mt-2 lg:hidden">
			<div class="mb-1 flex items-baseline justify-between gap-3 px-1">
				<h2 class="text-base font-semibold tracking-tight text-fg">{i18n.calendar_coming_up()}</h2>
				<span class="font-mono text-[11px] text-fg-faint">{i18n.lc_next_30_days()}</span>
			</div>
			<AgendaList events={agendaEvents} size="md" />
		</section>
	</div>
</div>
