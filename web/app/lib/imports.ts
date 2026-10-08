import type { StatusKind } from "@components/shared/StatusPill.svelte";
import { appLabel } from "./arr-import";
import type {
	ImportMode,
	ImportScan,
	ImportScanFile,
	ImportScanKind,
	ImportStatus,
	ImportTransferMode,
} from "./types";
import { NOUN_ALBUM, NOUN_BOOK, NOUN_FILE, NOUN_SHOW, type Noun } from "./nouns";
import { m as i18n } from "./paraglide/messages.js";

// Each scan kind reviews one unit — a movie file, a show folder, an album
// folder, a book — matched against one provider, at one endpoint under
// /library/imports/{id}/. Everything that routes or words a review by kind
// starts here.
export type ImportRows = "files" | "shows" | "albums" | "books";
export const IMPORT_KIND: Record<
	ImportScanKind,
	{ rows: ImportRows; source: string; noun: Noun; label: () => string }
> = {
	movie: { rows: "files", source: "TMDB", noun: NOUN_FILE, label: i18n.movies_label },
	series: { rows: "shows", source: "TVDB", noun: NOUN_SHOW, label: i18n.settings_series },
	music: { rows: "albums", source: "MusicBrainz", noun: NOUN_ALBUM, label: i18n.music_label },
	book: { rows: "books", source: "Hardcover", noun: NOUN_BOOK, label: i18n.books_label },
};

// The query key the unfiltered row list lives under, which the review's
// counts and its bulk skip read. Files predate the others and kept theirs.
export const pendingRowsKey = (rows: ImportRows) => (rows === "files" ? "pending" : `pending-${rows}`);

// importSourceLabel names the application a migrated scan read from, for its
// badge; a folder scan has none.
export function importSourceLabel(
	scan: Pick<ImportScan, "source">,
): string | null {
	return scan.source === "filesystem" ? null : appLabel(scan.source);
}

// scanLocation is what a scan is "of": the instance a migration read, or the
// directory a folder scan walked. A migrated scan's source_path is empty.
export function scanLocation(
	scan: Pick<ImportScan, "source" | "source_path" | "source_url">,
): string {
	if (scan.source !== "filesystem") return scan.source_url ?? "";
	return scan.source_path;
}

// isTitleOnly marks a row a Radarr source tracks without a file: it has no
// path to show and commits as a wanted library entry with no media file.
export function isTitleOnly(f: Pick<ImportScanFile, "source_path">): boolean {
	return f.source_path === "";
}

export type ImportStatusMeta = {
	label: string;
	kind: StatusKind;
	live: boolean;
};

// importStatusMeta is the single source of truth for how an import scan's
// status is worded and tinted. ScanRow, the drill-down header, and the
// stepper all read from here so labels/colors never drift apart.
export function importStatusMeta(status: ImportStatus): ImportStatusMeta {
	switch (status) {
		case "running":
			return { label: i18n.common_running(), kind: "downloading", live: true };
		case "committing":
			return { label: i18n.common_committing(), kind: "grabbing", live: true };
		case "awaiting_review":
			return { label: i18n.common_awaiting_review(), kind: "wanted", live: false };
		case "completed":
			return { label: i18n.status_completed(), kind: "available", live: false };
		case "cancelled":
			return { label: i18n.common_cancelled(), kind: "paused", live: false };
		case "failed":
			return { label: i18n.status_failed(), kind: "failed", live: false };
	}
}

// importModeLabel reads the scan's transfer intent: in_place is always
// "Adopt in place"; a rename scan shows the concrete verb when one was
// pinned for the scan, else the generic label.
export function importModeLabel(
	mode: ImportMode,
	importMode: ImportTransferMode | "" | undefined,
): string {
	if (mode === "in_place") return i18n.imports_adopt_in_place_label();
	if (importMode) return importMode.charAt(0).toUpperCase() + importMode.slice(1);
	return i18n.imports_import_rename_label();
}

type CommitAction = "in_place" | "move" | "copy" | "hardlink" | "import";

function commitAction(
	mode: ImportMode,
	importMode: ImportTransferMode | "" | undefined,
): CommitAction {
	if (mode === "in_place") return "in_place";
	switch (importMode) {
		case "move":
			return "move";
		case "copy":
			return "copy";
		case "hardlink":
			return "hardlink";
		default:
			return "import";
	}
}

// Each action carries a whole sentence rather than a verb the caller splices
// in: a French past participle has to agree with its subject, which a
// substituted word cannot do.
const NOTE: Record<CommitAction, () => string> = {
	in_place: i18n.imports_commit_note_in_place,
	move: i18n.imports_commit_note_move,
	copy: i18n.imports_commit_note_copy,
	hardlink: i18n.imports_commit_note_hardlink,
	import: i18n.imports_commit_note_import,
};

const SHORT: Record<CommitAction, () => string> = {
	in_place: i18n.imports_commit_short_in_place,
	move: i18n.imports_commit_short_move,
	copy: i18n.imports_commit_short_copy,
	hardlink: i18n.imports_commit_short_hardlink,
	import: i18n.imports_commit_short_import,
};

export function commitNote(
	mode: ImportMode,
	importMode: ImportTransferMode | "" | undefined,
): string {
	return NOTE[commitAction(mode, importMode)]();
}

export function commitSummary(
	mode: ImportMode,
	importMode: ImportTransferMode | "" | undefined,
): string {
	return SHORT[commitAction(mode, importMode)]();
}
