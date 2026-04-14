<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { ChevronLeft, ChevronRight, TriangleAlert } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { cn } from "../../lib/cn";
	import type { UpcomingList } from "../../lib/types";
	import {
		episodesToCalendarEvents,
		gridRange,
		resolveWeekStart,
		toCalendarEvents,
		type CalendarEvent,
	} from "../../lib/calendar";
	import MonthGrid from "../../components/calendar/MonthGrid.svelte";
	import Next30Panel from "../../components/calendar/Next30Panel.svelte";
	import EventLegend from "../../components/calendar/EventLegend.svelte";

	const today = new Date();
	let year = $state(today.getFullYear());
	let month0 = $state(today.getMonth());

	const weekStart = resolveWeekStart();

	const monthLabel = $derived(
		new Date(year, month0).toLocaleString(undefined, {
			month: "long",
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

	function next30Range() {
		const now = new Date();
		const to = new Date(now.getTime() + 30 * 86_400_000);
		return { from: now.toISOString(), to: to.toISOString() };
	}
	const upcomingQuery = createQuery<UpcomingList>(() => ({
		queryKey: ["calendar", "upcoming", 30],
		queryFn: () => {
			const { from, to } = next30Range();
			return api<UpcomingList>(
				`/calendar/upcoming?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
			);
		},
	}));

	let showMovies = $state(true);
	let showEpisodes = $state(true);

	let events = $derived.by(() => {
		const out: CalendarEvent[] = [];
		if (showMovies) out.push(...toCalendarEvents(gridQuery.data?.movies ?? []));
		if (showEpisodes)
			out.push(...episodesToCalendarEvents(gridQuery.data?.episodes ?? []));
		return out;
	});
	let upcoming = $derived(upcomingQuery.data?.movies ?? []);

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
	}
	function jumpToday() {
		year = today.getFullYear();
		month0 = today.getMonth();
	}

	const navIcon =
		"grid h-9 w-9 place-items-center rounded-md border border-border-strong text-fg-muted transition-colors hover:bg-surface hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring";
</script>

<div class="flex flex-col px-4 py-6 md:px-6">
	<header class="mb-4 flex flex-wrap items-center justify-between gap-4">
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-fg">
				{monthLabel}
			</h1>
			<p class="mt-1 text-sm text-fg-muted">
				Releases on the radar — cinema, digital, and streaming windows.
			</p>
		</div>
		<div class="flex flex-wrap items-center gap-2">
			<div
				class="flex items-center gap-1 rounded-full border border-border bg-surface px-1 py-1 text-[12px] text-fg-muted"
				role="group"
				aria-label="Filter events"
			>
				<button
					type="button"
					aria-pressed={showMovies}
					onclick={() => (showMovies = !showMovies)}
					class={cn(
						"rounded-full px-3 py-1 transition-colors",
						showMovies
							? "bg-bg-elevated text-fg shadow-1"
							: "text-fg-faint hover:text-fg",
					)}
				>
					<span
						class="mr-1.5 inline-block h-1.5 w-1.5 rounded-full bg-status-wanted"
						aria-hidden="true"
					></span>
					Movies
				</button>
				<button
					type="button"
					aria-pressed={showEpisodes}
					onclick={() => (showEpisodes = !showEpisodes)}
					class={cn(
						"rounded-full px-3 py-1 transition-colors",
						showEpisodes
							? "bg-bg-elevated text-fg shadow-1"
							: "text-fg-faint hover:text-fg",
					)}
				>
					<span
						class="mr-1.5 inline-block h-1.5 w-1.5 rounded-full bg-status-grabbing"
						aria-hidden="true"
					></span>
					Episodes
				</button>
			</div>
			<button
				type="button"
				onclick={() => shift(-1)}
				aria-label="Previous month"
				class={navIcon}
			>
				<ChevronLeft size={16} aria-hidden="true" />
			</button>
			<button
				type="button"
				onclick={jumpToday}
				class="h-9 rounded-md border border-border-strong px-4 text-sm text-fg transition-colors hover:bg-surface focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
			>
				Today
			</button>
			<button
				type="button"
				onclick={() => shift(1)}
				aria-label="Next month"
				class={navIcon}
			>
				<ChevronRight size={16} aria-hidden="true" />
			</button>
		</div>
	</header>

	<EventLegend />

	{#if gridQuery.isError}
		<div
			role="alert"
			class="mt-4 flex items-center gap-2 rounded-md border border-status-failed/40 bg-status-failed/10 px-3 py-2 text-sm text-status-failed"
		>
			<TriangleAlert size={15} aria-hidden="true" />
			<span>Couldn't load releases for this month.</span>
			<button
				type="button"
				onclick={() => gridQuery.refetch()}
				class="ml-auto rounded px-2 py-0.5 font-medium underline-offset-2 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
			>
				Retry
			</button>
		</div>
	{/if}

	<div class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-[1fr_320px]">
		<MonthGrid {year} {month0} {events} />
		<Next30Panel events={upcoming} />
	</div>
</div>
