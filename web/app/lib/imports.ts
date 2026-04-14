import type { StatusKind } from "../components/shared/StatusPill.svelte";
import type { ImportMode, ImportStatus, ImportTransferMode } from "./types";

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
			return { label: "Running", kind: "downloading", live: true };
		case "committing":
			return { label: "Committing", kind: "grabbing", live: true };
		case "awaiting_review":
			return { label: "Awaiting review", kind: "wanted", live: false };
		case "completed":
			return { label: "Completed", kind: "available", live: false };
		case "cancelled":
			return { label: "Cancelled", kind: "paused", live: false };
		case "failed":
			return { label: "Failed", kind: "failed", live: false };
	}
}

// importModeLabel reads the scan's transfer intent: in_place is always
// "Adopt in place"; a rename scan shows the concrete verb when one was
// pinned for the scan, else the generic label.
export function importModeLabel(
	mode: ImportMode,
	importMode: ImportTransferMode | "" | undefined,
): string {
	if (mode === "in_place") return "Adopt in place";
	if (importMode) return importMode.charAt(0).toUpperCase() + importMode.slice(1);
	return "Import & rename";
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
