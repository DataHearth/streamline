import { m as i18n } from "./paraglide/messages.js";
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
const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });

export function formatDateTime(iso: string | null | undefined): string {
	if (!iso) return "";
	return dtf.format(new Date(iso));
}

export function formatDateShort(iso: string | null | undefined): string {
	if (!iso) return "";
	return dateShort.format(new Date(iso));
}

const MIN = 60_000;
const HR = 3_600_000;
const DAY = 86_400_000;
const MONTH = 30 * DAY;
const YEAR = 365 * DAY;

type Plural = (inputs: { n: number }) => string;

function unit(n: number, one: Plural, other: Plural): string {
	return n === 0 ? "" : n === 1 ? one({ n }) : other({ n });
}

// The two largest non-zero units, so "1 year 2 days" keeps the days a
// year/month pairing would swallow. Years and months are nominal (365 / 30
// days) — close enough for a glanceable caption, and it never has to agree
// with the absolute date shown next to it.
function span(abs: number): string {
	const sub = abs % YEAR;
	return [
		unit(Math.floor(abs / YEAR), i18n.rel_years_one, i18n.rel_years_other),
		unit(Math.floor(sub / MONTH), i18n.rel_months_one, i18n.rel_months_other),
		unit(Math.floor((sub % MONTH) / DAY), i18n.rel_days_one, i18n.rel_days_other),
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
	// two-part span instead of leaning on Intl.RelativeTimeFormat. Assembled
	// from messages, not string literals — Intl covers the shorter ranges but
	// not this one.
	return diffMs < 0
		? i18n.rel_ago({ span: span(abs) })
		: i18n.rel_in({ span: span(abs) });
}
