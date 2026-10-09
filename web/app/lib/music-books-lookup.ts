// The add/request flow for music and books. MusicBrainz answers for artists and
// Hardcover for books; both are normalised to one hit here, so the desktop
// modal, the phone screen and a request row read the same fields.

import { api, ApiError, errorText } from "./api";
import type { RequestMediaDetails } from "./types";
import { kindLabel, overviewLang, type ProfileMedia, type BookFormat, type BookKind, type ReleaseType } from "./music-books";
import { artistPosterUrl, bookPosterUrl, lookupPosterUrl } from "./posters";
import { m as i18n } from "./paraglide/messages.js";

export type LookupKind = "artist" | "book";

export type ArtistHit = {
	mbid: string;
	name: string;
	sort_name?: string;
	// MusicBrainz's own tiebreaker for namesakes ("Swedish synth-pop band").
	disambiguation?: string;
	type?: "group" | "person";
	genre?: string;
	area?: string;
	since?: number;
	// MusicBrainz's search ranking; absent on a detail.
	score?: number;
	already_added?: boolean;
	// Set with already_added, so "Open in library" needs no library fetch.
	library_id?: number;
};

export type BookHit = {
	hardcover_id: number;
	type: "book" | "series";
	title: string;
	original_title?: string;
	author: string;
	// Absent on a search hit: Hardcover's search document does not carry it.
	kind?: BookKind;
	year?: number;
	// Series only.
	volumes?: number;
	ongoing?: boolean;
	already_added?: boolean;
	library_id?: number;
	// The library book whose poster is the cover, when the title is held.
	cover_id?: number;
};

export type LookupRelease = { mbid: string; title: string; year?: number; type: ReleaseType };
export type ArtistDetail = {
	overview?: string;
	genres?: string[];
	members?: string[];
	// Newest first.
	releases?: LookupRelease[];
};

export type LookupEdition = {
	language: string;
	format: BookFormat;
	publisher: string;
	year: number;
	original?: boolean;
};
export type BookDetail = {
	overview?: string;
	genres?: string[];
	pages?: number;
	editions?: LookupEdition[];
	// Series only: Hardcover ids of the first volumes, in order; each is a
	// lookup cover.
	volume_book_ids?: number[];
};

// /requests/{id}/metadata answers a music or book request with the hit and its
// detail in one object.
export type ArtistMeta = ArtistHit & ArtistDetail;
export type BookMeta = BookHit & BookDetail;

// What /requests/{id}/metadata answers, by the request's media_type.
export type RequestMetadata = RequestMediaDetails | ArtistMeta | BookMeta;

export type LookupHit = {
	// mbid for an artist; "b<id>" / "s<id>" for a book or a series, whose ids
	// Hardcover keeps in separate spaces.
	key: string;
	kind: LookupKind;
	series: boolean;
	title: string;
	// The line under the title: an artist's genre and area, a book's author.
	subtitle?: string;
	// Disambiguation or original title, shown in italics where there is room.
	aside?: string;
	image?: string;
	year?: number;
	held: boolean;
	library_id?: number;
	artist?: ArtistHit;
	book?: BookHit;
};

export function artistHit(a: ArtistHit): LookupHit {
	return {
		key: a.mbid,
		kind: "artist",
		series: false,
		title: a.name,
		subtitle: [a.genre, a.area].filter(Boolean).join(" · ") || undefined,
		aside: a.disambiguation,
		image: a.library_id ? artistPosterUrl(a.library_id) : lookupPosterUrl("artists", a.mbid),
		year: a.since,
		held: !!a.already_added,
		library_id: a.library_id,
		artist: a,
	};
}

// A series hit has no cover of its own; the placeholder renders.
function bookImage(b: BookHit): string | undefined {
	if (b.library_id && b.cover_id) return bookPosterUrl(b.cover_id);
	return b.type === "book" ? lookupPosterUrl("books", b.hardcover_id) : undefined;
}

export function bookHit(b: BookHit): LookupHit {
	return {
		key: (b.type === "series" ? "s" : "b") + b.hardcover_id,
		kind: "book",
		series: b.type === "series",
		title: b.title,
		subtitle: b.author,
		aside: b.original_title && b.original_title !== b.title ? b.original_title : undefined,
		image: bookImage(b),
		year: b.year,
		held: !!b.already_added,
		library_id: b.library_id,
		book: b,
	};
}

export async function lookupSearch(kind: LookupKind, q: string): Promise<LookupHit[]> {
	const query = encodeURIComponent(q);
	if (kind === "artist") {
		const res = await api<{ items: ArtistHit[] }>(`/music/search?query=${query}`);
		return (res.items ?? []).map(artistHit);
	}
	const res = await api<{ items: BookHit[] }>(`/books/search?query=${query}`);
	return (res.items ?? []).map(bookHit);
}

