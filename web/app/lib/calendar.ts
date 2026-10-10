import type { StatusKind } from "@components/shared/StatusPill.svelte";
import { albumPosterUrl, bookPosterUrl, posterUrl, tvPosterUrl } from "./posters";
import { releaseTypeLabel } from "./music-books";
import type {
	EpisodeStatus,
	UpcomingAlbum,
	UpcomingBook,
	UpcomingEpisode,
	UpcomingList,
	UpcomingMovie,
} from "./types";
import { getLocale } from "./paraglide/runtime.js";
import { m as i18n } from "./paraglide/messages.js";

export type CalendarKind = "movie" | "episode" | "album" | "book";

// A calendar event is a wanted-movie digital release, an upcoming episode
// air-date, a monitored artist's next release or a monitored book's
// publication. `status` is the item's real state and is only ever shown
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
	// Album covers are square; everything else is a 2:3 poster or cover.
	square?: boolean;
	date: Date;
	// Movies reach this list only while wanted; episodes carry their own.
	status: EpisodeStatus;
};

// Dots are a bare colour with no label, so they may only encode the one thing
// the calendar legend and filter chips already name: movie vs episode. Real
// state goes on a pill, which carries text. The two colours come from
// `--kind-*`, not `--status-*`: borrowing amber and violet made every movie
// look Wanted and every episode look like it was importing.
export function dotToken(e: CalendarEvent): CalendarKind {
	return e.kind;
}

export type GridCell = { date: Date; inMonth: boolean };

export type CalendarFilter = "all" | "movies" | "episodes" | "albums" | "books";

// The kind pill's word, and the filter cell each kind answers to.
export function kindLabel(kind: CalendarKind): string {
	if (kind === "movie") return i18n.common_movie();
	if (kind === "episode") return i18n.common_episode();
	if (kind === "album") return i18n.common_album();
	return i18n.common_book();
}
const FILTER_KIND: Record<Exclude<CalendarFilter, "all">, CalendarKind> = {
	movies: "movie",
	episodes: "episode",
	albums: "album",
	books: "book",
};

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
		detail:
			m.release_type === "theatrical"
				? i18n.calendar_theatrical_release()
				: i18n.calendar_digital_release(),
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

// MusicBrainz and Hardcover date most releases to the day, so these carry no
// time; parseDay keeps a bare day on the local calendar rather than UTC.
function parseDay(iso: string): Date {
	const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
	return m ? new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])) : new Date(iso);
}

export function albumsToCalendarEvents(albums: UpcomingAlbum[]): CalendarEvent[] {
	return albums.map((a) => ({
		id: `album-${a.id}`,
		kind: "album",
		title: a.title,
		subtitle: a.artist_name,
		detail: releaseTypeLabel(a.type),
		poster: albumPosterUrl(a.id),
		square: true,
		href: `/music/${a.artist_id}?release=${a.id}`,
		date: parseDay(a.release_date),
		status: a.status,
	}));
}

export function booksToCalendarEvents(books: UpcomingBook[]): CalendarEvent[] {
	return books.map((b) => ({
		id: `book-${b.id}`,
		kind: "book",
		title: b.title,
		subtitle: b.author,
		detail: b.position
			? b.title === b.series_title
				? i18n.calendar_volume({ n: b.position })
				: i18n.calendar_book_in_series({ series: b.series_title ?? "", n: b.position })
			: b.series_title,
		poster: bookPosterUrl(b.id),
		href: b.series_id ? `/books/series/${b.series_id}` : `/books/${b.id}`,
		date: parseDay(b.release_date),
		status: b.status,
	}));
}

// The pill an album or book row carries. Wanted is what every unreleased row
// is, so it says nothing and gets none; the four states that do say something
// are labelled. Movies and episodes keep the bare dot they had.
const SAID: Partial<Record<EpisodeStatus, StatusKind>> = {
	available: "available",
	downloading: "downloading",
	paused: "paused",
	skipped: "skipped",
};
export function statusPill(e: CalendarEvent): StatusKind | null {
	if (e.kind !== "album" && e.kind !== "book") return null;
	return SAID[e.status] ?? null;
}

export function upcomingEvents(data: UpcomingList | undefined): CalendarEvent[] {
	if (!data) return [];
	return [
		...toCalendarEvents(data.movies ?? []),
		...episodesToCalendarEvents(data.episodes ?? []),
		...albumsToCalendarEvents(data.albums ?? []),
		...booksToCalendarEvents(data.books ?? []),
	].sort((a, b) => a.date.getTime() - b.date.getTime());
}

export function filterEvents(
	events: CalendarEvent[],
	filter: CalendarFilter,
): CalendarEvent[] {
	if (filter === "all") return events;
	const kind = FILTER_KIND[filter];
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
