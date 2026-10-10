import { formatBytes } from "./format";
import { formatLabel, releaseTypeLabel } from "./music-books";
import type {
	ImportFileClassification,
	ImportScanAlbum,
	ImportScanBook,
	ImportScanFile,
	ImportScanKind,
	ImportScanShow,
} from "./types";
import { m as i18n } from "./paraglide/messages.js";

// The touch review row (and the sheet behind it) renders movie files and series
// show folders through one shape, so the phone/tablet list is built once and
// takes a noun. Desktop keeps its two table rows.
// MusicBrainz ids are strings; every other provider's are numbers.
export type MatchId = number | string;
export type TouchCandidate = {
	id: MatchId;
	title: string;
	year?: number | null;
	// Who made it — an album's artist, a book's author — where the title alone
	// is ambiguous.
	sub?: string;
	// What follows the year: an album's release type. The line is then
	// "artist · year · type" for an album and "author · year" for a book — a
	// release group has no track count, a Hardcover hit no series position.
	tag?: string;
};

export type TouchEntry = {
	id: number;
	// Parsed title when the parser found one, else the bare filename — which is
	// itself the signal that nothing was parsed.
	heading: string;
	headingWeak: boolean;
	path: string;
	sub: string;
	classification: ImportFileClassification;
	decision: string;
	outcome: string;
	outcomeMessage?: string;
	chosenId: MatchId | null;
	chosenLabel: string | null;
	candidates: TouchCandidate[];
	// What the provider search is seeded with: the parsed title, and an album's
	// artist with it.
	seed: string;
	// One extra fact the row should state: an album that brings a new artist.
	flag?: string;
};

export const CLASS_META: Record<
	ImportFileClassification,
	{ label: string; kind: string }
> = {
	confirmed: { label: i18n.imports_confirmed(), kind: "available" },
	ambiguous: { label: i18n.imports_ambiguous(), kind: "wanted" },
	unmatched: { label: i18n.imports_unmatched(), kind: "paused" },
	existing: { label: i18n.imports_existing(), kind: "grabbing" },
};

// The filter chips below lg. Same five options the desktop Select carries, in
// the same order, so the two can't drift.
export const CLASS_CHIPS: { key: "" | ImportFileClassification; label: string }[] =
	[
		{ key: "", label: i18n.common_all() },
		{ key: "confirmed", label: i18n.imports_confirmed() },
		{ key: "ambiguous", label: i18n.imports_ambiguous() },
		{ key: "unmatched", label: i18n.imports_unmatched() },
		{ key: "existing", label: i18n.imports_existing() },
	];

function basename(p: string): string {
	const i = p.lastIndexOf("/");
	return i === -1 ? p : p.slice(i + 1);
}

function titled(title: string, year?: number | null): string {
	return year ? `${title} (${year})` : title;
}

export function fileEntry(f: ImportScanFile): TouchEntry {
	const bits = [f.parsed_quality, f.parsed_release_group, formatBytes(f.size)];
	const chosen = f.decision_tmdb_id ?? null;
	const match =
		chosen != null
			? (f.candidates ?? []).find((c) => c.tmdb_id === chosen)
			: undefined;
	return {
		id: f.id,
		heading: f.parsed_title || basename(f.source_path),
		headingWeak: !f.parsed_title,
		path: f.source_path,
		sub: f.parsed_title
			? basename(f.source_path)
			: bits.filter(Boolean).join(" · "),
		classification: f.classification,
		decision: f.decision,
		outcome: f.outcome,
		outcomeMessage: f.outcome_message,
		chosenId: chosen,
		chosenLabel: chosen == null ? null : (match?.title ?? i18n.imports_match_selected()),
		candidates: (f.candidates ?? []).map((c) => ({
			id: c.tmdb_id,
			title: c.title,
			year: c.year,
		})),
		seed: f.parsed_title ?? "",
	};
}

export function showEntry(sh: ImportScanShow): TouchEntry {
	const chosen = sh.decision_tvdb_id ?? null;
	const match =
		chosen != null
			? (sh.candidates ?? []).find((c) => c.tvdb_id === chosen)
			: undefined;
	return {
		id: sh.id,
		heading: sh.parsed_title || basename(sh.folder_path),
		headingWeak: !sh.parsed_title,
		path: sh.folder_path,
		sub:
			sh.file_count === 1
				? i18n.imports_file_count_one({ count: sh.file_count })
				: i18n.imports_file_count_other({ count: sh.file_count }),
		classification: sh.classification,
		decision: sh.decision,
		outcome: sh.outcome,
		outcomeMessage: sh.outcome_message,
		chosenId: chosen,
		chosenLabel:
			chosen == null
				? null
				: match
					? titled(match.title, match.year)
					: i18n.imports_match_selected(),
		candidates: (sh.candidates ?? []).map((c) => ({
			id: c.tvdb_id,
			title: c.title,
			year: c.year,
		})),
		seed: sh.parsed_title ?? "",
	};
}

