import { posterUrl, tvPosterUrl } from "./posters";
import type {
	EpisodeStatus,
	UpcomingEpisode,
	UpcomingList,
	UpcomingMovie,
} from "./types";
import { getLocale } from "./paraglide/runtime.js";
import { m as i18n } from "./paraglide/messages.js";

export type CalendarKind = "movie" | "episode";

// A calendar event is either a wanted-movie digital release or an upcoming
// episode air-date. `status` is the item's real state and is only ever shown
// with its label next to it (a pill), never as colour alone. Dots encode
// `kind` instead — see dotToken. The rest of the fields let one row renderer
// serve both kinds. `subtitle` / `time` / `detail` are the three segments of
// the meta line, in that order — a movie fills only the last one.
export type CalendarEvent = {
	id: string;
	kind: CalendarKind;
	title: string;
	subtitle?: string;
	time?: string;
	detail?: string;
	poster: string;
	href: string;
	date: Date;
	// Movies reach this list only while wanted; episodes carry their own.
	status: EpisodeStatus;
};

// Dots are a bare colour with no label, so they may only encode the one thing
// the calendar legend and filter chips already name: movie (amber) vs episode
// (purple). Real state goes on a pill, which carries text.
export function dotToken(e: CalendarEvent): "wanted" | "grabbing" {
	return e.kind === "movie" ? "wanted" : "grabbing";
}

export type GridCell = { date: Date; inMonth: boolean };

export type CalendarFilter = "all" | "movies" | "episodes";

const clock = new Intl.DateTimeFormat(getLocale(), {
	hour: "2-digit",
	minute: "2-digit",
});

// A date-only air date formats as 00:00, which reads as "airs at midnight"
// rather than "we don't know". Only emit a time when the payload carries one.
function airTime(iso: string): string | undefined {
	if (!/T\d{2}:\d{2}/.test(iso)) return undefined;
	const d = new Date(iso);
	return Number.isNaN(d.getTime()) ? undefined : clock.format(d);
}

export function toCalendarEvents(movies: UpcomingMovie[]): CalendarEvent[] {
	return movies.map((m) => ({
		id: `movie-${m.id}`,
		kind: "movie",
		title: m.title,
		detail: i18n.calendar_digital_release(),
		poster: posterUrl({ id: m.id }),
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
		time: airTime(e.air_date),
		detail: e.title,
		poster: tvPosterUrl(e.series_id),
		href: `/series/${e.series_id}`,
		date: new Date(e.air_date),
		status: e.status,
	}));
}

export function upcomingEvents(data: UpcomingList | undefined): CalendarEvent[] {
	if (!data) return [];
	return [
		...toCalendarEvents(data.movies ?? []),
		...episodesToCalendarEvents(data.episodes ?? []),
	].sort((a, b) => a.date.getTime() - b.date.getTime());
}

export function filterEvents(
	events: CalendarEvent[],
	filter: CalendarFilter,
): CalendarEvent[] {
	if (filter === "all") return events;
	const kind: CalendarKind = filter === "movies" ? "movie" : "episode";
	return events.filter((e) => e.kind === kind);
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

// Untimed first — a digital release has no clock to sort by — then by air
// time. Ties fall back to the title so two 22:00 episodes keep a stable order
// across refetches.
function byTime(a: CalendarEvent, b: CalendarEvent): number {
	if (!a.time !== !b.time) return a.time ? 1 : -1;
	return (
		(a.time ?? "").localeCompare(b.time ?? "") || a.title.localeCompare(b.title)
	);
}

export function eventsForDay(
	events: CalendarEvent[],
	date: Date,
): CalendarEvent[] {
	const key = dayKey(date);
	return events.filter((e) => dayKey(e.date) === key).sort(byTime);
}

export type DayGroup = { key: number; date: Date; events: CalendarEvent[] };

export function groupByDay(events: CalendarEvent[]): DayGroup[] {
	const map = new Map<number, DayGroup>();
	for (const e of events) {
		const key = dayKey(e.date);
		const group = map.get(key);
		if (group) group.events.push(e);
		else map.set(key, { key, date: e.date, events: [e] });
	}
	return [...map.values()]
		.sort((a, b) => a.key - b.key)
		.map((g) => ({ ...g, events: g.events.sort(byTime) }));
}

export function isSameDay(a: Date, b: Date): boolean {
	return dayKey(a) === dayKey(b);
}

export function isToday(d: Date): boolean {
	return isSameDay(d, new Date());
}

const dayFmt = new Intl.DateTimeFormat(getLocale(), {
	weekday: "short",
	day: "numeric",
	month: "short",
});

export function dayLabel(d: Date): string {
	return dayFmt.format(d);
}

// Weekday labels ordered for the chosen week start. 2023-01-01 is a Sunday, so
// day-of-month i maps cleanly to weekday i. `narrow` is the phone grid, where a
// column is 48px wide.
export function weekdayLabels(
	weekStartsOn: 0 | 1,
	width: "short" | "narrow" = "short",
): string[] {
	const fmt = new Intl.DateTimeFormat(getLocale(), { weekday: width });
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

// The rolling window behind the agenda. `from` is now rather than midnight, so
// the list is forward-only by construction: an episode that aired this morning
// is already history, and history is the Activity page's job.
export function next30Range(): { from: string; to: string } {
	const now = new Date();
	return {
		from: now.toISOString(),
		to: new Date(now.getTime() + 30 * 86_400_000).toISOString(),
	};
}
