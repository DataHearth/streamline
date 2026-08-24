import type { ActivityEvent } from "./types";

export type EventSubject = {
	/** Row heading — the title the event happened to. */
	title: string;
	/** Detail page, or undefined when the row has nowhere to go. */
	href?: string;
	/** Qualifier under the heading (SxxExx, "Season 3"), when there is one. */
	detail?: string;
};

const pad = (n: number) => String(n).padStart(2, "0");

/**
 * Resolves which of an event's three possible owners is set and renders it as
 * one row. A series-scoped search carries the seasons it touched in its
 * payload; when it touched exactly one, saying so is more useful than the bare
 * show title, and when it touched several the count already reads as "all".
 */
export function eventSubject(event: ActivityEvent): EventSubject {
	if (event.movie) {
		return { title: event.movie.title, href: `/movies/${event.movie.id}` };
	}
	if (event.episode) {
		const e = event.episode;
		return {
			title: e.show_title,
			href: e.series_id ? `/series/${e.series_id}` : undefined,
			detail: `S${pad(e.season)}E${pad(e.episode)}`,
		};
	}
	if (event.series) {
		return {
			title: event.series.title,
			href: `/series/${event.series.id}`,
			detail: seasonLabel(event.payload),
		};
	}
	return { title: "Unknown" };
}

function seasonLabel(
	payload: Record<string, unknown> | undefined,
): string | undefined {
	const seasons = payload?.seasons;
	if (!Array.isArray(seasons) || seasons.length !== 1) return undefined;
	const n = seasons[0];
	return typeof n === "number" ? `Season ${n}` : undefined;
}
