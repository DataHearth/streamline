const dtf = new Intl.DateTimeFormat(undefined, {
	dateStyle: "medium",
	timeStyle: "short",
});
const dateShort = new Intl.DateTimeFormat(undefined, {
	month: "short",
	day: "numeric",
});
const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

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
