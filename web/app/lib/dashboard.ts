// The dashboard's two summaries across the four media: what arrived (the
// shelf) and what each library holds (the ledger). Both speak the same four
// nouns, so the units, the routes and the kind colours are stated here once.

import { additionLabel } from "./additions";
import { formatBytes } from "./format";
import { albumPosterUrl, bookPosterUrl, posterUrl, tvPosterUrl } from "./posters";
import {
	artistTally,
	kindLabel,
	releaseTypeLabel,
	volumeLabel,
	type Artist,
	type ShelfItem,
} from "./music-books";
import type { DiskUsage, LibraryArrival, Movie, SystemInfo, TVShow } from "./types";
import { m as i18n } from "./paraglide/messages.js";

type Counted = (inputs: { count: string }) => string;
const n = (v: number) => v.toLocaleString();
const plural = (count: number, one: Counted, other: Counted) =>
	(count === 1 ? one : other)({ count: n(count) });
const word = (count: number, one: () => string, other: () => string) =>
	(count === 1 ? one : other)();

// The calendar's kind colours (--kind-*). A show's arrivals are its episodes.
export type KindToken = "movie" | "episode" | "album" | "book";
export const kindToken = (a: Pick<LibraryArrival, "kind">): KindToken =>
	a.kind === "series" ? "episode" : a.kind;

// ── Arrivals ─────────────────────────────────────────────────────────────

// An album opens on its artist's page with the release selected; a volume on
// its series, which is where a volume lives.
export function arrivalHref(a: LibraryArrival): string {
	if (a.kind === "movie") return `/movies/${a.id}`;
	if (a.kind === "series") return `/series/${a.id}`;
	if (a.kind === "album") return a.artist_id ? `/music/${a.artist_id}?release=${a.id}` : "/music";
	return a.series_id ? `/books/series/${a.series_id}` : `/books/${a.id}`;
}

export function arrivalArt(a: LibraryArrival): string {
	if (a.kind === "movie") return posterUrl({ id: a.id });
	if (a.kind === "series") return tvPosterUrl(a.id);
	if (a.kind === "album") return albumPosterUrl(a.id);
	return bookPosterUrl(a.id);
}

// The two caption lines under a card's title: whose it is or what the import
// added, then — where the art and the line above do not already say it —
// what landed: an album's type, a book's format or volume. A movie and a show
// leave the second line to the time alone.
export function arrivalLines(a: LibraryArrival): { sub: string; kind: string } {
	if (a.kind === "movie") return { sub: [a.year, a.resolution].filter(Boolean).join(" · "), kind: "" };
	if (a.kind === "series")
		return { sub: a.addition ? additionLabel(a.addition) : a.year ? String(a.year) : "", kind: "" };
	if (a.kind === "album")
		return { sub: a.artist_name ?? "", kind: releaseTypeLabel(a.release_type ?? "album") };
	if (a.volume != null) return { sub: a.author ?? "", kind: volumeLabel(a.volume) };
	// A format is drawn as its icon on the card (as on the books shelf): the
	// word runs long in French and would push the time out.
	if (a.format) return { sub: a.author ?? "", kind: "" };
	const bookKind = a.book_kind && a.book_kind !== "novel" ? kindLabel(a.book_kind) : "";
	return { sub: a.author ?? "", kind: bookKind || i18n.common_book() };
}

// The week's arrivals by kind, in the sidebar's order, zeros left out.
export function arrivalTally(list: LibraryArrival[]): { token: KindToken; label: string }[] {
	const by = { movie: 0, series: 0, album: 0, book: 0 };
	for (const a of list) by[a.kind]++;
	return [
		{ token: "movie" as const, count: by.movie, label: plural(by.movie, i18n.dash_movie_count_one, i18n.dash_movie_count_other) },
		{ token: "episode" as const, count: by.series, label: plural(by.series, i18n.dash_series_count_one, i18n.dash_series_count_other) },
		{ token: "album" as const, count: by.album, label: plural(by.album, i18n.music_releases_one, i18n.music_releases_other) },
		{ token: "book" as const, count: by.book, label: plural(by.book, i18n.dash_book_count_one, i18n.dash_book_count_other) },
	]
		.filter((x) => x.count > 0)
		.map(({ token, label }) => ({ token, label }));
}

// ── Ledger ───────────────────────────────────────────────────────────────

// One medium in the unit people count it in: movies, episodes, releases,
// titles. `have`, `moving` and `wanted` share that unit and draw the bar
// against `total`.
export type LedgerRow = {
	key: "movies" | "series" | "music" | "books";
	token: KindToken;
	label: string;
	href: string;
	count: string;
	detail: string;
	total: number;
	have: number;
	moving: number;
	wanted: number;
	wantedUnit: string;
	movingUnit: string;
};