export function lookupDetail(h: LookupHit): Promise<ArtistDetail | BookDetail> {
	if (h.artist) return api<ArtistDetail>(`/music/search/${h.artist.mbid}?lang=${overviewLang()}`);
	const b = h.book!;
	return api<BookDetail>(`/books/search/${b.hardcover_id}?type=${b.type}`);
}

// A 409 from an add means another tab or user got there first.
export function addErrorText(e: unknown, direct: boolean): string {
	if (direct && e instanceof ApiError && e.status === 409) return i18n.lookup_already_in_library();
	return errorText(e, i18n.common_add_failed());
}

export const LOOKUP_SOURCE: Record<LookupKind, string> = {
	artist: "MusicBrainz",
	book: "Hardcover",
};

// The profile list a hit is added under, and the query root its library page
// lives under.
export const profileMedia = (k: LookupKind): ProfileMedia => (k === "artist" ? "music" : "books");
export const libraryRoot = (k: LookupKind) => (k === "artist" ? "music" : "books");

export function libraryHref(h: LookupHit, id: number): string {
	if (h.kind === "artist") return `/music/${id}`;
	return h.series ? `/books/series/${id}` : `/books/${id}`;
}

export function monitorOptions(h: LookupHit | undefined): { value: string; label: string }[] {
	if (!h || h.kind === "artist")
		return [
			{ value: "all", label: i18n.music_monitor_all() },
			{ value: "future", label: i18n.music_monitor_future() },
			{ value: "none", label: i18n.music_monitor_none() },
		];
	if (h.series)
		return [
			{ value: "all", label: i18n.books_monitor_volumes_all() },
			{ value: "future", label: i18n.books_monitor_volumes_future() },
		];
	return [
		{ value: "both", label: i18n.books_monitor_both() },
		{ value: "ebook", label: i18n.books_monitor_ebook() },
		{ value: "audiobook", label: i18n.books_monitor_audiobook() },
	];
}

export function addRequest(h: LookupHit, profile: string, monitor: string) {
	const quality_profile = profile || undefined;
	if (h.artist)
		return { path: "/music/artists", body: { mbid: h.artist.mbid, quality_profile, monitor } };
	const b = h.book!;
	return {
		path: b.type === "series" ? "/books/series" : "/books",
		body: { hardcover_id: b.hardcover_id, quality_profile, monitor },
	};
}

// The request a request_only member sends instead. The profile is a
// preference; the reviewer can override it.
export function requestBody(h: LookupHit, profile: string) {
	const title = h.title;
	const quality_profile = profile || undefined;
	if (h.artist) return { media_type: "artist", media_mbid: h.artist.mbid, title, quality_profile };
	return {
		media_type: h.series ? "book_series" : "book",
		media_id: h.book!.hardcover_id,
		title,
		quality_profile,
	};
}

// The chips under a hit's title: what it is and when.
export function hitChips(h: LookupHit): string[] {
	if (h.artist) {
		const a = h.artist;
		return [
			a.genre,
			a.since ? i18n.music_since({ year: String(a.since) }) : undefined,
		].filter((x): x is string => !!x);
	}
	const b = h.book;
	// A request's hit before its metadata lands carries neither record.
	if (!b) return [];
	return [
		b.kind ? kindLabel(b.kind) : undefined,
		b.year ? String(b.year) : undefined,
		b.volumes ? volumesCount(b.volumes) : undefined,
	].filter((x): x is string => !!x);
}

export const volumesCount = (n: number) =>
	(n === 1 ? i18n.lookup_volumes_one : i18n.lookup_volumes_other)({ count: String(n) });

// A music or book request as a hit, for the panel on the requests page. Until
// its own metadata lands the title is all there is, which still beats an empty
// pane — and metadata for another request (a cache still holding the last one)
// is never shown under this one's title.
export function requestHit(
	r: { media_type: string; media_id: number; media_mbid?: string; title: string },
	meta?: ArtistMeta | BookMeta,
): LookupHit {
	if (meta && r.media_type === "artist" && "mbid" in meta && meta.mbid === r.media_mbid) return artistHit(meta);
	if (meta && r.media_type !== "artist" && "hardcover_id" in meta && meta.hardcover_id === r.media_id)
		return bookHit(meta);
	return {
		key: r.media_mbid ?? (r.media_type === "book_series" ? "s" : "b") + r.media_id,
		kind: r.media_type === "artist" ? "artist" : "book",
		series: r.media_type === "book_series",
		title: r.title,
		held: false,
	};
}
