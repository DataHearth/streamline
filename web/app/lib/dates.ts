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

export function formatRelative(iso: string | null | undefined): string {
	if (!iso) return "";
	const diffMs = new Date(iso).getTime() - Date.now();
	const abs = Math.abs(diffMs);
	const min = 60_000;
	const hr = 3_600_000;
	const day = 86_400_000;
	if (abs < hr) return rtf.format(Math.round(diffMs / min), "minute");
	if (abs < day) return rtf.format(Math.round(diffMs / hr), "hour");
	return rtf.format(Math.round(diffMs / day), "day");
}
