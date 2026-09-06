import { getLocale } from "./paraglide/runtime.js";

// Bound to the app locale, not the browser's: a French UI in an English
// browser must not render English dates. Locale changes force a reload, so
// resolving once at module load is enough.
const locale = getLocale();

const dtf = new Intl.DateTimeFormat(locale, {
	dateStyle: "medium",
	timeStyle: "short",
});
const dateShort = new Intl.DateTimeFormat(locale, {
	month: "short",
	day: "numeric",
});
// Day, month, year — in that order in every locale. `dateStyle` would hand
// en-US "May 17, 2025"; the parts are reassembled so one reading order holds
// across the app rather than changing under the language.
const dateFull = new Intl.DateTimeFormat(locale, {
	day: "numeric",
	month: "short",
	year: "numeric",
});
const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });

export function formatDateTime(iso: string | null | undefined): string {
	if (!iso) return "";
	return dtf.format(new Date(iso));
}

export function formatDateShort(iso: string | null | undefined): string {
	if (!iso) return "";
	return dateShort.format(new Date(iso));
}

// A date-only string is a wall-clock fact, not an instant: `Date.parse` reads
// "2022-08-08" as UTC midnight, which then formats as the day before for every
// viewer west of UTC. Ten characters means a day, so it is parsed as local
// midnight; anything longer carries its own time and zone.
function parseDay(iso: string): number {
	return Date.parse(iso.length === 10 ? `${iso}T00:00:00` : iso);
}

// A day, with its year, and no time of day — for dates that are facts about the
// title rather than about our copy of it (a release date, not an import).
export function formatDate(iso: string | null | undefined): string {
	if (!iso) return "";
	const t = parseDay(iso);
	if (Number.isNaN(t)) return "";
	const parts = dateFull.formatToParts(new Date(t));
	const part = (type: string) => parts.find((p) => p.type === type)?.value ?? "";
	// Reassembled rather than formatted: this drops the locale's own separators
	// (en-US's comma) along with its ordering.
	return `${part("day")} ${part("month")} ${part("year")}`.trim();
}

const MIN = 60_000;
const HR = 3_600_000;
const DAY = 86_400_000;
const MONTH = 30 * DAY;
const YEAR = 365 * DAY;

function unit(n: number, name: string): string {
	return n === 0 ? "" : `${n} ${name}${n === 1 ? "" : "s"}`;
}

// The two largest non-zero units, so "1 year 2 days" keeps the days a
// year/month pairing would swallow. Years and months are nominal (365 / 30
// days) — close enough for a glanceable caption, and it never has to agree
// with the absolute date shown next to it.
function span(abs: number): string {
	const sub = abs % YEAR;
	return [
		unit(Math.floor(abs / YEAR), "year"),
		unit(Math.floor(sub / MONTH), "month"),
		unit(Math.floor((sub % MONTH) / DAY), "day"),
	]
		.filter(Boolean)
		.slice(0, 2)
		.join(" ");
}

export function formatRelative(iso: string | null | undefined): string {
	if (!iso) return "";
	const diffMs = new Date(iso).getTime() - Date.now();
	const abs = Math.abs(diffMs);
	if (abs < HR) return rtf.format(Math.round(diffMs / MIN), "minute");
	if (abs < DAY) return rtf.format(Math.round(diffMs / HR), "hour");
	if (abs < MONTH) return rtf.format(Math.round(diffMs / DAY), "day");
	// Past a month, a single unit reads as noise ("5,066 days ago"), so build a
	// two-part span instead of leaning on Intl.RelativeTimeFormat.
	return diffMs < 0 ? `${span(abs)} ago` : `in ${span(abs)}`;
}
