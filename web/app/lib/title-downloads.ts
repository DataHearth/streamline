// Activity's queue filtered on a title, or on a part of one.
//
// A season grabbed episode by episode, or a pack beside an upgrade, waits on
// several downloads at once, and one link cannot open them all: it opens the
// queue filtered on that part instead (`link.downloads`). The filter lives in
// Activity's query, under keys of its own, and is matched over the queue the
// page already polls.

import type { HistoryEntry, QueueEntry } from "./types";

/** The parts of the library the queue can be filtered on. */
export type DownloadFilter =
	| { kind: "show"; id: number }
	| { kind: "season"; seriesId: number; season: number }
	| { kind: "artist"; id: number }
	| { kind: "bookSeries"; id: number };

const positive = (v: string | null): number => {
	const n = Number(v);
	return Number.isInteger(n) && n > 0 ? n : 0;
};

/**
 * The filter a query names, or null: `series` (with `season`, 0 being the
 * specials), else `artist`, else `book_series`. Anything that is not a
 * positive id names nothing.
 */
export function downloadFilterFrom(p: URLSearchParams): DownloadFilter | null {
	const series = positive(p.get("series"));
	if (series) {
		const raw = p.get("season");
		const season = Number(raw);
		return raw !== null && raw !== "" && Number.isInteger(season) && season >= 0
			? { kind: "season", seriesId: series, season }
			: { kind: "show", id: series };
	}
	const artist = positive(p.get("artist"));
	if (artist) return { kind: "artist", id: artist };
	const bookSeries = positive(p.get("book_series"));
	if (bookSeries) return { kind: "bookSeries", id: bookSeries };
	return null;
}

/**
 * Writes the filter's keys, in the one order both Activity's own URL and a
 * link to it use: a link spelt another way would be rewritten on arrival.
 */
export function setDownloadFilter(p: URLSearchParams, f: DownloadFilter | null) {
	if (!f) return;
	switch (f.kind) {
		case "show":
			p.set("series", String(f.id));
			break;
		case "season":
			p.set("series", String(f.seriesId));
			p.set("season", String(f.season));
			break;
		case "artist":
			p.set("artist", String(f.id));
			break;
		case "bookSeries":
			p.set("book_series", String(f.id));
			break;
	}
}

/**
 * Whether a download is for the filtered title. Matched on the ids the API
 * links the record to, never on the release name: an episode record on its
 * show and its anchor season, an album on its artist, a discography pack on
 * its artist, a volume on its series.
 */
export function downloadMatches(q: QueueEntry | HistoryEntry, f: DownloadFilter): boolean {
	switch (f.kind) {
		case "show":
			return q.episode?.series_id === f.id;
		case "season":
			return q.episode?.series_id === f.seriesId && q.episode.season === f.season;
		case "artist":
			return q.album?.artist_id === f.id || q.artist?.id === f.id;
		case "bookSeries":
			return q.book?.series?.id === f.id;
	}
}

/**
 * The look of a link to a download where it is a word beside a row ("Download"
 * at the end of an event). One class string, so every such link reads the same.
 * It goes on a span inside the link, and the link carries `group/dl` (and
 * `touch-hit`, whose padding then grows the tap target, not the pill).
 */
export const DOWNLOAD_CHIP =
	"inline-flex items-center whitespace-nowrap rounded-full border border-accent-line bg-accent-soft px-2 py-[2px] font-sans text-[10.5px] font-semibold text-accent-text transition group-hover/dl:border-accent group-hover/dl:text-fg";

/**
 * The chip's "›", shown on hover and keyboard focus only: at rest the chip is
 * the word. A touch screen has no hover, so there it always shows. Wraps the
 * icon, inside the chip.
 */
export const DOWNLOAD_CHIP_ARROW =
	"-mr-1 inline-flex w-0 justify-end overflow-hidden opacity-0 transition-[width,opacity] duration-150 group-hover/dl:w-3.5 group-hover/dl:opacity-100 group-focus-visible/dl:w-3.5 group-focus-visible/dl:opacity-100 pointer-coarse:w-3.5 pointer-coarse:opacity-100";
