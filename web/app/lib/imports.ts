import type { StatusKind } from "../components/shared/StatusPill.svelte";
import type { ImportMode, ImportStatus, ImportTransferMode } from "./types";
import { m as i18n } from "./paraglide/messages.js";

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

// commitVerb returns the past-tense action the user sees on the action strip,
// e.g. "Confirmed files will be {commitVerb} into the library."
export function commitVerb(
	mode: ImportMode,
	importMode: ImportTransferMode | "" | undefined,
): string {
	if (mode === "in_place") return "adopted in place";
	switch (importMode) {
		case "move":
			return "moved";
		case "copy":
			return "copied";
		case "hardlink":
			return "hard-linked";
		default:
			return "imported";
	}
}