export function movieRow(list: Movie[]): LedgerRow {
	const have = list.filter((m) => m.status === "available").length;
	const moving = list.filter((m) => m.status === "downloading" || m.status === "importing").length;
	// An unmonitored title stays "wanted" server-side though nothing searches
	// for it, so it would advertise work that never runs.
	const wanted = list.filter((m) => m.status === "wanted" && m.monitored).length;
	return {
		key: "movies",
		token: "movie",
		label: i18n.movies_label(),
		href: "/movies",
		count: plural(list.length, i18n.dash_movie_count_one, i18n.dash_movie_count_other),
		detail: plural(have, i18n.dash_available_one, i18n.dash_available_other),
		total: list.length,
		have,
		moving,
		wanted,
		wantedUnit: word(wanted, i18n.dash_unit_movies_one, i18n.dash_unit_movies_other),
		movingUnit: word(moving, i18n.dash_unit_movies_one, i18n.dash_unit_movies_other),
	};
}

// Counted in episodes: a show is one row whether it lacks one episode or forty.
export function seriesRow(list: TVShow[]): LedgerRow {
	let total = 0,
		have = 0,
		moving = 0,
		wanted = 0;
	for (const s of list) {
		total += s.total_episodes ?? 0;
		have += s.have_episodes ?? 0;
		moving += (s.downloading_episodes ?? 0) + (s.importing_episodes ?? 0);
		if (s.monitored) wanted += s.wanted_episodes ?? 0;
	}
	return {
		key: "series",
		token: "episode",
		label: i18n.series_label(),
		href: "/series",
		count: plural(list.length, i18n.dash_series_count_one, i18n.dash_series_count_other),
		detail: i18n.dash_episodes_of({ have: n(have), total: n(total) }),
		total,
		have,
		moving,
		wanted,
		wantedUnit: word(wanted, i18n.dash_unit_episodes_one, i18n.dash_unit_episodes_other),
		movingUnit: word(moving, i18n.dash_unit_episodes_one, i18n.dash_unit_episodes_other),
	};
}

// Counted in releases that are out: an announced album is on the calendar,
// not owed to the library.
export function musicRow(list: Artist[]): LedgerRow {
	let total = 0,
		have = 0,
		moving = 0,
		wanted = 0;
	for (const a of list) {
		const t = artistTally(a);
		total += t.released;
		have += t.have;
		moving += t.downloading;
		wanted += t.wanted;
	}
	return {
		key: "music",
		token: "album",
		label: i18n.music_label(),
		href: "/music",
		count: plural(list.length, i18n.music_artists_one, i18n.music_artists_other),
		detail: i18n.dash_releases_of({ have: n(have), total: n(total) }),
		total,
		have,
		moving,
		wanted,
		wantedUnit: word(wanted, i18n.dash_unit_releases_one, i18n.dash_unit_releases_other),
		movingUnit: word(moving, i18n.dash_unit_releases_one, i18n.dash_unit_releases_other),
	};
}

// Counted in shelf titles, a series being one, as the books page counts them.
export function booksRow(list: ShelfItem[]): LedgerRow {
	const books = list.filter((i) => i.type === "book").length;
	const have = list.filter((i) => i.status === "available").length;
	const moving = list.filter((i) => i.status === "downloading").length;
	const wanted = list.filter((i) => i.status === "wanted").length;
	return {
		key: "books",
		token: "book",
		label: i18n.books_label(),
		href: "/books",
		count: plural(list.length, i18n.dash_title_count_one, i18n.dash_title_count_other),
		detail: `${plural(books, i18n.dash_book_count_one, i18n.dash_book_count_other)} · ${plural(list.length - books, i18n.dash_series_count_one, i18n.dash_series_count_other)}`,
		total: list.length,
		have,
		moving,
		wanted,
		wantedUnit: word(wanted, i18n.dash_unit_titles_one, i18n.dash_unit_titles_other),
		movingUnit: word(moving, i18n.dash_unit_titles_one, i18n.dash_unit_titles_other),
	};
}

// Free space across the library volumes system info probes, falling back to
// the data dir on a fresh install. Movie and series paths usually share one
// mount, so two probes reporting the same total and free bytes are one volume;
// a single volume keeps the server's own string.
export function freeSpace(sys: SystemInfo | undefined): string {
	if (!sys) return "";
	const probed = [sys.library_usage, sys.series_usage].filter((u): u is DiskUsage => !!u);
	const list = probed.length ? probed : sys.data_usage ? [sys.data_usage] : [];
	const seen = new Map<string, DiskUsage>();
	for (const u of list) seen.set(`${u.total}|${u.free}`, u);
	const vols = [...seen.values()];
	if (vols.length === 0) return "";
	if (vols.length === 1) return vols[0]?.free ?? "";
	return formatBytes(vols.reduce((s, u) => s + (u.free_bytes ?? 0), 0), "");
}