const trackCount = (n: number) =>
	(n === 1 ? i18n.imports_track_count_one : i18n.imports_track_count_other)({ count: n });

export function albumEntry(a: ImportScanAlbum): TouchEntry {
	const chosen = a.decision_release_group_mbid ?? null;
	const match = chosen != null ? (a.candidates ?? []).find((c) => c.release_group_mbid === chosen) : undefined;
	return {
		id: a.id,
		heading: a.tagged_album || basename(a.folder_path),
		headingWeak: !a.tagged_album,
		path: a.folder_path,
		sub: [a.tagged_artist, trackCount(a.file_count), a.format].filter(Boolean).join(" · "),
		classification: a.classification,
		decision: a.decision,
		outcome: a.outcome,
		outcomeMessage: a.outcome_message,
		chosenId: chosen,
		chosenLabel: chosen == null ? null : match ? titled(match.title, match.year) : i18n.imports_match_selected(),
		candidates: (a.candidates ?? []).map((c) => ({
			id: c.release_group_mbid,
			title: c.title,
			year: c.year,
			sub: c.artist,
			tag: c.type ? releaseTypeLabel(c.type) : undefined,
		})),
		seed: [a.tagged_artist, a.tagged_album].filter(Boolean).join(" "),
		// Committing a confirmed album whose artist the library lacks adds the
		// artist too, which is worth knowing before it happens.
		flag: a.classification === "confirmed" && a.artist_id == null ? i18n.imports_new_artist() : undefined,
	};
}

export function bookEntry(b: ImportScanBook): TouchEntry {
	const chosen = b.decision_book_hardcover_id ?? null;
	const match = chosen != null ? (b.candidates ?? []).find((c) => c.book_hardcover_id === chosen) : undefined;
	const parts = b.file_count > 1 ? i18n.imports_file_count_other({ count: b.file_count }) : "";
	return {
		id: b.id,
		heading: b.parsed_title || basename(b.source_path),
		headingWeak: !b.parsed_title,
		path: b.source_path,
		sub: [b.parsed_author, `${formatLabel(b.slot)} · ${b.format}`, parts].filter(Boolean).join(" · "),
		classification: b.classification,
		decision: b.decision,
		outcome: b.outcome,
		outcomeMessage: b.outcome_message,
		chosenId: chosen,
		chosenLabel: chosen == null ? null : match ? titled(match.title, match.year) : i18n.imports_match_selected(),
		candidates: (b.candidates ?? []).map((c) => ({
			id: c.book_hardcover_id,
			title: c.title,
			year: c.year,
			sub: c.author,
		})),
		seed: b.parsed_title ?? "",
	};
}

// A provider id as the candidate rows print it: MusicBrainz's are UUIDs, so
// the first block stands in for the rest, as on the artist page.
export const shortId = (id: MatchId) => (typeof id === "string" ? id.slice(0, 8) : String(id));

// The review's wording per kind. Movie and series keep the keys they always
// had; albums and books carry their own, so no sentence is glued to a noun.
export type UnitText = {
	heading: string;
	search: string;
	loading: string;
	empty: string;
	decide: string;
	nothingParsed: string;
	alreadyMatched: string;
	skipThis: string;
	skipAllTitle: string;
	skipPrefix: (n: number) => string;
	skipSuffix: string;
	skipped: (n: number) => string;
	restore: string;
	exclude: string;
	exists: string;
	column: string;
};
const pick = (one: (p: { count: number }) => string, other: (p: { count: number }) => string) => (n: number) =>
	(n === 1 ? one : other)({ count: n });

