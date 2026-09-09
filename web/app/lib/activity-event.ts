import {
	Ban,
	Check,
	CircleAlert,
	Download,
	Eye,
	Film,
	GitBranch,
	ListPlus,
	PenLine,
	Plus,
	Radar,
	RefreshCw,
	Replace,
	ShieldCheck,
	ShieldX,
	ThumbsUp,
	Trash,
	X,
} from "@lucide/svelte";
import { m as i18n } from "./paraglide/messages.js";
import type { ActivityEvent, ActivityType } from "./types";

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

/**
 * Visual treatment for one event type: the glyph, its tint, and the label.
 *
 * Shared rather than per-component because `Record<ActivityType, Mark>` is
 * exhaustive: two copies meant every new event type had to be added to both,
 * and svelte-check only reports the file it reaches first.
 */
export type Mark = {
	icon: typeof Check;
	bg: string;
	fg: string;
	label: string;
};

export const EVENT_MARKS: Record<ActivityType, Mark> = {
	imported: {
		icon: Check,
		bg: "bg-status-available/15",
		fg: "text-status-available",
		label: i18n.activity_imported(),
	},
	download_completed: {
		icon: Check,
		bg: "bg-status-available/15",
		fg: "text-status-available",
		label: i18n.dash_evt_download_completed(),
	},
	grabbed: {
		icon: Download,
		bg: "bg-status-grabbing/15",
		fg: "text-status-grabbing",
		label: i18n.dash_evt_grabbed(),
	},
	download_failed: {
		icon: X,
		bg: "bg-status-failed/15",
		fg: "text-status-failed",
		label: i18n.dash_evt_download_failed(),
	},
	import_failed: {
		icon: X,
		bg: "bg-status-failed/15",
		fg: "text-status-failed",
		label: i18n.dash_evt_import_failed(),
	},
	drift_detected: {
		icon: GitBranch,
		bg: "bg-status-wanted/15",
		fg: "text-status-wanted",
		label: i18n.dash_evt_drift_detected(),
	},
	drift_confirmed: {
		icon: ShieldCheck,
		bg: "bg-status-wanted/15",
		fg: "text-status-wanted",
		label: i18n.dash_evt_drift_confirmed(),
	},
	searched: {
		icon: Radar,
		bg: "bg-surface-2",
		fg: "text-fg-muted",
		label: i18n.dash_evt_searched(),
	},
	download_cancelled: {
		icon: Ban,
		bg: "bg-status-canceled/15",
		fg: "text-status-canceled",
		label: i18n.dash_evt_download_cancelled(),
	},
	grab_widened: {
		icon: ListPlus,
		bg: "bg-status-grabbing/15",
		fg: "text-status-grabbing",
		label: i18n.dash_evt_grab_widened(),
	},
	import_held_for_review: {
		icon: CircleAlert,
		bg: "bg-status-held/15",
		fg: "text-status-held",
		label: i18n.dash_evt_import_held(),
	},
	added: {
		icon: Plus,
		bg: "bg-accent/15",
		fg: "text-accent",
		label: i18n.dash_evt_added(),
	},
	file_renamed: {
		icon: PenLine,
		bg: "bg-surface-2",
		fg: "text-fg-muted",
		label: i18n.dash_evt_file_renamed(),
	},
	file_removed: {
		icon: Trash,
		bg: "bg-status-missing/15",
		fg: "text-status-missing",
		label: i18n.dash_evt_file_removed(),
	},
	reidentified: {
		icon: Replace,
		bg: "bg-accent/15",
		fg: "text-accent",
		label: i18n.dash_evt_reidentified(),
	},
	metadata_refreshed: {
		icon: RefreshCw,
		bg: "bg-surface-2",
		fg: "text-fg-muted",
		label: i18n.dash_evt_metadata_refreshed(),
	},
	monitoring_changed: {
		icon: Eye,
		bg: "bg-surface-2",
		fg: "text-fg-muted",
		label: i18n.dash_evt_monitoring_changed(),
	},
	request_approved: {
		icon: ThumbsUp,
		bg: "bg-status-available/15",
		fg: "text-status-available",
		label: i18n.dash_evt_request_approved(),
	},
	transcode_completed: {
		icon: Film,
		bg: "bg-status-succeeded/15",
		fg: "text-status-succeeded",
		label: i18n.dash_evt_transcode_completed(),
	},
	transcode_failed: {
		icon: Film,
		bg: "bg-status-failed/15",
		fg: "text-status-failed",
		label: i18n.dash_evt_transcode_failed(),
	},
	transcode_rejected: {
		icon: ShieldX,
		bg: "bg-status-rejected/15",
		fg: "text-status-rejected",
		label: i18n.dash_evt_transcode_rejected(),
	},
};
