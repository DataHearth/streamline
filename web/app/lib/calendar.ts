import type { UpcomingEpisode, UpcomingMovie } from "./types";

export type CalendarKind = "movie" | "episode";

// A calendar event is either a wanted-movie digital release or an upcoming
// episode air-date. `status` drives the dot colour (movies = wanted/amber,
// episodes = grabbing/purple, matching the filter chips); `href` + `subtitle`
// let movies and episodes share one renderer.
export type CalendarEvent = {
	id: string;
	kind: CalendarKind;
	title: string;
	subtitle?: string;
	href: string;
	date: Date;
	status: "wanted" | "grabbing";
};

export type GridCell = { date: Date; inMonth: boolean };

export function toCalendarEvents(movies: UpcomingMovie[]): CalendarEvent[] {
	return movies.map((m) => ({
		id: `movie-${m.id}`,
		kind: "movie",
		title: m.title,
		href: `/movies/${m.id}`,
		date: new Date(m.digital_release_date),
		status: "wanted",
	}));
}

function pad2(n: number): string {
	return String(n).padStart(2, "0");
}

export function episodesToCalendarEvents(
	episodes: UpcomingEpisode[],
): CalendarEvent[] {
	return episodes.map((e) => ({
		id: `episode-${e.series_id}-${e.season}-${e.episode}`,
		kind: "episode",
		title: e.series_title,
		subtitle: `S${pad2(e.season)}E${pad2(e.episode)}`,
		href: `/series/${e.series_id}`,
		date: new Date(e.air_date),
		status: "grabbing",
	}));
}

// Monday-first, app-wide. The Claude design artifact pins Mon-first as the
// canonical convention regardless of viewer locale.
export function resolveWeekStart(): 0 | 1 {
	return 1;
}

function gridStartDate(year: number, month0: number, weekStartsOn: 0 | 1): Date {
	const first = new Date(year, month0, 1);
	const offset = (first.getDay() - weekStartsOn + 7) % 7;
	return new Date(year, month0, 1 - offset);
}

export function buildMonthGrid(
	year: number,
	month0: number,
	weekStartsOn: 0 | 1,
): GridCell[][] {
	const start = gridStartDate(year, month0, weekStartsOn);
	const weeks: GridCell[][] = [];
	for (let w = 0; w < 6; w++) {
		const row: GridCell[] = [];
		for (let d = 0; d < 7; d++) {
			const date = new Date(
				start.getFullYear(),
				start.getMonth(),
				start.getDate() + w * 7 + d,
			);
			row.push({ date, inMonth: date.getMonth() === month0 });
		}
		weeks.push(row);
	}
	return weeks;
}

function dayKey(d: Date): number {
	return d.getFullYear() * 10000 + d.getMonth() * 100 + d.getDate();
}

export function eventsForDay(
	events: CalendarEvent[],
	date: Date,
): CalendarEvent[] {
	const key = dayKey(date);
	return events.filter((e) => dayKey(e.date) === key);
}

export function isSameDay(a: Date, b: Date): boolean {
	return dayKey(a) === dayKey(b);
}

// Short weekday labels ordered for the chosen week start. 2023-01-01 is a
// Sunday, so day-of-month i maps cleanly to weekday i.
export function weekdayLabels(weekStartsOn: 0 | 1): string[] {
	const fmt = new Intl.DateTimeFormat(undefined, { weekday: "short" });
	const labels: string[] = [];
	for (let i = 0; i < 7; i++) {
		labels.push(fmt.format(new Date(2023, 0, 1 + ((weekStartsOn + i) % 7))));
	}
	return labels;
}

// Half-open [from, to) RFC3339 window covering the full 6×7 grid (42 cells),
// so events bleeding in from adjacent months still render.
export function gridRange(
	year: number,
	month0: number,
	weekStartsOn: 0 | 1,
): { from: string; to: string } {
	const start = gridStartDate(year, month0, weekStartsOn);
	const from = new Date(start.getFullYear(), start.getMonth(), start.getDate());
	const to = new Date(
		start.getFullYear(),
		start.getMonth(),
		start.getDate() + 42,
	);
	return { from: from.toISOString(), to: to.toISOString() };
}
