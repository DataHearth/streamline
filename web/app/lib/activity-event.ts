import {
	Ban,
	type Check,
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
import { formatLabel, tracksCount, volumeLabel } from "./music-books";
import { NOUN_ALBUM } from "./nouns";
import { m as i18n } from "./paraglide/messages.js";
import { link, targetHref, type MediaTarget } from "./routes";
import type { ActivityEvent, ActivityType } from "./types";

export type EventSubject = {
	/** Row heading — the title the event happened to. */
	title: string;
	/** What the row opens on, or undefined when it has nowhere to go. */
	target?: MediaTarget;
	/** Its address (`targetHref(target)`). */
	href?: string;
	/** Qualifier under the heading (SxxExx, "Season 3"), when there is one. */
	detail?: string;
	/**
	 * The part of the title the event is about, when it is narrower than the
	 * title itself: the episode, the season, the album, the volume. A detail
	 * page's History tab is already on the title, so this is the link it has
	 * left to give.
	 */
	part?: MediaTarget;
};

const pad = (n: number) => String(n).padStart(2, "0");
const joinDot = (parts: (string | undefined)[]) => parts.filter(Boolean).join(" · ") || undefined;

function subject(s: Omit<EventSubject, "href">): EventSubject {
	return { ...s, href: targetHref(s.target) };
}

/**
 * Resolves which of an event's possible owners is set and renders it as one
 * row. A series-scoped event carries the seasons and episode count it
 * covered in its payload; when it touched exactly one season, saying so is
 * more useful than the bare show title, and the episode count (bulk imports,
 * renames, grab widenings, drift) rides alongside it — see `seriesQualifier`.
 */
export function eventSubject(event: ActivityEvent): EventSubject {
	if (event.movie) {
		return subject({ title: event.movie.title, target: { kind: "movie", id: event.movie.id } });
	}
	if (event.episode) {
		const e = event.episode;
		const target: MediaTarget | undefined = e.series_id
			? { kind: "episode", seriesId: e.series_id, season: e.season, episodeId: e.episode_id }
			: undefined;
		return subject({
			title: e.show_title,
			target,
			detail: `S${pad(e.season)}E${pad(e.episode)}`,
			part: target,
		});
	}
	if (event.series) {
		const season = singleSeason(event.payload);
		const part: MediaTarget | undefined =
			season === undefined ? undefined : { kind: "season", seriesId: event.series.id, season };
		return subject({
			title: event.series.title,
			target: part ?? { kind: "show", id: event.series.id },
			detail: seriesQualifier(event.payload),
			part,
		});
	}
	// An album row reads like the queue's — the artist, then which album — and
	// lands on the artist page with that release open.
	if (event.album) {
		const a = event.album;
		const tracks = event.payload?.tracks;
		const target: MediaTarget | undefined = a.artist_id
			? { kind: "album", id: a.id, artistId: a.artist_id }
			: undefined;
		return subject({
			title: a.artist_name || a.title,
			target,
			detail: joinDot([a.artist_name ? a.title : undefined, typeof tracks === "number" ? tracksCount(tracks) : undefined]),
			part: target,
		});
	}
	if (event.artist) {
		return subject({ title: event.artist.name, target: { kind: "artist", id: event.artist.id } });
	}
	// A volume's own title is often just "Tome 3": the series and its number are
	// what identify it, as in the queue. The slot rides alongside, since a book
	// holds an ebook and an audiobook and either can be the one that moved.
	if (event.book) {
		const b = event.book;
		const slot = b.slot ? formatLabel(b.slot) : undefined;
		const target: MediaTarget = { kind: "book", id: b.id };
		return subject({
			title: b.series ? b.series.title : b.title,
			target,
			detail: joinDot([b.series ? volumeLabel(b.series.number) : undefined, slot]),
			// A volume is its own page, under the series page that lists it.
			part: b.series ? target : undefined,
		});
	}
	if (event.book_series) {
		return subject({ title: event.book_series.title, target: { kind: "bookSeries", id: event.book_series.id } });
	}
	return { title: i18n.common_unknown() };
}

/**
 * The download record an event came from, when it still exists: a grab, the
 * download finishing or failing, a hold, the import it produced. Its address is
 * the record's detail on Activity.
 */
export function eventDownloadHref(event: ActivityEvent): string | undefined {
	return event.download_id ? link.download(event.download_id) : undefined;
}

function singleSeason(payload: Record<string, unknown> | undefined): number | undefined {
	const seasons = payload?.seasons;
	return Array.isArray(seasons) && seasons.length === 1 && typeof seasons[0] === "number" ? seasons[0] : undefined;
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
	const n = singleSeason(payload);
	const season = n === undefined ? undefined : i18n.season_number({ number: n });
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
	drift_reverted: {
		icon: FileX,
		bg: "bg-status-wanted/15",
		fg: "text-status-wanted",
		label: i18n.dash_evt_drift_reverted(),
	},
	drift_recovered: {
		icon: CircleCheck,
		bg: "bg-status-available/15",
		fg: "text-status-available",
		label: i18n.dash_evt_drift_recovered(),
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
 * episodes moved — as an artist's policy change carries its albums and a book
 * series' its volumes.
 */
export function monitoringDetail(event: ActivityEvent): string {
	if (event.type !== "monitoring_changed") return "";
	const state = event.payload?.monitored
		? i18n.activity_mon_on()
		: i18n.activity_mon_off();
	return joinDot([cascadeCount(event.payload), state]) ?? state;
}

// How much one monitoring row moved: a series' episodes, an artist's albums
// or a book series' volumes. Absent or zero says nothing.
function cascadeCount(payload: Record<string, unknown> | undefined): string | undefined {
	const n = (key: string) => {
		const v = payload?.[key];
		return typeof v === "number" && v > 0 ? v : undefined;
	};
	const episodes = n("episodes");
	if (episodes) return episodes === 1 ? i18n.activity_one_episode() : i18n.activity_n_episodes({ count: episodes });
	const albums = n("albums");
	if (albums) return NOUN_ALBUM.count(albums);
	const volumes = n("volumes");
	if (volumes) return volumes === 1 ? i18n.activity_one_volume() : i18n.activity_n_volumes({ count: volumes });
	return undefined;
}
