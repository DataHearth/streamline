import {
	Ban,
	Check,
	CircleAlert,
	CircleCheck,
	CircleX,
	Download,
	Eye,
	FileX,
	Film,
	GitBranch,
	ListPlus,
	PackageCheck,
	PenLine,
	Plus,
	Radar,
	RefreshCw,
	Replace,
	ShieldCheck,
	ShieldX,
	ThumbsUp,
	Trash,
	VideoOff,
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
 * one row. A series-scoped event carries the seasons and episode count it
 * covered in its payload; when it touched exactly one season, saying so is
 * more useful than the bare show title, and the episode count (bulk imports,
 * renames, grab widenings, drift) rides alongside it — see `seriesQualifier`.
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
			detail: seriesQualifier(event.payload),
		};
	}
	return { title: "Unknown" };
}

/**
 * Qualifier for a series-scoped event: the season when the payload names
 * exactly one, the episode count when the payload has one, both joined when
 * both are present. A search with no count still reads as "Season N" alone —
 * that is the pre-existing behaviour for `searched`, which carries seasons
 * but no episode count.
 */
function seriesQualifier(
	payload: Record<string, unknown> | undefined,
): string | undefined {
	const seasons = payload?.seasons;
	const season =
		Array.isArray(seasons) && seasons.length === 1 && typeof seasons[0] === "number"
			? i18n.season_number({ number: seasons[0] })
			: undefined;
	const episodes = payload?.episodes;
	if (typeof episodes !== "number") return season;
	const count =
		episodes === 1
			? i18n.activity_one_episode()
			: i18n.activity_n_episodes({ count: episodes });
	return season ? `${season} · ${count}` : count;
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
		icon: PackageCheck,
		bg: "bg-status-available/15",
		fg: "text-status-available",
		label: i18n.activity_imported(),
	},
	download_completed: {
		icon: CircleCheck,
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
		icon: CircleX,
		bg: "bg-status-failed/15",
		fg: "text-status-failed",
		label: i18n.dash_evt_download_failed(),
	},
	import_failed: {
		icon: FileX,
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
		icon: VideoOff,
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

/**
 * Second-line text for a monitoring toggle, or "" for any other event.
 *
 * The direction is the point — "Monitoring changed" says something happened
 * without saying which way, and the eye glyph is the same either way. A season
 * or series toggle is one row for a whole cascade, so it also carries how many
 * episodes moved.
 */
export function monitoringDetail(event: ActivityEvent): string {
	if (event.type !== "monitoring_changed") return "";
	const state = event.payload?.monitored
		? i18n.activity_mon_on()
		: i18n.activity_mon_off();
	const n = event.payload?.episodes;
	if (typeof n !== "number" || n === 0) return state;
	const count =
		n === 1 ? i18n.activity_one_episode() : i18n.activity_n_episodes({ count: n });
	return `${count} · ${state}`;
}
