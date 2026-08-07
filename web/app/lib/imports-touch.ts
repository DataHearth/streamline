import { formatBytes } from "./format";
import type {
	ImportFileClassification,
	ImportScanFile,
	ImportScanShow,
} from "./types";
import { m as i18n } from "./paraglide/messages.js";

// The touch review row (and the sheet behind it) renders movie files and series
// show folders through one shape, so the phone/tablet list is built once and
// takes a noun. Desktop keeps its two table rows.
export type TouchCandidate = { id: number; title: string; year?: number | null };

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
	chosenId: number | null;
	chosenLabel: string | null;
	candidates: TouchCandidate[];
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
		sub: i18n.imports_file_count({ count: sh.file_count }),
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