export function unitText(kind: ImportScanKind): UnitText {
	if (kind === "series")
		return {
			heading: i18n.common_shows(),
			search: i18n.imports_search_folder_title(),
			loading: i18n.common_loading_shows(),
			empty: i18n.imports_no_match_shows(),
			decide: i18n.imports_decide_show(),
			nothingParsed: i18n.imports_nothing_parsed_show(),
			alreadyMatched: i18n.imports_already_matched_show(),
			skipThis: i18n.imports_skip_this_show(),
			skipAllTitle: i18n.imports_skip_all_unmatched_shows(),
			skipPrefix: pick(i18n.imports_skip_body_prefix_show_one, i18n.imports_skip_body_prefix_show_other),
			skipSuffix: i18n.imports_skip_body_suffix_show(),
			skipped: pick(i18n.imports_skipped_show_one, i18n.imports_skipped_show_other),
			restore: i18n.imports_restore_show(),
			exclude: i18n.imports_exclude_show(),
			exists: i18n.imports_show_exists(),
			column: i18n.imports_show_folder(),
		};
	if (kind === "music")
		return {
			heading: i18n.common_albums(),
			search: i18n.imports_search_album(),
			loading: i18n.common_loading_albums(),
			empty: i18n.imports_no_match_albums(),
			decide: i18n.imports_decide_album(),
			nothingParsed: i18n.imports_nothing_parsed_album(),
			alreadyMatched: i18n.imports_already_matched_album(),
			skipThis: i18n.imports_skip_this_album(),
			skipAllTitle: i18n.imports_skip_all_unmatched_albums(),
			skipPrefix: pick(i18n.imports_skip_body_prefix_album_one, i18n.imports_skip_body_prefix_album_other),
			skipSuffix: i18n.imports_skip_body_suffix_album(),
			skipped: pick(i18n.imports_skipped_album_one, i18n.imports_skipped_album_other),
			restore: i18n.imports_restore_album(),
			exclude: i18n.imports_exclude_album(),
			exists: i18n.imports_album_exists(),
			column: i18n.imports_album_folder(),
		};
	if (kind === "book")
		return {
			heading: i18n.books_label(),
			search: i18n.imports_search_book(),
			loading: i18n.common_loading_books(),
			empty: i18n.imports_no_match_books(),
			decide: i18n.imports_decide_book(),
			nothingParsed: i18n.imports_nothing_parsed_book(),
			alreadyMatched: i18n.imports_already_matched_book(),
			skipThis: i18n.imports_skip_this_book(),
			skipAllTitle: i18n.imports_skip_all_unmatched_books(),
			skipPrefix: pick(i18n.imports_skip_body_prefix_book_one, i18n.imports_skip_body_prefix_book_other),
			skipSuffix: i18n.imports_skip_body_suffix_book(),
			skipped: pick(i18n.imports_skipped_book_one, i18n.imports_skipped_book_other),
			restore: i18n.imports_restore_book(),
			exclude: i18n.imports_exclude_book(),
			exists: i18n.imports_book_exists(),
			column: i18n.common_file(),
		};
	return {
		heading: i18n.common_files(),
		search: i18n.imports_search_filename(),
		loading: i18n.common_loading_files(),
		empty: i18n.imports_no_match_files(),
		decide: i18n.imports_decide_file(),
		nothingParsed: i18n.imports_nothing_parsed_file(),
		alreadyMatched: i18n.imports_already_matched_file(),
		skipThis: i18n.imports_skip_this_file(),
		skipAllTitle: i18n.imports_skip_all_unmatched_files(),
		skipPrefix: pick(i18n.imports_skip_body_prefix_file_one, i18n.imports_skip_body_prefix_file_other),
		skipSuffix: i18n.imports_skip_body_suffix_file(),
		skipped: pick(i18n.imports_skipped_file_one, i18n.imports_skipped_file_other),
		restore: i18n.imports_restore_file(),
		exclude: i18n.imports_exclude_file(),
		exists: i18n.imports_movie_exists(),
		column: i18n.common_file(),
	};
}

export type OutcomeTone = "need" | "ok" | "link" | "muted" | "fail";

// The row's trailing word. A committed scan reports what happened; a scan under
// review reports what will happen — and "Decide" is the only one that is work,
// which is what makes the column scannable.
export function outcomeWord(
	e: TouchEntry,
	series = false,
): { text: string; tone: OutcomeTone } {
	switch (e.outcome) {
		case "created":
			return { text: i18n.common_created(), tone: "ok" };
		case "attached":
			return { text: i18n.imports_attached(), tone: "link" };
		case "skipped":
			return { text: i18n.common_skipped(), tone: "muted" };
		case "failed":
			return { text: i18n.status_failed(), tone: "fail" };
	}
	if (e.decision === "skip") return { text: i18n.imports_will_skip(), tone: "muted" };
	if (e.decision === "accept")
		return { text: series ? i18n.imports_will_adopt() : i18n.imports_will_accept(), tone: "ok" };
	if (e.classification === "confirmed")
		return { text: series ? i18n.imports_adopt() : i18n.imports_accept(), tone: "ok" };
	if (e.classification === "existing")
		// Both kinds bind to an entry the library already has. "Link" was the
		// shortened form of the show row's "Link to show", but as a bare word in
		// a trailing column it reads as a hyperlink, so both say Attach.
		return { text: i18n.imports_attach(), tone: "link" };
	return { text: i18n.common_decide(), tone: "need" };
}

export function isActionable(c: ImportFileClassification): boolean {
	return c === "ambiguous" || c === "unmatched";
}

export function pendingDecision(e: TouchEntry): boolean {
	return e.decision === "pending" && isActionable(e.classification);
}
